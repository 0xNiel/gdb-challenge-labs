# Metrics

Every measured number in the project lands here. Files carry the date and the host, e.g. `p1-2026-10-12-dev-vm.json`. Nothing here is edited by hand except this index and the parts of `capacity.md` outside the block `labd-perf report` generates.

## Hosts

| Label | What it is | Authoritative? |
| --- | --- | --- |
| `dev-vm` | Lima VM on the Apple Silicon Mac, arm64, 4 vCPU / 8 GB | No. Correctness and trends only |
| `linux-laptop` | Owner's Linux x86-64 laptop (specs in `environment-linux-laptop.md`, QUESTIONS Q10) | Yes, for Phases 1 and 4; 100-lab runs only if RAM allows |
| `hostinger` | The production VPS, 8 vCPU / 32 GB | Yes, final |

## Files (index; keep updated)

| File | Phase | What |
| --- | --- | --- |
| `capacity.md` | 1, 3, 4, 7, 8 | The capacity table from the spec, estimates replaced by measurements as they arrive |
| `environment-dev-vm.md`, `environment-linux-laptop.md`, `environment-hostinger.md` | 0, 4, 8 | Kernel, containerd, runsc, cgroup mode, CPU, RAM, KVM presence |
| `p0-<date>-<host>.{json,md}` | 1 | gdb feature matrix under runsc and runc |
| `single-lab-<date>-<host>.{json,md}` | 1 | One idle lab: RSS, Sentry RSS, start latency, image size |
| `create-latency-<date>-<host>.md` | 2 | Container create-to-running from P4-lite |
| `p1-<date>-<host>.{json,md}` | 3 | Single-session 10-minute stepper profile |
| `run-P<n>-<date>-<host>[-runc\|-kvm].json` | 4 | Raw scenario runs from `labd-perf run`: meta, summary, criteria evaluated, timeline, every simulated user, host samples every 5 s. `-runc` is the gVisor reference, `-kvm` the runsc KVM platform |
| `perf-report-<date>-<host>.json` | 4, 8 | The spec-schema report from `labd-perf report` (`meta.sources` names the run behind each value); it also rewrites the generated block of `capacity.md` |
| `challenges-<date>.md` | 5 | Per-challenge layer size, build and oracle time |
| `web-<date>-<host>.md` | 6 | Click-to-prompt and Django request latency |
| `admin-live-<date>.png` | 7 | Screenshot of the live view during the 20-lab run |
| `beta-week1.md` | 8 | First week of real users |

## Conventions

- JSON is the source; Markdown is a rendering for humans. Both are committed.
- Percentiles are `p50`, `p95`, `p99`, `max` unless the spec's schema says otherwise.
- Memory in MB (10^6 bytes) unless the field name says `mib`. Latency in ms. Bandwidth in bytes per second.
- Every Markdown table has a "Source" column or a line under it naming the JSON file and the command that produced it.


## What the dashboards show and where the numbers come from

Phase 7. Staff only, at `/admin/analytics/…` and `/admin/live`.

| Page | Shows | Source |
| --- | --- | --- |
| Live | slots in use against `max_sessions`, queued, host memory; active and queued over 2 hours | `GET /internal/stats` now; `rollups_1m` (`labd.active`, `labd.queued`, max per minute) |
| Usage | per day for 30 days: labs started, unique users, median session length, start latency p50/p95 | `events` (`lab_started` and its `start_latency_ms`, from request to running), `sessions` (ended − started) |
| Learning | funnel per tier; per challenge: started, solved, median time to solve, hints per learner; commands in the last 5 minutes of labs ended without a solve | `events` (`lab_started`, `challenge_solved.time_to_solve_s`, `lab_ended`, `command_entered`), `progress` |
| Capacity | memory per lab (24 h by minute, 30 days by hour), peak concurrent labs per day, host memory and CPU | `rollups_1m`, `rollups_1h` of `session.rss_mb`, `labd.active`, `host.mem_used_mb`, `host.cpu_pct` |
| `/admin/live` | each live lab (user, challenge, state, age, memory, commands), kill, drain/resume, pending pulls, challenge toggles | `GET /internal/sessions`, `GET /internal/stats`, `POST /internal/drain` |

Where the raw numbers come from:
- labd's sampler writes `samples` every `metrics_flush_s` (10 s). Host: memory used, CPU %, CPU and memory pressure, containerd's disk, active and queued. Per lab: cgroup memory, CPU ms since the last tick, Sentry RSS, terminal bytes in and out.
- `manage.py rollup` (every minute) builds `rollups_1m` from samples and events, and `rollups_1h` from those.
- `manage.py retention` (daily) keeps samples 7 days, and events and `rollups_1m` 90 days. `rollups_1h` are kept forever.
- Percentiles over many labs are p50 and p95 of every lab's readings in the bucket. Hour p95 is the largest minute p95, so it errs high.
