# 0019 — Production `max_sessions` is 120

Date: 2026-10-01 · Status: accepted (owner to confirm: QUESTIONS.md Q15) · Phase: 7 (task 7.12) · Supersedes: 0013

## Context

ADR 0013 set 100 from Phase 4, when CPU under abuse was the expected limit and no run had put 8 CPUs under labs. Phase 7's P10 (ADR 0017) measured that on the x86-64 laptop with 8 CPUs online, labs under runsc, 90 % learners on the five real tier-1 labs and 10 % busy loops (`docs/metrics/capacity-search-2026-10-01-linux-laptop.md`):

| Input | Measured | Source |
| --- | --- | --- |
| Largest count meeting every criterion | 150, the largest run; no count failed. 180 would not fit in the laptop's 15 GB | `capacity-search-2026-10-01-linux-laptop.json` |
| At 150: host CPU p95, echo p95, `next` p95, lab start p95 | 98 %, 9.7 ms, 18 ms, 259 ms | `run-P10-2026-10-01-linux-laptop-n150.json` |
| At 120: host CPU p95, echo p95 | 80 %, 4.9 ms | `run-P10-2026-10-01-linux-laptop-n120.json` |
| CPU per learner | 0.17 to 0.19 % of a core | the four P10 runs |
| Memory bound if every lab fills its 128 MiB | 166 labs | `docs/metrics/vps-capacity.md` |

A VPS vCPU is probably slower than the laptop's performance-core threads and may lose time to neighbours. `vps-capacity.md` keeps a margin of 20 to 30 % for that, an estimate.

## Decision

- Production `max_sessions` is **120**: the measured 150 less a 20 % margin. `max_queue` stays 50.
- `deploy/labd.prod.yaml` carries it, and `labd/internal/config/config_test.go` pins it.
- 120 keeps the worst-case memory bound (166) above the cap, so a box where every lab fills its memory limit still fits.
- Raise it only with one of: a measured busy-loop share well under 10 % (beta data), a lower `cpu_millicores` re-checked by P10, or P10 on the VPS itself (Phase 8, task 8.8). It reloads with SIGHUP.

## Consequences

- 20 % more learners at once than ADR 0013 allowed, on the same box.
- If the VPS is much slower than the margin assumes, echo latency rises first; at 150 on the laptop it was 10 % of its target, so there is room. Phase 8's task 8.8 checks it on the VPS.
- The 30 % margin (105) is the fallback if the owner prefers it; Q15 asks.
