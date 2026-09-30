# Capacity

Every row marked *est.* is the spec's working assumption and must be replaced by a measured value with its source. The MVP is not done while any *est.* remains. The production `max_sessions` is derived from this table (ADR to be written in Phase 4, task 4.11).

## Per lab

Memory values from `single-lab-*.json` are MiB (2^20 bytes), although the JSON fields end in `_mb`.

| Resource | Value | Host | Source |
| --- | --- | --- | --- |
| Memory: cgroup `memory.current` p95 | 25.8 MiB stopped at a breakpoint; 23.5 MiB at the gdb prompt (one lab, 30 samples) | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Memory: gVisor Sentry RSS p95 | 45.5 MiB at a breakpoint; 42.5 MiB at the prompt. RSS counts shared pages (the runsc binary), so it overstates the cost per lab; the cgroup row is the better figure | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Memory: total per lab p95 | 25.8 MiB (27.1 MB): the cgroup holds the whole sandbox, Sentry and gofer included. One lab, not yet under load; Phase 4 (P1–P3) re-measures at N labs | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| CPU: idle | *est.* < 1 % of a core | — | spec |
| CPU: during `step` loops | 0.53 % of a core average (host cgroup `usage_usec` over 600 s wall), one lab replaying `session.gdb` at 20 commands/min, 201 commands. Against the spec's *est.* 5–20 %. A person stepping faster costs more; Phase 4 measures at N labs | linux-laptop | `p1-2026-09-30-linux-laptop.json` |
| Disk: writable snapshot per session | *est.* ~1 MB | — | spec |
| Bandwidth: WS bytes/s per active terminal | 57 B/s out (terminal output), 3.6 B/s in (keystrokes), averaged over 600 s at 20 commands/min: 34162 and 2135 bytes. Against the spec's *est.* 0.5–5 KB/s. Payload only, no WebSocket or TLS framing | linux-laptop | `p1-2026-09-30-linux-laptop.json` |
| Start latency: create to prompt p95 | 1543 ms (p50 1463 ms, 10 runs); target < 2 s met. Container create to gdb's first prompt, not browser click to prompt | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Echo latency p95 | 5.5 ms (p50 3.2 ms, max 9.6 ms; 201 commands over 600 s): command sent to the first byte back over the WebSocket, one lab, loopback. Target < 100 ms met. Phase 4 re-measures at N labs | linux-laptop | `p1-2026-09-30-linux-laptop.json` |

## Host at 100 labs

| Resource | Value | Host | Source |
| --- | --- | --- | --- |
| Memory used | *est.* ~13 GB labs + ~3 GB system | — | spec |
| CPU | *est.* 1–2 cores average, 4 bursty | — | spec |
| Disk: base image (labbase + perf program) | 28.4 MiB as containerd stores it (compressed layers); every challenge shares the labbase layers | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Disk: images (200 challenges) | *est.* < 500 MB | — | spec; per-challenge layer sizes come in Phase 5 |
| Disk: data per session-minute | *est.* 2–4 KB | — | spec |
| Bandwidth aggregate | *est.* < 1 Mbps | — | spec |

## gVisor overhead (runsc vs runc)

| Metric | runc | runsc | Delta | Host | Source |
| --- | --- | --- | --- | --- | --- |
| Memory per lab, gdb at prompt, cgroup p95 | 12.7 MiB | 23.5 MiB | +10.8 MiB; target < 40 MB met | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Memory per lab, stopped at a breakpoint, cgroup p95 | 13.1 MiB | 25.8 MiB | +12.7 MiB | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Start to gdb prompt p95 | 466 ms | 1543 ms | +1077 ms (3.3×) | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| `step` command latency | *est.* | *est.* | *est.* < 2× | — | spec; measured per command in Phase 4. Early warning: a whole `gdb -batch -x session.gdb` run (gdb startup included) takes 425 ms under runc and 2723 ms under runsc, 6.4× (`single-lab-2026-09-29-linux-laptop.json`) |

## Derived

| Value | Formula | Result | Source |
| --- | --- | --- | --- |
| `max_sessions` at 25 % memory headroom | `floor((32768 × 0.75 − baseline_mb) / per_lab_p95_mb)` | *est.* ~180 | spec |
| Production `max_sessions` | ADR (Phase 4, task 4.11) | *est.* 100 | spec |

## Dev VM observations (arm64, not authoritative)

These do not replace any *est.* above (ADR 0001); the x86-64 rows above come from the laptop. Kept for comparison with arm64. Source: `p0-2026-09-29-dev-vm.json`, `single-lab-2026-09-29-dev-vm.json`.

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

Format: `YYYY-MM-DD · scenario · criterion · measured · decision · link`.

- 2026-09-30 · P1 (linux-laptop) · start latency p95 < 2 s · 2296 ms, one start, from the API request to `(gdb) ` over the WebSocket, including the client starting gdb · recorded, not blocking: the Phase 3 gate requires the number, not the pass. One start is not a p95. The container-to-prompt p95 over 10 runs is 1543 ms (`single-lab-2026-09-29-linux-laptop.json`). Phase 4 (P2) measures the start p95 over many starts and decides there · [p1-2026-09-30-linux-laptop.md](p1-2026-09-30-linux-laptop.md)
