# 0013 — Production `max_sessions` is 100

Date: 2026-10-01 · Status: superseded by 0019 · Phase: 4 (task 4.11) · Refines: spec "Capacity estimates", plan 4.11

## Context

Phase 4 measured the lab platform at 100 concurrent labs on the x86-64 laptop (`docs/metrics/perf-report-2026-10-01-linux-laptop.json`; rows in `docs/metrics/capacity.md`).

| Input | Measured | Source |
| --- | --- | --- |
| Lab cgroup memory p95 at 100 readers | 25.5 MiB | `run-P2-2026-10-01-linux-laptop.json` |
| Whole host cost per lab, shim included | 50.2 MB (43.8 in the first P2 run: the desktop's idle memory moves between runs) | same |
| Host memory used with no lab (laptop, desktop running) | 4795 MB | same |
| Derived `max_sessions` at 25 % memory headroom on 32 GB | 394 = floor((32768 × 0.75 − 4795) / 50.2); 446 from the first run | same |
| CPU per lab: reader, stepper | 0.03 %, 0.39 % of a core | `run-P2-…`, `run-P8-…` |
| CPU per abuser lab | its full quota: 50 % of a core (`cpu_millicores: 500`) | `run-P3-2026-10-01-linux-laptop.json` |
| Host CPU at 100 mixed (60/30/10), 16 vCPUs | 32 % of all CPUs, about 5 cores, nearly all of it the 10 abusers | same |

Memory is not the limit: 100 labs use 3.5 to 5 GB above idle, far inside the 24 GB budget. CPU under abuse is. Each lab may use half a core, and an abusive learner will. The spec's own P3 mix is 10 % abusers. On the 8-vCPU VPS, 100 labs at that mix need about 5 cores for the abusers alone. At 200 labs they would need about 10, more than the box has, and every other lab's echo latency would suffer.

## Decision

- Production `max_sessions` is **100**, the spec's target. It is below the memory-derived 394, as plan 4.11 requires. `max_queue` stays 50.
- `deploy/labd.prod.yaml` carries these values. Phase 8's provisioning installs it as `/etc/labd/labd.yaml`.
- Raise it only after Phase 8, task 8.8, re-measures P2 and P3 on the VPS itself, and only together with one of these: a lower per-lab CPU quota (for example `cpu_millicores: 250`, after checking that gdb stays responsive), or measured beta data showing the abusive share is well under 10 %. `max_sessions` reloads without a restart (SIGHUP), so the dial is cheap to turn.

## Consequences

- The spec's success criterion "100 concurrent labs with ≥ 25 % memory headroom (measured)" holds on the laptop's numbers; Phase 8 confirms it on the VPS.
- The idle host baseline on the VPS (no desktop) will be lower than the laptop's 5 GB, so the memory-derived figure there will be higher still. That changes nothing here: CPU sets the limit.
- Revisit when Phase 7's dashboards show real usage (share of abusive sessions, CPU per lab), or if the VPS P3 shows other labs' echo latency rising beside abusers.
