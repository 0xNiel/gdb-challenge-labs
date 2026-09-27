# Project status

Update this file at the end of every working session. Keep it factual. Newest log entry at the top.

## Current phase

**Phase 0 — Bootstrap.** Not started. First task: [docs/plan/phase-0-bootstrap.md](plan/phase-0-bootstrap.md), task 0.1.

## Phase board

| Phase | Name | State | Gate result | Date |
| --- | --- | --- | --- | --- |
| 0 | Bootstrap: repo, toolchain, dev VM, CI | not started | — | — |
| 1 | gVisor + gdb spike (labbase, sandbox spec, P0) | blocked on 0 | — | — |
| 2 | labd core (sessions, semaphore, reconciler) | blocked on 1 | — | — |
| 3 | Terminal gateway (WebSocket ↔ PTY) | blocked on 2 | — | — |
| 4 | Perf suite and measured capacity | blocked on 3 | — | — |
| 5 | Challenge pipeline and tier 1 content | blocked on 1 | — | — |
| 6 | Django web app | blocked on 3, 5 | — | — |
| 7 | Metrics, rollups, admin live view | blocked on 6 | — | — |
| 8 | Production on the VPS, tiers 2–3, beta | blocked on 4, 7 | — | — |

States: `not started`, `in progress`, `gate failing`, `done`, `blocked on N`.

## Open blockers

- None yet. Decisions awaiting the owner are in [QUESTIONS.md](QUESTIONS.md); each has a default that is in force.

## Measured numbers so far

None. Every figure in [metrics/capacity.md](metrics/capacity.md) is still an estimate from the spec.

## Log

### 2026-09-27 — two hosts, two developers
- Owner has a Linux x86-64 laptop: it becomes the reference environment (ADR 0001 amended, Q1 resolved). Both arm64 (Mac via Lima) and x86-64 must stay green.
- `run.sh` now works natively on Linux (`vm up` provisions the host, `vm ssh`/`verify` run locally) and gained `doctor`, a per-host dependency report with install hints; `check` is `doctor --strict`. Verified on the Mac and in a bare x86-64 Ubuntu container.
- Added `docs/ONBOARDING.md` for the second developer; Phase 0 gained tasks 0.8 (both hosts green + environment files) and 0.9 (second developer onboarded).
- New questions Q10 (laptop specs, where the 100-lab run happens), Q11 (challenge images arm64?), Q12 (second developer's OS).

### 2026-09-26 — project scaffolded
- Folder structure, CLAUDE.md, phase plan (0–8), conventions, security invariants, ADRs 0001–0006, `run.sh`, `Makefile`, Lima template, gate dispatcher created.
- Spec moved from the repo root to `docs/spec/mvp-spec.md`.
- Git initialised on `main`; nothing committed yet.
- Next: Phase 0, task 0.1.
