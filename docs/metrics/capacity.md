# Capacity

Every row marked *est.* is the spec's working assumption and must be replaced by a measured value with its source. The MVP is not done while any *est.* remains. The production `max_sessions` is derived from this file (Phase 4, task 4.11).

Three parts: single-lab numbers from Phases 1 and 3 (hand-written); the Phase 4 numbers at N labs, which `labd-perf report --in docs/metrics --host <host>` writes between the markers below; and the rows still estimated, each removed once the report measures it.

## Single lab (Phases 1 and 3)

Memory values from `single-lab-*.json` are MiB (2^20 bytes), although the JSON fields end in `_mb`.

| Resource | Value | Host | Source |
| --- | --- | --- | --- |
| Memory: cgroup `memory.current` p95 | 25.8 MiB stopped at a breakpoint; 23.5 MiB at the gdb prompt (one lab, 30 samples) | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Memory: gVisor Sentry RSS p95 | 45.5 MiB at a breakpoint; 42.5 MiB at the prompt. RSS counts shared pages (the runsc binary), so it overstates the cost per lab; the cgroup row is the better figure | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Memory: total per lab p95 | 25.8 MiB (27.1 MB): the cgroup holds the whole sandbox, Sentry and gofer included. One lab; the Phase 4 block below measures at N labs, including each lab's shim | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| CPU: during `step` loops | 0.53 % of a core average (host cgroup `usage_usec` over 600 s wall), one lab replaying `session.gdb` at 20 commands/min, 201 commands. The spec estimated 5–20 %. A person stepping faster costs more | linux-laptop | `p1-2026-09-30-linux-laptop.json` |
| Bandwidth: WS bytes/s per active terminal | 57 B/s out (terminal output), 3.6 B/s in (keystrokes), averaged over 600 s at 20 commands/min: 34162 and 2135 bytes. The spec estimated 0.5–5 KB/s. Payload only, no WebSocket or TLS framing | linux-laptop | `p1-2026-09-30-linux-laptop.json` |
| Start latency: create to prompt p95 | 1543 ms (p50 1463 ms, 10 runs); target < 2 s met. Container create to gdb's first prompt, not browser click to prompt | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Echo latency p95 | 5.5 ms (p50 3.2 ms, max 9.6 ms; 201 commands over 600 s): command sent to the first byte back over the WebSocket, one lab, loopback. Target < 100 ms met | linux-laptop | `p1-2026-09-30-linux-laptop.json` |
| Disk: base image (labbase + perf program) | 28.4 MiB as containerd stores it (compressed layers); every challenge shares the labbase layers | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |

gVisor overhead, one lab:

| Metric | runc | runsc | Delta | Host | Source |
| --- | --- | --- | --- | --- | --- |
| Memory per lab, gdb at prompt, cgroup p95 | 12.7 MiB | 23.5 MiB | +10.8 MiB; target < 40 MB met | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Memory per lab, stopped at a breakpoint, cgroup p95 | 13.1 MiB | 25.8 MiB | +12.7 MiB | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Start to gdb prompt p95 | 466 ms | 1543 ms | +1077 ms (3.3×) | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |
| Whole `gdb -batch -x session.gdb` run (gdb startup included) | 425 ms | 2723 ms | 6.4×: an early warning for the per-command number below | linux-laptop | `single-lab-2026-09-29-linux-laptop.json` |

## At N labs (Phase 4)

<!-- labd-perf report: begin (generated; edit outside these markers) -->
Not generated yet: the Phase 4 runs happen on the x86-64 laptop (`labd/perf/runall.sh`).
<!-- labd-perf report: end -->

## Still estimated

Each row goes once the block above carries its measured number.

| Resource | Estimate | Replaced by |
| --- | --- | --- |
| CPU: idle lab | *est.* < 1 % of a core | P2 (readers) |
| Disk: writable snapshot per session | *est.* ~1 MB | P2 disk samples |
| Memory used at 100 labs | *est.* ~13 GB labs + ~3 GB system | P2 |
| CPU at 100 labs | *est.* 1–2 cores average, 4 bursty | P3 |
| Disk: images (200 challenges) | *est.* < 500 MB | labbase + 200 × program layer (P2 disk samples); real layers in Phase 5 |
| Disk: data per session-minute | *est.* 2–4 KB | P9 |
| Bandwidth aggregate | *est.* < 1 Mbps | P8 |
| `step` command latency, runsc against runc | *est.* < 2× | P1 and P1 with runc |
| `max_sessions` at 25 % memory headroom | *est.* ~180 | P2 (`labd-perf report`) |

## Production `max_sessions`

| Value | Source |
| --- | --- |
| *est.* 100 (the spec's target) | ADR from Phase 4, task 4.11 |

## Dev VM observations (arm64, not authoritative)

These replace no estimate (ADR 0001); the x86-64 rows above come from the laptop. Kept for comparison with arm64. Source: `p0-2026-09-29-dev-vm.json`, `single-lab-2026-09-29-dev-vm.json`.

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
