# Project status

Update this file at the end of every working session. Keep it factual. Newest log entry at the top.

## Current phase

**Phase 1 — gVisor + gdb spike.** In progress on branch `phase-1-gvisor-gdb-spike`. Tasks 1.1–1.8 are built. Lab binaries are now static (ADR 0010). The gate passes every check it can on the Mac. It is waiting only for **x86-64 results from the Linux laptop**, re-run with the static perf image and the runc fix. First push this branch from the Mac (`git push`; not done by the agent), then on the laptop:

```
git pull && ./run.sh vm up
./run.sh gate --phase 1                                  # builds images, runs P0 on this host (scratch)
LAB_HOST=linux-laptop ./run.sh perf --scenario P0          # the authoritative P0
LAB_HOST=linux-laptop ./run.sh perf --scenario single-lab  # ~3 minutes
git add docs/metrics && git commit -m "[P1] metrics: x86-64 P0 and single-lab" && git push
./run.sh gate --phase 1                                  # must now pass
```

Expected on the laptop: runsc has no FAIL rows, and FALLBACK only for `hw-watchpoint`, `disable-randomization` and `aslr-gdb-stack`; runc has no FAIL rows. If any new ASLR row (`aslr-gdb-libc`, `aslr-direct-libc`, `no-shared-libs`) fails, stop: ADR 0010 does not hold on x86-64.

Phase 0 task 0.9 (second developer onboarding) is still open and non-blocking.

<details><summary>Earlier checklist (done)</summary>


- **0.8 on the Linux x86-64 laptop:** clone, `./run.sh doctor`, `./run.sh vm up`, `./run.sh vm verify`, then `./run.sh gate --phase 0`. Write `docs/metrics/environment-linux-laptop.md` in the same format as `environment-dev-vm.md`. Also check whether non-terminal gVisor I/O hangs there too (ADR 0007): `sudo timeout 25 ctr -n labs run --rm --null-io --runtime io.containerd.runsc.v1 docker.io/library/alpine:3.20 t1 /bin/true; echo $?` (124 means it hangs).
- **0.9:** second developer runs `./run.sh doctor` and follows `docs/ONBOARDING.md`.
- **0.7:** removed. The owner deleted the CI workflow to save Actions minutes; do not add workflows (ADR 0008).
- **0.10:** gate output from the laptop pasted below. **Do not start Phase 1** until the owner has run Phase 0 on the Linux laptop.

</details>

## Phase board

| Phase | Name | State | Gate result | Date |
| --- | --- | --- | --- | --- |
| 0 | Bootstrap: repo, toolchain, dev VM | done (0.9 open, non-blocking) | passed on Mac and laptop | 2026-09-28 |
| 1 | gVisor + gdb spike (labbase, sandbox spec, P0) | in progress: needs x86-64 P0 and single-lab | Mac: all local checks pass | 2026-09-29 |
| 2 | labd core (sessions, semaphore, reconciler) | blocked on 1 | — | — |
| 3 | Terminal gateway (WebSocket ↔ PTY) | blocked on 2 | — | — |
| 4 | Perf suite and measured capacity | blocked on 3 | — | — |
| 5 | Challenge pipeline and tier 1 content | blocked on 1 | — | — |
| 6 | Django web app | blocked on 3, 5 | — | — |
| 7 | Metrics, rollups, admin live view | blocked on 6 | — | — |
| 8 | Production on the VPS, tiers 2–3, beta | blocked on 4, 7 | — | — |

States: `not started`, `in progress`, `gate failing`, `done`, `blocked on N`.

## Open blockers

- None yet. Decisions awaiting the owner are in [QUESTIONS.md](QUESTIONS.md); each has a default that is in force.

## Measured numbers so far

Dev VM only (arm64, not authoritative; see [metrics/capacity.md](metrics/capacity.md) "Dev VM observations"). gVisor idle lab 14 MiB, gdb at prompt 23 MiB, start to prompt p95 513 ms warm, CPU quota exact. Every capacity estimate still stands until the laptop's x86-64 numbers arrive.

## Log

### 2026-09-29 — static lab binaries (ADR 0010)
- **Decision (owner):** gVisor cannot turn ASLR off: `personality(ADDR_NO_RANDOMIZE)` returns EINVAL, and host sysctls do not reach the sandbox. Every lab binary is now linked `-static -no-pie -fno-pie`, so code, globals and libc are fixed. Stack and heap still move; no exercise may depend on them. ADR 0010 lists the rejected options. CONVENTIONS and the Phase 5 plan now require `-static`. The spec is unchanged; the ADR overrides it.
- **perf image:** `perf` and `probe` are static. `build.sh` rejects a binary with an `INTERP` or `DYNAMIC` segment (tested against a dynamic build). Both binaries still build byte-identical twice. Measured size cost on arm64: text 4.8 KB → 48.7 KB, file 78 KB → 366 KB.
- **P0:** 3 new rows. `aslr-gdb-libc` and `aslr-direct-libc` check that `&printf` is identical over 3 runs, under gdb and run directly. `no-shared-libs` checks that `/proc/self/maps` has no `ld-musl` or `.so` mapping. All three pass under runsc and runc on the dev VM. `disable-randomization` and `aslr-gdb-stack` stay FALLBACK and cite ADR 0010.
- **Bug found by the static build:** `session.gdb` never stepped the `sum_scores` loop. In a dynamic binary, `display i` in `main` bound silently to a global `i` in `/lib/ld-musl-aarch64.so.1`, so the session passed by accident. It now `continue`s into `sum_scores` first and ends with `report key=42`.
- **Dev VM P0** (arm64, explicit run, committed as `docs/metrics/p0-2026-09-29-dev-vm.*`): runc 30 PASS, 0 FAIL. runsc 20 PASS, 3 FALLBACK, 7 FAIL, up from 14 PASS and 10 FAIL.
- **arm64 gdb under gVisor:** static binaries removed the loader crash, but 7 checks still fail. Three gVisor arm64 ptrace gaps were hiding behind the loader crash: stepping over a breakpoint (displaced stepping) crashes the program, the FP/SIMD register read returns EINVAL, and with displaced stepping off `next` runs to the end. Q13 updated; its default stands (Mac developers use runc for gdb work).
- **Gate on the Mac:** everything passes except the two x86-64 checks (output below). The gate's own P0 went to `.scratch/` and was not committed. `./run.sh check`, `./run.sh lint`: pass.
- Not re-run: the dev-VM single-lab. Its "at a breakpoint" memory under runsc may now be reachable on arm64, but only the laptop's numbers count.
- **Next:** the owner runs the laptop steps above. Then `capacity.md` gets the measured rows and Phase 1 is marked done.

Phase 1 gate on the Mac:
```
==> gate for phase 1 — 2026-09-29T16:36Z — macbook.local
==> [gate 1] images
  PASS  labbase builds
  PASS  labbase contents (images/labbase/test.sh)
  PASS  perf image builds; binaries reproducible
==> [gate 1] unit tests (spec golden file, invariants, cgroup parsing)
  PASS  run.sh test --all
==> [gate 1] P0 and single-lab run on this host
  PASS  P0 completes (runsc and runc)
==> [gate 1] authoritative results (x86-64, ADR 0001)
  FAIL  no P0 from an x86-64 host yet: on the laptop run LAB_HOST=linux-laptop ./run.sh perf --scenario P0, commit docs/metrics
  FAIL  no single-lab measurement from an x86-64 host yet: LAB_HOST=linux-laptop ./run.sh perf --scenario single-lab
==> GATE 1 FAILED. Fix the FAIL lines above; do not start the next phase.
```

### 2026-09-29 — laptop P0: gdb works under gVisor on x86-64
- **Laptop runsc P0:** every gdb check passes, except the two with fallbacks already in force:
  - Hardware watchpoints are accepted but never trigger, which is worse than a refusal. The `.gdbinit` default of 0 matters.
  - `personality(ADDR_NO_RANDOMIZE)` fails, so the stack moves; `&main` is fixed at 0x4013d2.
- Every sandbox check passes, with the same numbers as the dev VM. Q13 is now arm64-only.
- **Laptop single-lab (runsc):**
  - start to prompt p50 1450 ms, p95 1507 ms (target < 2 s);
  - memory at the gdb prompt 23.4 MiB and at a breakpoint 28.6 MiB (cgroup);
  - Sentry RSS 42 to 46 MiB;
  - session.gdb 3.2 s.
- **Laptop runc** failed everywhere with `exec /usr/bin/gdb: resource temporarily unavailable`. `RLIMIT_NPROC` (ADR 0009) counts every host process of uid 1000, which is the owner's desktop user. Reproduced on the dev VM with 40 processes owned by uid 1000.
- **Fix:** the process limit is runtime-aware. gVisor uses `RLIMIT_NPROC`; runc uses the cgroup only and sets no `RLIMIT_NPROC`. runc now passes 27 of 27 under that load. ADR 0009 amended; the golden spec now shows cgroup pids 128 for the gVisor default.
- Also fixed: the P0 hardware-watchpoint row says "accepted but never triggered", and single-lab no longer prints an `awk` error when the cgroup vanishes at the end of a hold.
- **Next:** the owner re-runs P0 and single-lab on the laptop so the committed files have valid runc columns, then commits and pushes `docs/metrics`.

### 2026-09-29 — first Phase 1 gate run on the laptop
- The owner's gate run failed for reasons outside the Phase 1 code: no access to the Docker socket (permission denied), a transient proxy.golang.org error, and P0 failing only because no images existed.
- Fixed: every image script goes through `docker_init` in `images/lib.sh`. It uses plain `docker` when the user can reach the socket, else `sudo docker` after one visible prompt with the permanent fix printed, else it says the daemon is down. Build containers run as the invoking user, so `images/out` never gets root-owned files. `doctor` reports Docker access. `run.sh test` pre-downloads Go modules with 3 retries. The gate's own P0 run now writes to a scratch directory, not `docs/metrics`.
- Tested: the sudo fallback with a fake `docker` in the VM, and the full gate on the Mac (same result as before: everything passes except the two x86-64 result checks).

### 2026-09-29 — Phase 1 tasks 1.1–1.8 on the dev VM
- **labbase:** Alpine 3.20 + gdb 14.2 + binutils + file. Only the spec's keep-list is on PATH (326 entries removed). 95.9 MB uncompressed, 30.6 MB gzipped, against a 45 MB target; Python is most of it (task 1.9). 50 content checks pass.
- **Sandbox spec + `BuildSpec`:** 14 ways of loosening it are rejected, and the golden file is pinned. `specrun` runs one container with a terminal and reports cgroup peaks. Verified under gVisor: uid 1000, no capabilities, read-only root, 16 MiB tmpfs, no network interfaces.
- **perf image:** perf and probe build reproducibly. **P0** runs 27 checks per runtime in about 30 s.
- **Findings** (arm64 VM):
  1. gdb under gVisor crashes the traced program in the musl loader, 10 of 10 runs (Q13).
  2. `personality(ADDR_NO_RANDOMIZE)` fails. Code and globals stay fixed thanks to `-no-pie`; the stack moves.
  3. The cgroup pids limit counts gVisor's own host tasks; a 32 limit killed the sandbox. Fixed with ADR 0009.
  4. gVisor's in-sandbox CPU clock over-reports; the host cgroup confirms the quota is exact.
  5. gVisor mounts its own synthetic `/sys`.
- **From the owner's commit `d322f44`:** `ctr run` needs root even with the containerd group, and `timeout` needs `--foreground` around `script`. Docs corrected; the rule is in ADR 0007.
- Go bumped to 1.26.6 (containerd v2.4.1 client). Provisioning installs it.
- Next: the owner runs the laptop steps above.

### 2026-09-28 — Phase 0 gate passes on the Linux laptop
- The owner ran Phase 0 on the laptop: `doctor` all required present, `vm up` a no-op, `vm verify` passes on x86_64, and the gate passes (output below). Environment in `docs/metrics/environment-linux-laptop.md`.
- ADR 0007 is confirmed on x86-64: gVisor without a terminal hangs (`timeout 25` exited 124), as on the arm64 VM. Terminal mode works on both, and labs always use it.
- Open: task 0.9 (second developer onboarding) and the laptop's CPU model for the environment file.

Phase 0 gate on the Linux laptop:
```
==> gate for phase 0 — 2026-09-28T01:21Z — linux-laptop
  PASS  run.sh check
  PASS  run.sh test --all
  PASS  run.sh vm verify prints runsc ok + cgroup2fs
  PASS  working tree clean
==> GATE 0 PASSED.
```

### 2026-09-27 — `vm verify` hung silently on the laptop
- On the laptop, `./run.sh vm verify` printed `cgroup2fs` and then nothing. Likely cause: the shell predates the `containerd` group membership that `vm up` added, so verify used `sudo ctr`. That call ran inside `script` (the PTY required by ADR 0007), which is a new terminal, so sudo asked for the password again, and the prompt was swallowed by the captured output. The Mac VM never showed this because Lima has passwordless sudo.
- Fix: verify asks for sudo once, visibly, before doing anything, and runs the whole `script` under sudo, so nothing inside can prompt. It prints each step to stderr. On a timeout or bad output it dumps diagnostics (task state, gVisor processes, runsc log, containerd journal for that container) and then cleans up. `VERIFY_TIMEOUT` overrides the 60 s limit.
- Tested in the VM: the normal path passes, and a simulated hang produces the diagnostics and leaves no containers or sandboxes. The gVisor container finishes in under 0.3 s.
- Still unconfirmed on the laptop: whether the sudo prompt was the whole story, or whether gVisor also hangs there with a terminal. The new diagnostics will show which.

### 2026-09-27 — doctor crash with shellcheck installed; laptop is Ubuntu 26.04
- On the laptop, `./run.sh doctor` stopped silently after the `jq` line. The cause: `shellcheck --version` has no number on its first line, so the generic version parser's `grep` failed, and `set -euo pipefail` aborted the script. It never showed up before because no tested host had shellcheck. Fixed: shellcheck gets its own parser, and a failed version lookup can no longer abort `doctor`. Verified in a container with shellcheck and inside the fully provisioned VM, which covers every Linux branch of `doctor`.
- The laptop runs **Ubuntu 26.04.1** (kernel 7.0), whose repositories ship only Postgres 18. That is what broke `vm up`. With the previous fix, an Ubuntu 26.04 container gets Postgres 16.15 from apt.postgresql.org (`resolute-pgdg`), and every base package exists on 26.04.
- New warning in `doctor` and `provision.sh` when a Docker or distro containerd package is installed: `vm up` replaces the running containerd daemon with the pinned build.
- Laptop specs: 16 vCPU, 15 GB RAM, `/dev/kvm` present. Too little RAM for the 100-lab run (QUESTIONS Q10 updated).

### 2026-09-27 — Postgres 16 on non-reference distros
- On the owner's Linux laptop, `./run.sh vm up` failed with `Unable to locate package postgresql-16`. `provision.sh` assumed Ubuntu 24.04, whose repositories ship Postgres 16; the laptop's distro does not.
- Fix: `provision.sh` detects the distro, refuses non-apt distros with a clear message, and adds the official PostgreSQL apt repository (apt.postgresql.org) when the distro lacks `postgresql-16`. Derivatives map to their Ubuntu or Debian base codename.
- Tested in containers: Debian 12, Ubuntu 22.04 and a Mint-style derivative get 16.15 from the PostgreSQL repo; Ubuntu 24.04 still uses the distro package. All are idempotent. Fedora is refused. The Mac's VM is unchanged.
- `doctor` now shows the distro and reports the Postgres server separately from the optional `psql` client.

### 2026-09-27 — remote added, CI removed
- Owner added a remote and pushed. `main` now holds every Phase 0 commit (fast-forward, linear history) plus the owner's "removed CI" commit.
- No GitHub Actions until the owner asks (ADR 0008). Phase 0 task 0.7 and Phase 5 task 5.13 were rewritten to use local `run.sh` commands and a local rebuild-changed helper.
- Commits pushed so far carry the placeholder author email `your.email@example.com`. Fixing them now means rewriting pushed history and force-pushing, which only the owner should decide. Setting `git config user.email` fixes future commits.
- Next: the owner runs Phase 0 on the Linux laptop (task 0.8). Phase 1 has not started.

### 2026-09-27 — Phase 0 tasks 0.1–0.7
- Committed the scaffold, then on `phase-0-bootstrap`: Go module with config loader and `/healthz` (0.2), Django 5.2 skeleton (0.3), Lima VM + `provision.sh` + `verify-runtime.sh` (0.4–0.6), CI workflow (0.7).
- Pinned: containerd 2.4.1, gVisor release-20260921.0, runc 1.5.2, Postgres 16, Go 1.26.4, uv 0.12.3, Django 5.2.17, Python 3.13.14.
- `vm up` from nothing took 1 min 34 s. `provision.sh` second run is a no-op. Go and Django tests pass on macOS arm64 and inside the Linux arm64 VM with `-race`.
- **Finding, ADR 0007:** gVisor containers started through containerd hang in `Create` unless they have a terminal. A shim goroutine dump shows it waiting on a pipe the sandbox inherited. It reproduces on two gVisor releases; runc is fine. labs always use a terminal, so the product path is unaffected, but P0 scripts and the challenge oracle must allocate a PTY. Not yet checked on x86-64.
- **Finding:** gVisor releases since 2026-08-31 ship as one tarball with a `gvisor-bin/` sidecar directory that must sit next to `runsc`.
- Dev hosts now get gcc (needed by `go test -race`); production still gets no compiler.
- Git author email on the Mac is the placeholder `your.email@example.com`; fix with `git config user.email` and amend before the first push.

Phase 0 gate on the Mac:
```
==> gate for phase 0 — 2026-09-27T21:29Z — macbook.local
  PASS  run.sh check
  PASS  run.sh test --all
  PASS  run.sh vm verify prints runsc ok + cgroup2fs
  PASS  working tree clean
==> GATE 0 PASSED.
```

### 2026-09-27 — two hosts, two developers
- Owner has a Linux x86-64 laptop: it becomes the reference environment (ADR 0001 amended, Q1 resolved). Both arm64 (Mac via Lima) and x86-64 must stay green.
- `run.sh` now works natively on Linux (`vm up` provisions the host, `vm ssh`/`verify` run locally) and gained `doctor`, a per-host dependency report with install hints; `check` is `doctor --strict`. Verified on the Mac and in a bare x86-64 Ubuntu container.
- Added `docs/ONBOARDING.md` for the second developer; Phase 0 gained tasks 0.8 (both hosts green + environment files) and 0.9 (second developer onboarded).
- New questions Q10 (laptop specs, where the 100-lab run happens), Q11 (challenge images arm64?), Q12 (second developer's OS).

### 2026-09-26 — project scaffolded
- Folder structure, CLAUDE.md, phase plan (0–8), conventions, security invariants, ADRs 0001–0006, `run.sh`, `Makefile`, Lima template, gate dispatcher created.
- Spec moved from the repo root to `docs/spec/mvp-spec.md`.
- Git initialised on `main`; nothing committed yet.
- Next: Phase 0, task 0.1.
