# Capacity

Every row marked *est.* is the spec's working assumption and must be replaced by a measured value with its source. The MVP is not done while any *est.* remains. The production `max_sessions` is derived from this table (ADR to be written in Phase 4, task 4.11).

## Per lab

| Resource | Value | Host | Source |
| --- | --- | --- | --- |
| Memory: cgroup `memory.current` p95 | *est.* 60–80 MB | — | spec |
| Memory: gVisor Sentry RSS p95 | *est.* 30–50 MB | — | spec |
| Memory: total per lab p95 | *est.* ~130 MB | — | spec |
| CPU: idle | *est.* < 1 % of a core | — | spec |
| CPU: during `step` loops | *est.* 5–20 % of a core | — | spec |
| Disk: writable snapshot per session | *est.* ~1 MB | — | spec |
| Bandwidth: WS bytes/s per active terminal | *est.* 0.5–5 KB/s | — | spec |
| Start latency: create to prompt p95 | *est.* < 2 s (target) | — | spec |
| Echo latency p95 | *est.* < 100 ms (target) | — | spec |

## Host at 100 labs

| Resource | Value | Host | Source |
| --- | --- | --- | --- |
| Memory used | *est.* ~13 GB labs + ~3 GB system | — | spec |
| CPU | *est.* 1–2 cores average, 4 bursty | — | spec |
| Disk: images (200 challenges) | *est.* < 500 MB | — | spec |
| Disk: data per session-minute | *est.* 2–4 KB | — | spec |
| Bandwidth aggregate | *est.* < 1 Mbps | — | spec |

## gVisor overhead (runsc vs runc)

| Metric | runc | runsc | Delta | Host | Source |
| --- | --- | --- | --- | --- | --- |
| Memory per idle lab | *est.* | *est.* | *est.* < 40 MB | — | spec |
| `step` command latency | *est.* | *est.* | *est.* < 2× | — | spec |

## Derived

| Value | Formula | Result | Source |
| --- | --- | --- | --- |
| `max_sessions` at 25 % memory headroom | `floor((32768 × 0.75 − baseline_mb) / per_lab_p95_mb)` | *est.* ~180 | spec |
| Production `max_sessions` | ADR (Phase 4, task 4.11) | *est.* 100 | spec |

## Dev VM observations (arm64, not authoritative)

These do not replace any *est.* above (ADR 0001). They show the shape of the numbers until the x86-64 laptop results arrive. Source: `p0-2026-09-29-dev-vm.json`, `single-lab-2026-09-29-dev-vm.json`.

| Observation | gVisor (runsc) | runc |
| --- | --- | --- |
| Idle lab, cgroup memory | 14 to 15 MiB | 2 MiB |
| gdb at its prompt, cgroup memory | 22.9 MiB | 9.1 MiB |
| Program stopped at a breakpoint, cgroup memory | not reachable (arm64 gdb crash) | 13.6 MiB |
| Sentry process RSS, gdb at prompt | 39.6 MiB (RSS counts shared pages; the cgroup figure is the better per-lab cost) | n/a |
| Start to gdb prompt, p50 / p95, warm | 428 / 513 ms | 77 / 95 ms |
| Start to gdb prompt, cold first run | p50 1239, p95 1862 ms | not measured |
| Host tasks, idle lab | 20 to 21 | 6 |
| Busy loop with a 0.5 core quota, host CPU | 0.51 cores | 0.50 cores |

## Failures and decisions

None recorded yet. Format: `YYYY-MM-DD · scenario · criterion · measured · decision · link`.
