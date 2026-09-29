# Phase 1 — gVisor + gdb spike

| | |
| --- | --- |
| Depends on | Phase 0 |
| Unblocks | Phases 2 and 5 |
| Spec sections | "Lab images → Base image", "Sandbox security profile" (entire), "Local performance test suite → Scenarios P0", "Open questions & risks" rows on ASLR and hardware watchpoints |
| Effort | one weekend |

## Objective

Prove that gdb does its job inside gVisor with the sandbox locked down as the spec requires, find out which features do not work and write the fallback into `.gdbinit`, and take the first real measurement: memory and start time for one idle lab under `runsc` versus `runc`. This phase de-risks the only real unknown in the project.

## As built (2026-09-29) — read this before the tasks

The tasks below are the original plan. Where the implementation differs, this list wins:

- **Build image** is Alpine 3.20 (musl, same digest as labbase) + `build-base gdb file`, not `gcc:13`. A glibc binary would not run on Alpine.
- **gdb settings** live at `/etc/lab-config/gdb/gdbinit`, read via `XDG_CONFIG_HOME` set in the sandbox spec. Alpine's gdb has no system gdbinit, and `/home/lab` is an empty tmpfs at runtime.
- **Image tooling** is shared in `images/lib.sh`. Docker builds for the lab host's architecture, then `ctr import` loads the image into namespace `labs`.
- **`specrun`** lives at `labd/cmd/specrun`. The containerd code is in `labd/internal/orch` (`RunOnce`, `BuildSpec`, `CheckInvariants`, `ReadCgroupStats`), because nothing outside `orch` may import containerd.
- **Static binaries (ADR 0010):** gVisor cannot turn ASLR off, so `perf` and `probe` are built `-static -no-pie -fno-pie`, and `build.sh` rejects a binary with an `INTERP` or `DYNAMIC` segment. Code, globals and libc are fixed; the stack and heap move. `disable-randomization` and `aslr-gdb-stack` are FALLBACK rows under gVisor.
- **P0 checks** are inline in `labd/perf/p0/p0.sh`, not separate `checks/*.gdb` files, and a `probe` binary in the perf image tests the sandbox with raw syscalls. 30 checks per runtime, including `aslr-gdb-libc`, `aslr-direct-libc` and `no-shared-libs` (ADR 0010).
- **Measurements** are in `labd/perf/single-lab.sh` (`./run.sh perf --scenario single-lab`).
- **Pids:** the lab limit is `RLIMIT_NPROC`; cgroup pids = limit + 96 under gVisor (ADR 0009).
- **CPU** is judged from the host cgroup. gVisor's in-sandbox CPU clock over-reports.
- **Gate:** requires a P0 with zero runsc FAIL rows, and single-lab numbers, from an **x86-64** host. The arm64 dev VM cannot pass P0 under gVisor (QUESTIONS Q13).
- **Task 1.9** (gdb without Python) is not started. It is optional; do it if the x86-64 start latency or image size warrants it.

## Deliverables

| File | Purpose |
| --- | --- |
| `images/labbase/Dockerfile` | `alpine:3.20@sha256:<digest>` + `gdb binutils`, applet removal, user `lab`, `.gdbinit`, `PS1` |
| `images/labbase/gdbinit` | `set disable-randomization on`, `set can-use-hw-watchpoints 0`, `set pagination off`, `set confirm off`, plus anything P0 proves necessary |
| `images/labbase/strip-applets.sh` | Removes `apk`, `wget`, `nc`, `ftpget`, `telnet`, `httpd`, `udhcpc`, `ifconfig`, `route`, `/etc/apk`; keeps the allow-list from the spec |
| `images/labbase/test.sh` | Asserts absent and present tools, uid 1000, home, `.gdbinit` contents, image size < 45 MB |
| `images/labbase/build.sh` | Builds with `docker buildx --platform linux/amd64` (and `linux/arm64` for the dev VM), exports OCI tar, imports into containerd namespace `labs`, prints the digest |
| `labd/sandbox/sandbox-base.json` | The OCI runtime spec every lab starts from; all controls from the spec table |
| `labd/sandbox/README.md` | Field-by-field explanation of `sandbox-base.json` with the invariant number (S1–S8) each field enforces |
| `images/perf/` | `Dockerfile` (FROM labbase), `src/perf.c` (a tier-1-like program with a loop, a struct, three pthreads, a signal handler, a deliberate SIGSEGV path), `build.sh`, `session.gdb` (scripted 10-minute workflow), `core/` (a pre-generated core file for the core-load check) |
| `labd/perf/p0/p0.sh` | Runs every P0 check under `runsc` and `runc`, writes `docs/metrics/p0-<date>-<host>.json` and `.md` |
| `labd/perf/p0/checks/*.gdb` | One batch script per check |
| `docs/metrics/p0-<date>-<host>.md` | Result table |
| `docs/metrics/single-lab-<date>-<host>.md` | RSS, start latency, image size for one lab, runsc vs runc |

## Tasks

### 1.1 Base image
Write the Dockerfile. Pin `alpine:3.20` by digest. `apk add --no-cache gdb binutils`. Run `strip-applets.sh`. Create user `lab` uid 1000 gid 1000, home `/home/lab`. Copy `gdbinit` to `/home/lab/.gdbinit` and `/etc/gdb/gdbinit` (system-wide, so `HOME` tricks do not bypass it). Set `ENV PS1='lab$ '`, `USER lab`, `WORKDIR /home/lab`. Record the resulting image size.
**Done when:** `images/labbase/build.sh` succeeds and `images/labbase/test.sh` passes, including size < 45 MB. If size is over 45 MB, record the number and continue; the `--without-python` gdb build is task 1.9 (stretch).

### 1.2 Sandbox base spec
Write `labd/sandbox/sandbox-base.json`. Start from `ctr oci spec` output and edit: `root.readonly: true`; `process.user` 1000/1000; `process.noNewPrivileges: true`; `process.capabilities` all five lists empty; `process.rlimits` `RLIMIT_FSIZE` 32 MiB soft/hard, `RLIMIT_NOFILE` 256, `RLIMIT_NPROC` 32; `process.env` `HOME=/home/lab PATH=/usr/bin:/bin TERM=xterm-256color`; `process.terminal: true`; `linux.namespaces` includes `network` with no `path` (fresh empty netns), plus pid, ipc, uts, mount; `mounts` exactly: `/proc` (proc), `/dev` (tmpfs, minimal), `/dev/pts` (devpts), `/tmp` and `/home/lab` as tmpfs `size=16m,noexec,nosuid,nodev,mode=1777` and `mode=0700` respectively; **no** `/sys`, no host binds; `linux.resources` placeholders for memory/cpu/pids that `labd` fills from the manifest (leave defaults 128 MiB, 50000/100000 quota, pids 32); `linux.maskedPaths` and `readonlyPaths` from runc defaults. Write `README.md` mapping each field to S1–S8.
**Done when:** `ctr -n labs run --runtime io.containerd.runsc.v1 --config labd/sandbox/sandbox-base.json labbase p0 /bin/sh -c 'id; touch /x; cat /proc/net/dev'` prints `uid=1000`, `touch: /x: Read-only file system`, and a `/proc/net/dev` with only `lo` (down). Note: `ctr` does not take `--config`; use `ctr run --with-ns` flags or, simpler, a tiny Go program `labd/sandbox/cmd/specrun/main.go` that loads the JSON and creates the container via the containerd client. Keep that program; Phase 2 grows from it.

### 1.3 Perf challenge program
Write `images/perf/src/perf.c` (< 200 lines): a `struct account { char name[8]; unsigned checksum; int balance; }` array, a loop with an off-by-one, a `find()` that can return NULL, three pthreads incrementing a shared counter, a `SIGUSR1` handler that sets a flag, a `crash()` path that dereferences NULL when argv[1] is `crash`, and a `report()` function. Build with `gcc -O0 -g -no-pie -fno-pie -fno-stack-protector -pthread` inside `images/build/Dockerfile` (create the build image now: `gcc:13` pinned by digest with `SOURCE_DATE_EPOCH`). Generate a core file by running the crash path with `ulimit -c unlimited` in the build container and ship it at `/home/lab/perf.core`... no — `/home/lab` is tmpfs at runtime, so ship it at `/opt/perf/perf.core` and the binary and source at `/opt/perf/`.
**Done when:** `images/perf/build.sh` produces an image; `sha256sum` of the binary is identical across two consecutive builds.

### 1.4 Scripted session
Write `images/perf/session.gdb`: 10 minutes of realistic work — `break main`, `run`, 200× `next`/`step`, `print` of locals and struct members, `display`, one `watch` on a variable in a short loop, `bt` in a crash, `info threads`, `thread apply all bt`, `finish`, `set var`, `x/16xb`, `call report()`. Pace it with `shell sleep`? No: `shell` is what we want unavailable. Pacing is the driver's job in Phase 4; here the file is just the command list, one per line, so Phase 3/4 clients can feed it with timing.
**Done when:** `gdb -batch -x session.gdb /opt/perf/perf` inside the container exits 0 and prints the final `report()` line.

### 1.5 P0 checks
One `.gdb` batch file per row, run by `p0.sh` under both runtimes, each producing PASS / FAIL / FALLBACK:

| Check | Assertion |
| --- | --- |
| breakpoint | `break main`, `run`, stops at `main` |
| sw watchpoint | `set can-use-hw-watchpoints 0`, `watch counter`, triggers with old/new values |
| hw watchpoint | `set can-use-hw-watchpoints 1`, `watch counter`; record whether it triggers (expected FAIL under systrap → FALLBACK is the `.gdbinit` line) |
| aslr-gdb | `print &main` in 3 separate gdb runs identical |
| aslr-direct | run binary directly 3 times, it prints `&main`; identical |
| threads | `info threads` lists 4 threads after the pthreads start |
| signal | `handle SIGUSR1 stop print`, `signal SIGUSR1` delivered, handler flag set |
| core | `gdb /opt/perf/perf /opt/perf/perf.core`, `bt` shows `crash` frame |
| set-var-return-jump | `set var`, `return`, `jump` each change control flow as expected |
| call | `call report()` prints |
| finish | `finish` prints `Value returned` |
| no-net | `cat /proc/net/route` empty; a tiny C program that calls `socket()+connect()` to 1.1.1.1:80 gets `ENETUNREACH` |
| ro-root | `touch /x` fails |
| tmpfs-limits | `dd if=/dev/zero of=/tmp/f bs=1M count=20` fails; `cp /bin/sh /tmp/sh && /tmp/sh` fails with permission denied |
| pids | fork bomb capped: `ulimit -u` shows 32 or the fork loop errors |
| shell-cmd | gdb `shell wget x` → `sh: wget: not found`; `shell nc` → not found; `python print(1)` → record whether Python is present (it will be until 1.9) |

**Done when:** `p0.sh` writes the JSON and Markdown report and every row is PASS or FALLBACK with a fallback text. No FAIL rows remain. The arm64 dev VM run is labelled `dev-vm`; the x86-64 run is labelled with its host name.

### 1.6 First measurements
In `p0.sh` (or a sibling `single-lab.sh`), for `runsc` and `runc`: start the perf container 10 times, measure wall time from `ctr run` to the gdb `(gdb)` prompt appearing; after `break main`/`run`, sample `memory.current` from the container's cgroup and the RSS of the `runsc-sandbox` process (gVisor Sentry) for 30 s; record p50/p95/max. Also record image size and the time for `gdb -batch -x session.gdb` to complete under each runtime (this is the `step_cmd_ms_runc` vs `runsc` overhead seed).
**Done when:** `docs/metrics/single-lab-<date>-<host>.md` has a table with those numbers and the raw JSON sits beside it.

### 1.7 Image test
`images/labbase/test.sh` runs the image under runsc and asserts: `which wget nc ftpget telnet httpd udhcpc ifconfig route apk` all fail; `which sh ls cat less grep head tail wc hexdump strings file objdump readelf nm gdb` all succeed; `id -u` is 1000; `/home/lab/.gdbinit` and `/etc/gdb/gdbinit` contain the four settings; `ls /etc/apk` fails; image size from `ctr images ls` < 45 MB (or the recorded number if 1.9 is not done).
**Done when:** the script exits 0.

### 1.8 Wire the gate
Fill `phase_1` in `scripts/gate.sh`: build labbase, run `test.sh`, run `p0.sh`, assert no FAIL rows in the newest `p0-*.json`, assert `single-lab-*.md` exists.
**Done when:** `./run.sh gate --phase 1` exits 0.

### 1.9 Stretch: gdb without Python
Only if 1.1 exceeds 45 MB or time allows. Build gdb from source `--without-python --disable-tui --without-guile` in a builder stage and copy the binary into labbase. Re-run 1.5 and 1.7.
**Done when:** image size recorded before and after; `python print(1)` in gdb reports Python unavailable.

## Tests

- `images/labbase/test.sh` (image contents).
- `labd/perf/p0/p0.sh` (behaviour under the sandbox, both runtimes).
- Reproducibility: two builds of `perf` produce the same binary hash (asserted in `images/perf/build.sh`).
- The spec golden test for `sandbox-base.json` is written in Phase 2; here the JSON is hand-checked against `README.md` and validated with `runsc spec --help`-style loading through `specrun`.

## Gate

```
./run.sh gate --phase 1
```
1. `images/labbase/build.sh` and `test.sh` exit 0.
2. `labd/perf/p0/p0.sh --runtime runsc` and `--runtime runc` exit 0; newest `docs/metrics/p0-*.json` has zero `FAIL`.
3. `docs/metrics/single-lab-*.md` exists and has non-empty p95 RSS for both runtimes.
4. If the run was on the arm64 dev VM only, the gate still passes but prints `WARNING: x86-64 P0 pending` and STATUS.md must carry that note. The x86-64 P0 run is a hard requirement for the Phase 5 gate.

## Metrics to record

`docs/metrics/single-lab-<date>-<host>.json`:
```json
{"host":"","arch":"","runtime":"runsc|runc","image_mb":0,
 "start_to_prompt_ms":{"p50":0,"p95":0,"max":0},
 "cgroup_mem_current_mb":{"p50":0,"p95":0,"max":0},
 "sentry_rss_mb":{"p50":0,"p95":0,"max":0},
 "session_gdb_wall_s":0}
```
Update `docs/metrics/capacity.md`: replace the "Memory per lab" and "Disk (images)" estimates with measured values, mark `dev-vm` if not from x86-64.

## Non-goals

- Anything in Go beyond `specrun` (Phase 2).
- Real challenge content (Phase 5). The perf program is not a challenge.
- Network policy at the host level (there is no network to police).

## Handoff

- STATUS.md updated; `docs/metrics/` has the p0 and single-lab files.
- Any `.gdbinit` additions are listed in `images/labbase/README.md` with the P0 row that motivated them.
- If hardware watchpoints worked under systrap on x86-64, open a question in QUESTIONS.md about relaxing `can-use-hw-watchpoints` per tier; do not change the default.
