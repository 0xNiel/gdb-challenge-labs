# Phase 4 — Perf suite and measured capacity

| | |
| --- | --- |
| Depends on | Phase 3; the x86-64 Linux laptop (per-lab numbers) and, for the 100-lab run, a box with 32 GB (laptop if it has it, else the VPS in Phase 8; QUESTIONS.md Q10) |
| Unblocks | Phase 8 |
| Spec sections | "Local performance test suite" (entire), "Capacity estimates", "Success criteria" |
| Effort | one week plus run time (P9 alone is 2 hours) |

## Objective

This is the phase the project exists for. `labd-perf` drives `labd` through its real API and WebSocket with N scripted users, runs scenarios P1–P9, and writes `perf-report.json`. When this phase ends, every *est.* in `docs/metrics/capacity.md` is replaced by a measured number, and `max_sessions` for production is a derived value, not a guess.

## Design fixed by this document

- `labd/cmd/labd-perf` is a single binary with subcommands `run --scenario P2 --n 100 --ramp 5 --hold 20m --runtime runsc --out docs/metrics/`, `report --in <dir> --out perf-report.json --md capacity.md`, and `profiles` (prints the three profiles).
- **Profiles** (spec): `reader` 2 cmd/min from `list`, `info locals`, `bt`, 30 % idle; `stepper` 20 cmd/min `next`/`step`/`print` loops plus one `watch`; `abuser` runs the four abuse actions in sequence and then idles. Commands come from `images/perf/session.gdb` sections tagged `# profile: reader|stepper`.
- **Virtual user** = one goroutine: `POST /internal/sessions` (records create latency and queue position), dial WS with the returned token, send `gdb /opt/perf/perf`, wait for prompt (records start-to-prompt), run the profile until `hold` elapses, `quit`, `DELETE`. Records every command's echo latency and bytes in/out.
- **Collector** goroutine: `GET /internal/stats` every 5 s; host `/proc/meminfo`, `/proc/stat`, `/proc/pressure/*` (PSI); per-session cgroup `memory.current`, `cpu.stat`; `ps` RSS of `runsc-sandbox` and `containerd-shim-runsc-v1` processes summed per session; `labd` process RSS and goroutines; `ctr -n labs c ls` count every 30 s; `dmesg` grep for `oom` at the end.
- **Output** per run: `run-<scenario>-<date>-<host>.json` with raw series and percentiles; `report` merges the newest run of each scenario into `perf-report-<date>-<host>.json` matching the spec's schema, and renders `capacity.md`.
- **Runtime switch**: `--runtime runc` is accepted only when `labd.yaml` is the dev config that registers runc; the report labels the runc runs `reference`.
- **Scenarios** as in the spec table. Shell wrappers for the ones that need host actions: `p5.sh` (kill -9 during P5, reuse Phase 2's), `p7.sh` (flush content store: `ctr -n labs images rm`, `ctr content` prune, then `labd pull`, then start 10), `p9.sh` (P4 for 2 h with disk sampling: `du` of containerd root, journal size, `pg_total_relation_size` of `events` and `samples`).
- **Bandwidth (P8)**: measured at the WS layer (bytes in/out per session per second) and at the interface (`/proc/net/dev` delta on the loopback between Caddy and labd is not available in the VM without Caddy; measure WS bytes and note that TLS framing adds ~5 %). Phase 8 re-measures behind Caddy.

## Deliverables

| File | Purpose |
| --- | --- |
| `labd/cmd/labd-perf/main.go` | CLI |
| `labd/internal/perf/profile.go`, `vuser.go`, `collector.go`, `report.go`, `percentile.go` and tests | The driver |
| `labd/perf/p5.sh`, `p7.sh`, `p9.sh`, `runall.sh` | Wrappers; `runall.sh` runs P1–P9 in order and then `report` |
| `labd/labd.perf.yaml` | Config for the perf box: `max_sessions: 100`, `max_queue: 50` |
| `docs/metrics/run-P*-<date>-<host>.json` | Raw runs |
| `docs/metrics/perf-report-<date>-<host>.json` | Spec-schema report |
| `docs/metrics/capacity.md` | Replaced with measured numbers |
| `docs/decisions/00NN-max-sessions.md` | The production `max_sessions` value and why |

## Tasks

### 4.1 Percentiles and report schema
`percentile.go` with p50/p95/p99/max over `[]float64`; `report.go` structs mirroring the spec JSON exactly.
**Done when:** unit tests: percentiles on known inputs; report marshals to the exact key set in the spec (golden).

### 4.2 Profiles
Parse `session.gdb` into profile command lists; pacing with jitter ±20 %.
**Done when:** test: `reader` produces about 2 cmd/min over a simulated 10 min with fake clock; `stepper` about 20.

### 4.3 Virtual user
**Done when:** test against the Phase 3 fake server: a vuser completes a 30 s `stepper` run and reports N commands with latencies.

### 4.4 Collector
**Done when:** test with a fake `/proc` and cgroup directory tree (testdata) parses `memory.current`, `cpu.stat`, `meminfo`, `stat` correctly.

### 4.5 Scenario runner
`run` orchestrates ramp (`--ramp` sessions/s), hold, teardown, collection, and writes the raw JSON. Abuser actions: `shell while :; do :; done &` is not available (no `shell`?) — gdb `shell` exists, `sh` exists, so the abuser sends `shell sh -c 'while :; do :; done' &`, fork attempts `shell sh -c 'for i in $(seq 100); do sleep 60 & done'`, `shell dd if=/dev/zero of=/tmp/f bs=1M count=20`, and a 100 KB paste. Each is expected to be bounded by S7 and S13.
**Done when:** `labd-perf run --scenario P1 --n 1 --hold 1m` in the VM produces a JSON with all series non-empty.

### 4.6 Smoke on the dev VM
Run P1, P6 (150 into cap 100 → assert 50 queued, positions correct, FIFO drain), P4 (10 min), P5 on the arm64 dev VM with `max_sessions: 20`. These validate the driver, not the capacity.
**Done when:** all four runs exit 0 and are saved with host label `dev-vm`.

### 4.7 Prepare the x86-64 host
The Linux laptop is already provisioned (Phase 0, task 0.8; `dev` role registers runc for the reference run). Import `labbase` and `perf` images for `linux/amd64`. Confirm `docs/metrics/environment-linux-laptop.md` is current. Decide from its RAM whether the 100-lab scenarios (P2, P3, P8) fit with 25 % headroom; if not, run them at the largest N that fits and mark them `partial` in the report; the full run is repeated on the VPS in Phase 8, task 8.8.
**Done when:** `./run.sh vm verify` passes on the laptop and P0 from Phase 1 has passed there (this clears the `x86-64 P0 pending` warning).

### 4.8 Run P1–P9 on the perf box
`labd/perf/runall.sh`. Order: P1, P2, P3, P6, P4, P5, P7, P8, P9. Between scenarios, assert zero containers. If `/dev/kvm` exists, run P1 and P2 additionally with `platform = "kvm"` in `runsc.toml` and label the runs `kvm`.
**Done when:** every scenario has a `run-P*-<date>-<host>.json`.

### 4.9 Reference run with runc
P1, P2, P3 with `--runtime runc`.
**Done when:** three `run-P*-<date>-<host>-runc.json` files exist.

### 4.10 Report and capacity table
`labd-perf report` writes `perf-report-<date>-<host>.json` and rewrites `docs/metrics/capacity.md` with a table: per-lab memory (cgroup + Sentry, p50/p95/max), CPU, disk, bandwidth; host at 100; gVisor overhead; leaks; `derived.max_sessions_at_25pct_headroom = floor((32768 MB × 0.75 − baseline_mb) / per_lab_p95_mb)` where `baseline_mb` is host memory used with 0 labs.
**Done when:** `capacity.md` has no `est.` marker; every number has a source file name.

### 4.11 Decide `max_sessions`
Write `docs/decisions/00NN-max-sessions.md`: the derived value, the spec's 100, and the chosen production value (never above the derived value). Update `deploy/labd.prod.yaml` template.
**Done when:** the ADR exists and `labd.prod.yaml` matches it.

### 4.12 Evaluate pass criteria
Compare each scenario against the spec's pass criteria. For any failure, add a section "Failures and decisions" to `capacity.md`: what failed, by how much, and the decision (lower cap, tune limits, accept, or open a question).
**Done when:** every failing criterion has a decision line.

### 4.13 Wire the gate
**Done when:** `./run.sh gate --phase 4` exits 0.

## Tests

Unit: percentiles, report golden, profiles pacing, vuser against fake server, collector parsing. Smoke: P1/P4/P5/P6 on the dev VM. Full: P1–P9 on the x86-64 box plus runc reference.

## Gate

```
./run.sh gate --phase 4
```
1. `go test ./...` green (includes `internal/perf`).
2. `docs/metrics/perf-report-*-<host>.json` exists for an x86-64 host (not `dev-vm`), with every key of the spec schema non-zero where a value is expected (`leaks.*` may be zero). Scenarios run below N=100 for lack of RAM are marked `partial` and listed in "Failures and decisions" with the follow-up "repeat on VPS, Phase 8 task 8.8".
3. `docs/metrics/run-P{1..9}-*-<host>.json` all exist for that host.
4. `docs/metrics/capacity.md` contains no `est.`.
5. `docs/decisions/*-max-sessions.md` exists.
6. Every pass criterion either passes or has a decision line in `capacity.md` "Failures and decisions".

## Metrics to record

The spec's `perf-report.json` in full, plus: PSI `some avg10` for memory and CPU at 100 labs; `labd` RSS and goroutines at 100; containerd RSS at 100; Sentry RSS distribution; per-profile echo latency; queue wait times in P6; recovery time in P5; cold pull time vs warm start in P7; disk growth per hour in P9; `events` and `samples` rows per session-minute in P9.

## Non-goals

- Tuning gVisor beyond `platform` (that is post-MVP unless P2 fails badly).
- Caddy in the path (Phase 8 re-measures P8 behind Caddy on the VPS).

## Handoff

- STATUS.md log: headline numbers (per-lab p95 memory, host memory at 100, derived max sessions, echo p95, start p95).
- `docs/metrics/README.md` index updated with every file.
- If the derived `max_sessions` is below 100, update `docs/spec/mvp-spec.md` success criteria row via ADR, not by silent edit.
