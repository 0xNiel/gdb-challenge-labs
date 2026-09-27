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

## Failures and decisions

None recorded yet. Format: `YYYY-MM-DD · scenario · criterion · measured · decision · link`.
