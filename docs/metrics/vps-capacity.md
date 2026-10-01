# Capacity of the target VPS

How many concurrent labs fit on the target box: **8 vCPU, 32 GB RAM, 400 GB NVMe, 32 TB transfer a month** (Hostinger KVM, x86-64). ADR 0017 explains why this is answered on the laptop and not on the VPS.

Status: **first estimate, 2026-10-01.** Memory, bandwidth and disk come from measured per-lab costs. CPU, the resource that runs out first, rests on two unmeasured inputs. Phase 7's P10 run measures both on the laptop with 8 CPUs online, and this file is then rewritten from it. Values marked *est.* are not measured.

## Answer so far

| Bound | Concurrent labs | Basis |
| --- | --- | --- |
| Memory, typical use | **about 425** | measured 50.2 MB per lab; *est.* 3.2 GB for everything else; 25 % headroom |
| Memory, every lab at its 128 MiB limit | **about 166** | the sandbox's own limit, a hard upper bound on use; no headroom beyond 10 % |
| CPU | **about 80 to 130 at 75 % CPU, if 10 % of learners run a busy loop**; about 300 if none do | *est.*: depends on a learner's real CPU cost and on the share of busy loops; see below |
| Bandwidth | thousands | about 0.25 TB a month at 400 labs, under 1 % of 32 TB |
| Disk | thousands | under 80 GB of data at 400 labs, mostly 90 days of `events` |

**Current recommendation: 100** (`max_sessions`, ADR 0013). It is safe on every row above. The real limit is probably between 100 and about 170: CPU sets it, and memory caps it at about 166 if labs are ever allowed to fill their memory limits. P10 decides where in that range. It may come out lower if gdb under gVisor costs more CPU than the synthetic runs suggest.

## Memory

| Input | Value | Source |
| --- | --- | --- |
| Whole host cost per lab: sandbox, gofer, containerd shim | 50.2 MB (measured at 100 labs) | `run-P2-2026-10-01-linux-laptop.json` |
| Lab cgroup `memory.current` p95 | 25.5 MiB idle at gdb; 29.6 MiB in the mixed P3 run | `run-P2-…`, `run-P3-2026-10-01-linux-laptop.json` |
| Cost outside the cgroup per lab (shim and the like) | about 23.5 MB: 50.2 MB − 26.7 MB | derived from the two rows above |
| Everything that is not a lab on the VPS | *est.* 3.2 GB: Postgres 2.2 GB (Phase 8's `shared_buffers=2GB`), OS 0.5 GB, gunicorn 4 workers 0.3 GB, Caddy, labd and containerd idle 0.2 GB | the laptop's 4.8 GB includes a desktop, so it is not used |
| A lab's memory limit | 128 MiB (134 MB), which includes its `/tmp` and home tmpfs (gVisor keeps tmpfs in the sandbox's memory) | `default_limits` in `deploy/labd.prod.yaml`; tmpfs accounting *est.*, P10 checks it |

- Typical: (32768 × 0.75 − 3200) / 50.2 = **425**.
- Worst case, every lab at its limit: (32768 × 0.90 − 3200) / (134 + 23.5) = **166**. Reaching it needs every learner to fill memory at once. The kernel would then OOM-kill whole sandboxes, not the host. The limit is per lab, so a lower `memory_mb` raises this bound.

## CPU

| Input | Value | Source |
| --- | --- | --- |
| Reader (2 gdb commands a minute, 30 % idle) | 0.03 % of a core | `run-P2-2026-10-01-linux-laptop.json` |
| Stepper (20 commands a minute: next, step, print, one watch) | 0.39 % of a core | `run-P8-2026-10-01-linux-laptop.json` |
| A busy loop (abuser) | 50 % of a core: its quota (`cpu_millicores: 500`) | `run-P3-2026-10-01-linux-laptop.json` |
| A learner working a real lab: restarting gdb, `run` again and again, software watchpoints | ***est.* 1 to 5 % of a core.** Not measured. Starting gdb and running a program under gVisor cost far more than stepping: a whole scripted gdb session takes 6.4× longer than under runc | `single-lab-2026-09-29-linux-laptop.json`; P10 measures it |
| Share of labs running a busy loop | ***est.* 10 %**: the spec's P3 mix. Real users are probably fewer; nobody has measured | P3 mix; Phase 7 dashboards will show it |
| CPU for labs | *est.* 5.5 of 8 vCPU: 75 % of the box, less about half a core for web, Postgres and labd | |

Labs at the 75 % budget = 5.5 / (0.9 × learner + 0.1 × 0.5):

| Learner cost | 10 % busy loops | No busy loops |
| --- | --- | --- |
| 1 % of a core | 93 | 611 (memory caps it first) |
| 2 % | 81 | 305 |
| 5 % | 58 | 122 |

Beyond the budget, CPU is shared, not exhausted: every lab is its own cgroup with equal weight, so a busy loop cannot take more than its half core, and interactive labs need little CPU. Going past the budget should first show as slower lab starts (gVisor's boot is CPU-heavy) and slower `run`, then slower echo. That is what P10 measures: it raises the count until a criterion breaks.

Two dials move this table:
- `cpu_millicores: 250` halves a busy loop's cost: at a 2 % learner and 10 % busy loops, 81 becomes 128. P10 also checks gdb stays responsive at that quota.
- A lower busy-loop share, from real usage, moves toward the right-hand column.

## Bandwidth

| Input | Value | Source |
| --- | --- | --- |
| Terminal traffic per active lab | 61 B/s out, 3.4 B/s in, payload; about +5 % for TLS and WebSocket framing | `run-P8-2026-10-01-linux-laptop.json` |
| Page weight per session | *est.* 0.3 MB: the lab page, xterm.js (290 KB, about 75 KB compressed by Caddy, cached after the first visit) | `web/static/vendor/xterm-5.5.0/` |

At 400 labs all month, with 30-minute sessions: terminals 400 × 68 B/s ≈ 70 GB, pages 800 sessions an hour × 0.3 MB ≈ 175 GB. That is about **0.25 TB a month, under 1 % of 32 TB.**

Worst case: output is capped at 256 KiB/s per connection (S13). Using 32 TB in a month takes 12 MB/s around the clock: about 48 labs streaming at the cap without pause. Each would also be cut by the 15-minute idle timeout unless someone keeps typing. Bandwidth does not limit this box.

## Disk

| Input | Value | Source |
| --- | --- | --- |
| Images | labbase 28.1 MiB plus about 0.25 MiB per challenge: about 80 MB for 200 challenges | `challenges-2026-10-01-linux-laptop.md`, `run-P2-…` |
| Writable snapshot per lab | 0.01 MB: labs write only to tmpfs, which is memory | `run-P2-…` |
| `events` per session-minute | 1.02 KB (labd's events and command lines) | `run-P9-2026-09-30-linux-laptop.json` |
| `samples` per session-minute | *est.* 1.5 KB (four metrics every 10 s); Phase 7 measures it | |
| Journal | 8 MB an hour at 100 labs churning | `run-P9-…` |

At 400 labs around the clock: about 1.4 GB of data a day. Retention (`samples` 7 days, `events` 90 days) holds that under 70 GB, plus journal and Postgres overhead: **under 80 GB of 400 GB.**

## What P10 will measure (Phase 7, tasks 7.10–7.12)

- The **learner** profile: per lab, a plain run, `gdb -q`, a look around (`list`, `info locals`, `bt`), the lab's own path to the fix (its `solve.gdb`), a watchpoint where the lesson teaches one, `quit`, again. About 6 commands a minute, 20 % idle.
- **P10**: runs at increasing counts, for example 60, 90, 120, 150 and 180, with 90 % learners and 10 % busy loops. It runs on the laptop with CPUs 8 to 15 offline (`sudo chcpu -d 8-15`), labs under runsc. At each count it records: learner CPU, host CPU, CPU pressure, memory, lab start p95, gdb start p95, `run` p95, `next` p95, echo p95.
- **The answer** is the largest count at which every criterion holds:
  - lab start p95 < 2 s;
  - echo p95 < 100 ms;
  - `next` p95 < 250 ms;
  - no command times out.

  It is then reduced by a margin for a VPS vCPU being slower than the laptop's cores (*est.* 20 to 30 %, to be replaced by a measured figure if a VPS is ever rented).
