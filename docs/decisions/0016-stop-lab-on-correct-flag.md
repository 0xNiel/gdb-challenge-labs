# 0016 — A correct flag stops the lab

Date: 2026-10-01 · Status: accepted · Phase: 6 · Overrides: spec "Start-a-lab flow" step 8 ("The lab can be stopped or kept open independently of the flag")

## Context

After the Phase 6 human check the owner asked that a lab stop as soon as its flag is accepted: once solved there is nothing left to do in it, and a running lab holds one of the 100 slots (ADR 0013) until it idles out after 15 minutes. labd already accepts `solved` as a stop reason on `DELETE /internal/sessions/{id}`, and the lab page already explains it ("the challenge was solved").

## Decision

- When web accepts a correct flag for the first time, it stops the user's live session for that challenge with reason `solved`. labd ends it, records `lab_ended {reason: solved}`, and sends `state: ended, reason: solved` to the page, which shows that the lab ended and links back to the challenge.
- A wrong flag, a resubmission after the solve, or a solve with no running lab stops nothing.
- The flag counts even if the stop fails (labd unreachable, session already gone): the failure is logged, and the lab then ends by its idle timeout as before.
- The learner can start the lab again from the challenge page to look around; nothing else changes.

## Consequences

- Slots free up sooner; per-session `duration_s` now ends at the solve, which Phase 7's "median session length" will show.
- A learner who wanted to keep experimenting after the flag must start a new session (about half a second to the prompt, `docs/metrics/web-2026-10-01-linux-laptop.md`).
- The e2e test now expects the session to end with `solved` right after the flag, instead of being stopped by hand.
