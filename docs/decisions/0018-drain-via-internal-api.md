# 0018 — Drain and resume through labd's internal API, not an override file

Date: 2026-10-01 · Status: accepted · Phase: 7 (task 7.7) · Refines: plan 7 "Admin live", spec "kill switch"

## Context

Plan 7 has the admin page's Drain button write `max_sessions: 0` to a runtime override file and then call `POST /internal/reload`. In production, web and labd run as different users (S10), so a file that web writes and labd reads needs shared write permissions on a config path. labd also has no override-file mechanism today: a reload re-reads `labd.yaml` alone, so the file and its merge rules would be new code. The admin page also needs to know whether labd is draining, which a file does not tell it.

## Decision

- labd's internal API gains `POST /internal/drain` with `{"drain": true|false}`. It is bearer-authenticated and loopback-only like every other route (S11), and returns the new stats.
- While draining, labd's effective `max_sessions` is 0: nothing is admitted and running labs are not touched. New starts queue, and the queue timeout (2 min) abandons them. Resume restores the configured value.
- A SIGHUP or `/internal/reload` while draining updates the configured `max_sessions` and leaves the drain on.
- `GET /internal/stats` adds `draining`, `configured_max_sessions` and `pending_pull` (enabled challenges whose image is not in containerd). `max_sessions` reports the effective cap.
- The drain is in memory. A labd restart comes back not draining, which is what a reboot after a kernel update wants. An operator who must keep it drained across restarts sets `max_sessions: 0` in `labd.yaml`.

## Consequences

- No file is shared between the two users. One call does what took a file write plus a reload.
- One more internal API route, covered by the same auth tests.
