# 0011 — `sessions` records the challenge slug, not web's challenge id; four extra columns

Date: 2026-09-29 · Status: accepted · Phase: 2 · Refines: spec "Data model" (`sessions` row)

## Context

The spec lists `sessions(id, user_id, challenge_id, state, created_at, started_at, ended_at, end_reason, container_id, peak_rss_mb, commands)`. labd writes this table (ADR 0003), but labd never learns web's numeric challenge id. The internal API passes `{user_id, challenge_slug}` (spec, "Internal HTTP API"), `challenges.json` is keyed by slug, and the `events` table already uses `challenge_slug`. labd's migration also runs before web's, and web's `challenges` table belongs to the `web` role, so a foreign key from `sessions` to it cannot be created by labd.

Task 2.9 (reconciler) and the admin live view need a few facts the spec's column list lacks.

## Decision

- `sessions.challenge_slug text NOT NULL` replaces `challenge_id`. Django's unmanaged model (Phase 6) joins on `challenges.slug`.
- `user_id bigint NOT NULL` has no foreign key, for the same ownership reason. It matches Django's default `BigAutoField`.
- Additions to the spec's columns:
  - `image text`: the image reference the lab ran, so an incident can be traced to a digest.
  - `extended boolean`: whether the one extension was used (spec state machine), so an adopted session cannot extend twice.
  - `state` has a CHECK constraint listing exactly the spec's seven states.
  - `end_reason text NOT NULL DEFAULT ''` holds the spec's reasons plus labd's own: `task_exited`, `queue_timeout`, `create_failed`, `reconciled` (constants in `labd/internal/orch/session.go`).
- Migrations live in `labd/internal/store/migrations/` (embedded; the plan listed both that path and `labd/migrations/`, and code outside `internal/` and `cmd/` breaks CONVENTIONS). A `schema_migrations` table records what ran.
- The migration grants `web` SELECT on `sessions` and `samples` and SELECT, INSERT on `events` when the `web` role exists.

## Consequences

- Phase 6's unmanaged `Session` model uses `challenge_slug`; the Phase 6 plan should say so when it is written against this schema.
- Renaming a challenge slug orphans its session history. Slugs are stable by design (they also key the flag, ADR 0005).
