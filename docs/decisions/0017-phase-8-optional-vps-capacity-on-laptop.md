# 0017 — Phase 8 is optional; the VPS's capacity is measured on the laptop shaped as the VPS

Date: 2026-10-01 · Status: accepted · Phase: 7 (affects 8) · Refines: IMPLEMENTATION_PLAN "Why this order" goal 1, plan 8.8, ADR 0013

## Context

The owner will not deploy to the VPS soon and has made Phase 8 optional. What matters most now is the project's first goal: a measured answer to how many concurrent labs the target VPS can run. The target is 8 vCPU, 32 GB RAM, 400 GB NVMe and 32 TB of transfer a month.

Phase 4 measured cost per lab on the x86-64 laptop. Phase 8 task 8.8 was to re-measure on the VPS itself, and that is no longer scheduled. Two gaps remain:

- **CPU, which sets the limit** (ADR 0013). The laptop has 16 logical CPUs, not 8. And Phase 4's per-lab CPU comes from synthetic readers and steppers on the perf image. No run has replayed what a learner does in the real labs: start gdb repeatedly, `run` the program, use software watchpoints.
- **The VPS's shape.** Nothing has measured 8 CPUs saturated by labs, so it is unknown at what count interactive latency degrades.

The laptop can approximate the VPS: Linux can take CPUs offline at run time (`chcpu -d`). With 8 logical CPUs online, the whole host (labd, containerd, Postgres, every sandbox) competes for 8 CPUs, as on the VPS. Its 15 GB of RAM limits a run to about 180 labs. That covers the range where CPU is expected to run out, and memory per lab is already measured.

## Decision

- **Phase 8 is optional.** It no longer blocks anything, and the phase board says "optional (deferred by the owner)". Its tasks stay in its document for when a VPS exists. The MVP "done" list keeps its VPS items, marked deferred.
- **Phase 7 gains three tasks** (7.10–7.12 in its document):
  - a `learner` profile in labd-perf that works the five real tier-1 labs;
  - a capacity-search scenario, P10. It ramps a learner and abuser mix through increasing counts and records where each criterion breaks;
  - `docs/metrics/vps-capacity.md`: the estimate for the target VPS, with every input named.
- **The authoritative P10 run is the laptop with 8 CPUs online**, labs under runsc (ADR 0001). Gate 7 requires that record.
- `docs/metrics/vps-capacity.md` holds the estimate from today's numbers now and is replaced by the measured curve once P10 has run. Each value is labelled *measured* or *est.*

## Consequences

- An 8-CPU laptop is not the VPS. Its i5-13450HX mixes fast and efficient cores, and a VPS vCPU is a shared hyperthread whose speed and neighbours are unknown. The estimate keeps a margin for that and names it. If a VPS is rented later, Phase 8's 8.1 (environment) and 8.8 (re-measure) are the check, and are short.
- Production `max_sessions` stays 100 (ADR 0013) until P10 says otherwise. A change then gets its own ADR.
- Revisit when a VPS exists, or if real usage (Phase 7's dashboards) differs from the learner profile.
