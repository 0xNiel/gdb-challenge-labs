# Capacity of the target VPS

How many concurrent labs fit on the target box: **8 vCPU, 32 GB RAM, 400 GB NVMe, 32 TB transfer a month** (Hostinger KVM, x86-64). ADR 0017 explains why this is answered on the laptop and not on the VPS.

Status: **measured, 2026-10-01** (task 7.12). CPU comes from P10, run on the x86-64 laptop with 8 CPUs online (`capacity-search-2026-10-01-linux-laptop.md`). Memory, bandwidth and disk come from measured per-lab costs. Values marked *est.* are not measured; none is in the CPU section.

## Answer

| Bound | Concurrent labs | Basis |
| --- | --- | --- |
| CPU, 8 cores, 10 % of labs running a busy loop | **at least 150; the limit was not reached** | P10 passed every criterion at 60, 90, 120 and 150 labs. It stopped at 150 because the laptop has 15 GB of RAM |
| Memory, typical use | **about 425** | measured 50.2 MB per lab; *est.* 3.2 GB for everything else; 25 % headroom |
| Memory, every lab at its 128 MiB limit | **about 166** | the sandbox's own limit, a hard upper bound on use; 10 % headroom |
| Bandwidth | thousands | about 0.25 TB a month at 400 labs, under 1 % of 32 TB |
| Disk | thousands | about 98 GB of data at 400 labs around the clock, a quarter of 400 GB |

**Recommendation: `max_sessions` 120** (ADR 0019, superseding ADR 0013's 100). It is the measured 150 less a 20 % margin (*est.*) for a VPS vCPU being slower than the laptop's cores and for noisy neighbours. At 120 the laptop's host CPU was 80 % (p95), so the margin is also the CPU headroom it had there. 120 is below the worst-case memory bound of 166, so even labs that all fill their memory limit fit.

What the numbers say:
- **CPU did not run out of interactive capacity at 150.** The host CPU was saturated (98 % p95), almost all of it by the 15 busy loops: 15 × 0.5 core = 7.5 of 8. Learners stayed responsive because each lab is its own cgroup with equal weight. Echo p95 rose from 2.0 to 9.7 ms, against a 100 ms criterion.
- **A learner is cheap: about 0.2 % of a core.** The first estimate assumed 1 to 5 %. Without busy loops, CPU is not the limit at all; memory is.
- **Memory caps the box**: about 425 labs in typical use, about 166 if every lab filled its limit.
- **Two dials move the answer**: the share of busy loops, and `cpu_millicores`. See [Dials](#dials).

## Memory

| Input | Value | Source |
| --- | --- | --- |
| Whole host cost per lab: sandbox, gofer, containerd shim | 50.2 MB (measured at 100 labs) | `run-P2-2026-10-01-linux-laptop.json` |
| Lab cgroup `memory.current` p95 under P10's learners and abusers | 29.2 to 29.8 MiB at every count; max 33.9 MiB | `run-P10-2026-10-01-linux-laptop-n{60,90,120,150}.json` |
| Lab cgroup `memory.current` p95 in Phase 4 | 25.5 MiB idle at gdb; 29.6 MiB in the mixed P3 run | `run-P2-…`, `run-P3-2026-10-01-linux-laptop.json` |
| Cost outside the cgroup per lab (shim and the like) | about 23.5 MB: 50.2 MB − 26.7 MB | derived from the P2 rows above |
| Host memory used as P10 added labs | 7160 MB at 60 labs, 10298 MB at 150: 34.9 MB per added lab. Below 50.2 MB, so 50.2 stays the figure. P10's own "host per lab" column (37 to 116 MB) moves with the laptop's desktop and is not used | `capacity-search-2026-10-01-linux-laptop.json` |
| Everything that is not a lab on the VPS | *est.* 3.2 GB: Postgres 2.2 GB (Phase 8's `shared_buffers=2GB`), OS 0.5 GB, gunicorn 4 workers 0.3 GB, Caddy, labd and containerd idle 0.2 GB | the laptop's 4.8 GB includes a desktop, so it is not used |
| A lab's memory limit | 128 MiB (134 MB), which includes its `/tmp` and home tmpfs (gVisor keeps tmpfs in the sandbox's memory) | `default_limits` in `deploy/labd.prod.yaml`; tmpfs accounting *est.*: no run has filled the tmpfs and read the cgroup |

- Typical: (32768 × 0.75 − 3200) / 50.2 = 21376 / 50.2 = **425**.
- Worst case, every lab at its limit: (32768 × 0.90 − 3200) / (134 + 23.5) = 26291 / 157.5 = **166**. Reaching it needs every learner to fill memory at once. The kernel would then OOM-kill whole sandboxes, not the host. The limit is per lab, so a lower `memory_mb` raises this bound.

## CPU

P10 on `linux-laptop`: i5-13450HX, x86-64, CPUs 8–15 offline. The 8 online were CPUs 0–7: four 4.6 GHz performance cores with their hyperthreads. Labs under runsc. 90 % learners working the five real tier-1 labs (`labd/perf/learner.txt`: about 6 commands a minute, 20 % idle), 10 % abusers running a busy loop at their quota. 8 minutes at each count.

| Labs | Lab start p95 | Echo p95 | `next` p95 | gdb start p95 | `run` p95 | Host CPU p95 | CPU pressure max | Learner CPU per lab | Abuser CPU per lab | Source |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 60 | 274 ms | 2.0 ms | 8 ms | 932 ms | 29 ms | 42 % | 55.0 | 0.19 % of a core | 50.0 % | `run-P10-2026-10-01-linux-laptop-n60.json` |
| 90 | 232 ms | 2.5 ms | 6 ms | 399 ms | 29 ms | 61 % | 57.1 | 0.18 % | 50.0 % | `run-P10-2026-10-01-linux-laptop-n90.json` |
| 120 | 258 ms | 4.9 ms | 12 ms | 366 ms | 44 ms | 80 % | 69.3 | 0.18 % | 50.0 % | `run-P10-2026-10-01-linux-laptop-n120.json` |
| 150 | 259 ms | 9.7 ms | 18 ms | 379 ms | 69 ms | 98 % | 83.6 | 0.17 % | 49.6 % | `run-P10-2026-10-01-linux-laptop-n150.json` |

Criteria at every count: lab start p95 < 2 s, echo p95 < 100 ms, `next` p95 < 250 ms, no command timed out, no OOM kill, every simulated user completed. **All four counts passed all of them**, with 0 command timeouts in 19945 commands and 0 OOM kills. Host CPU is % of all 8 online CPUs; pressure is `/proc/pressure/cpu` some avg10. Summary: `capacity-search-2026-10-01-linux-laptop.{json,md}`.

Where the CPU goes, from the same runs:

| Labs | Abusers × their CPU | Learners × their CPU | Host CPU p95 in cores | The rest: labd, containerd, the perf driver, the desktop |
| --- | --- | --- | --- | --- |
| 60 | 6 × 0.500 = 3.00 cores | 54 × 0.0019 = 0.10 | 0.415 × 8 = 3.32 | 0.22 core |
| 90 | 9 × 0.500 = 4.50 | 81 × 0.0018 = 0.15 | 0.612 × 8 = 4.90 | 0.25 |
| 120 | 12 × 0.500 = 6.00 | 108 × 0.0018 = 0.19 | 0.805 × 8 = 6.44 | 0.25 |
| 150 | 15 × 0.496 = 7.44 | 135 × 0.0017 = 0.23 | 0.985 × 8 = 7.88 | 0.21 |

- **Busy loops take the CPU, learners barely touch it.** At 150, 135 learners used a quarter of one core between them; 15 abusers used 7.4 cores.
- **The knee was not found.** At 150 the abusers had the CPU they were allowed, and the box had none spare. Past that, CPU is shared, not exhausted: each lab is its own cgroup with equal weight, so the next busy loop takes a little from every other busy loop, and an interactive lab still gets the slice it asks for. Latency did grow as the box filled: echo p95 2.0 → 9.7 ms, `next` p95 8 → 18 ms, `run` p95 29 → 69 ms, from 60 to 150 labs. All are far inside their criteria.
- **Learners' own cost is 0.17 to 0.19 % of a core**, including gdb restarts, `run` and watchpoints. The first estimate, before P10, was 1 to 5 %. Phase 4's synthetic stepper, at 20 commands a minute, used 0.39 % (`run-P8-2026-10-01-linux-laptop.json`).
- **Lab start p95 stayed near 260 ms** (request to the shell's prompt) at every count. Phase 4's 1.8 s (`run-P2-…`) is request to gdb's prompt during a 5-per-second ramp; P10 ramps at 2 a second and starts gdb later, timed separately (gdb start p95 above).
- The 15 GB laptop could not run 180 (it needs about 15 GB), so 150 is the largest count measured, not the limit.

## Margin for a VPS vCPU

The laptop is not the VPS, and three differences cut capacity. None is measured.

- **A VPS vCPU is probably slower** than a 4.6 GHz performance-core thread: *est.* 20 to 30 % less work per second. A busy loop still takes only its half core, because the quota is time, not work. Learners and gVisor's own work take longer.
- **Noisy neighbours**: a shared host steals time (`steal` in `/proc/stat`). Unknown until a VPS is rented.
- **web, Postgres and Caddy serve real users** on the VPS. P10 ran labd and the driver, not the web app: *est.* half a core.

Applying a 20 % margin to the measured 150 gives **120**; 30 % gives 105. The recommended `max_sessions` is 120. If a VPS is rented, Phase 8's task 8.8 re-runs P10 there and replaces this margin with a measured one.

## Dials

| Dial | Now | Effect, from P10's per-lab costs |
| --- | --- | --- |
| Share of labs running a busy loop | 10 % in P10 (the spec's P3 mix); real usage unknown until beta users exist | Each abuser costs 0.5 core, each learner 0.002. At 5 % busy loops, 150 labs need about 4 cores of the 8 for them. At none, CPU stops being the limit, and memory (166 to 425) decides |
| `cpu_millicores` | 500 (`deploy/labd.prod.yaml`) | 250 halves what a busy loop costs: 150 labs at 10 % would need 3.75 cores for them, not 7.5. Not yet run: P10 at 250 must first show gdb stays responsive at that quota |
| `memory_mb` | 128 | Lower raises the worst-case memory bound of 166. P10's lab memory never passed 34 MiB |

Raise `max_sessions` above 120 only with one of these: a measured busy-loop share well under 10 %, a lower `cpu_millicores` checked by P10, or P10 on the VPS itself. It reloads without a restart (SIGHUP).

## Bandwidth

| Input | Value | Source |
| --- | --- | --- |
| Terminal traffic per active lab | 61 B/s out, 3.4 B/s in, payload; about +5 % for TLS and WebSocket framing | `run-P8-2026-10-01-linux-laptop.json` |
| Page weight per session | *est.* 0.3 MB: the lab page, xterm.js (290 KB, about 75 KB compressed by Caddy, cached after the first visit) | `web/static/vendor/xterm-5.5.0/` |

At 400 labs all month, with 30-minute sessions: terminals 400 × 68 B/s ≈ 70 GB, pages 800 sessions an hour × 0.3 MB ≈ 175 GB. That is about **0.25 TB a month, under 1 % of 32 TB.** At 120 labs it is under 0.1 TB.

Worst case: output is capped at 256 KiB/s per connection (S13). Using 32 TB in a month takes 12 MB/s around the clock: about 48 labs streaming at the cap without pause. Each would also be cut by the 15-minute idle timeout unless someone keeps typing. Bandwidth does not limit this box.

## Disk

| Input | Value | Source |
| --- | --- | --- |
| Images | labbase 28.1 MiB plus about 0.25 MiB per challenge: about 80 MB for 200 challenges | `challenges-2026-10-01-linux-laptop.md`, `run-P2-…` |
| Writable snapshot per lab | 0.01 MB: labs write only to tmpfs, which is memory | `run-P2-…` |
| `events` per session-minute | 1474 B (table, indexes and TOAST), 20 labs for 5 minutes | `data-per-session-2026-10-02-linux-laptop.json` |
| `samples` per session-minute | 5324 B, same run, labd's sampler every 10 s. It includes the host rows, which do not grow with labs, so at more labs it is a little lower | `data-per-session-2026-10-02-linux-laptop.json` |
| Journal | 8 MB an hour at 100 labs churning | `run-P9-2026-09-30-linux-laptop.json` |

Every lab slot busy around the clock is 1440 session-minutes a day each. Retention keeps `samples` 7 days and `events` 90 days (`manage.py retention`).

| Labs | `events` a day | `samples` a day | `events` kept (90 days) | `samples` kept (7 days) | Data at steady state |
| --- | --- | --- | --- | --- | --- |
| 120 | 172800 × 1474 B = 255 MB | 172800 × 5324 B = 920 MB | 22.9 GB | 6.4 GB | **about 29 GB** |
| 400 | 576000 × 1474 B = 849 MB | 576000 × 5324 B = 3.07 GB | 76.4 GB | 21.5 GB | **about 98 GB** |

The first estimate put `samples` at 1.5 KB per session-minute; the measured 5.3 KB more than triples it, and `samples` is still the smaller part because it is kept 7 days, not 90. Images, the journal and the rollup tables add little. **Even at 400 labs around the clock, data stays under a quarter of 400 GB.** Disk does not limit this box.
