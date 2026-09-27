# 0006 — Phase gates are commands; perf failures require a recorded decision

Date: 2026-09-26 · Status: accepted · Phase: all

## Context

The plan will be executed across many sessions, possibly by a less capable model, with context lost between sessions. "Done" must be checkable without judgement, and perf targets in the spec are goals that measurement may prove wrong.

## Decision

- Each phase N has a gate `./run.sh gate --phase N` implemented in `scripts/gate.sh`. The checks are listed in the phase document; the script and the document are kept in sync.
- The next phase starts only after the gate exits 0 and its output is pasted into `docs/STATUS.md`.
- Correctness checks (tests, invariants, zero leaked containers) must pass. There is no override.
- Performance criteria (latency, memory, capacity) are measured and recorded. A miss is not a gate failure **if** the phase document allows it and a decision is recorded in `docs/metrics/capacity.md` under "Failures and decisions" (or an ADR for anything that changes a spec number). The gate checks that the decision exists.
- Human checks are allowed only where a phase document names them; they are recorded as a line in STATUS.md with date and initials, and the gate greps for that line.
- Unimplemented gate sections exit non-zero with a message pointing at the phase document, so a gate can never pass by accident.

## Consequences

- Progress is legible from STATUS.md alone.
- Some friction: a session cannot "just start" the next phase. That is the point.
