# Phase 7 — Metrics, rollups, admin live view

| | |
| --- | --- |
| Depends on | Phase 6 |
| Unblocks | Phase 8 |
| Spec sections | "Metrics & analytics" (entire), "Data model" rows `samples`, `rollups_1m`, `rollups_1h`, "Web app → adminpanel", "Orchestrator → Internal HTTP API" (`/internal/stats`, `DELETE`) |
| Effort | one to two weeks |

## Objective

`labd` writes resource samples and all its events; `web` writes its events; a rollup command summarises them every minute and hour; Django renders the four dashboards; the admin live page shows sessions with kill buttons and a capacity gauge, and the kill switch (`max_sessions: 0` drain) is one click. Raw retention is enforced.

## Design fixed by this document

- **`labd` sampler** `internal/metrics`: every `metrics_flush_s` (10 s) read host `/proc/meminfo`, `/proc/stat` (CPU % since last), `/proc/pressure/memory`, disk used of containerd root; per session `memory.current` and `cpu.stat usage_usec` delta from `/sys/fs/cgroup/labs/<id>/`; WS bytes in/out from the bridge counters. One batched `INSERT` into `samples`. Metric names exactly as the spec lists. `session.rss_mb` is `memory.current`; add `session.sentry_rss_mb` from the `runsc-sandbox` process RSS (found via the shim's pid file) because Phase 4 showed it matters. `sessions.peak_rss_mb` updated on end.
- **Events**: `labd` emits `lab_requested, lab_queued, lab_started (start_latency_ms), lab_ended (reason, duration_s, commands), command_entered`. `web` emits `user_signed_up, user_logged_in, hint_viewed, flag_submitted, challenge_solved`. One `events` table, two writers, one schema; `web` writes with its own role.
- **Rollups**: `manage.py rollup --minute` and `--hour`. `rollups_1m(bucket_ts, metric, dims jsonb, count, sum, min, max, p50, p95)`. Dims: `{}` for host metrics, `{"challenge": slug}` for per-session metrics aggregated across sessions, `{"type": ...}` for event counts. Percentiles via `percentile_cont` in SQL. Idempotent per bucket (`ON CONFLICT DO UPDATE`). `--hour` rolls up from `rollups_1m`. `manage.py retention` deletes `samples` older than 7 days, `events` older than 90 days, `rollups_1m` older than 90 days.
- **Dashboards** (`analytics` app, staff only, HTMX refresh 30 s): Live, Usage, Learning, Capacity, exactly the spec's four with the listed panels. Charts via a small inline SVG helper (`analytics/svg.py`: line and bar) — no Chart.js unless SVG becomes painful; if adopted, vendor it.
- **Admin live** (`adminpanel`): table from `GET /internal/sessions` joined with users and challenges; kill button → `DELETE /internal/sessions/{id}` with `admin_kill`; capacity gauge from `/internal/stats`; "Drain" button writes `max_sessions: 0` to a runtime override file and calls `/internal/reload`; "Resume" restores. Challenge enable/disable toggles; "N challenges pending pull" banner from `/internal/stats.pending_pull`.
- **systemd** units for `rollup.timer` (every minute) and `retention.timer` (daily) written now into `deploy/systemd/`, used in Phase 8.

## Deliverables

| File | Purpose |
| --- | --- |
| `labd/internal/metrics/sampler.go`, `cgroup.go`, `proc.go` and tests | Collection |
| `labd/internal/orch/events.go` | Event emission at each transition |
| `web/analytics/management/commands/rollup.py`, `retention.py` | Aggregation and retention |
| `web/analytics/views.py`, `svg.py`, templates | Dashboards |
| `web/adminpanel/` | Live view, kill, drain, toggles |
| `deploy/systemd/rollup.{service,timer}`, `retention.{service,timer}` | Timers |
| `web/analytics/tests/`, `labd/internal/metrics/*_test.go` | Tests |
| `docs/metrics/admin-live-<date>.png` | Screenshot of the live page during the 20-lab run |

## Tasks

### 7.1 Sampler
**Done when:** unit tests with a testdata `/proc` and cgroup tree; integration test in the VM: with 2 sessions running for 30 s, `samples` has rows for every metric name in the spec plus `session.sentry_rss_mb`.

### 7.2 Event emission in labd
**Done when:** unit test with the fake store: a session lifecycle produces `lab_requested → lab_started → lab_ended` with the right `data` keys; queued path adds `lab_queued`.

### 7.3 Event emission in web
**Done when:** tests assert each of the five web events is written by its action.

### 7.4 Rollup command
**Done when:** tests on fixture data: minute buckets have correct count/sum/min/max/p50/p95 (hand-computed); running twice does not duplicate; hourly derives from minute rows.

### 7.5 Retention command
**Done when:** test: rows older than the window are deleted, newer kept, rollups untouched except `rollups_1m` beyond 90 days.

### 7.6 Dashboards
**Done when:** view tests render each dashboard with fixture rollups and return 200 for staff, 403 for non-staff; SVG helper unit tests produce valid XML.

### 7.7 Admin live view, kill, drain
**Done when:** view tests with fake labd client: list renders; kill calls `DELETE` with `admin_kill`; drain writes the override and calls reload; non-staff 403. Integration in the VM: kill a real session and see the container gone.

### 7.8 Twenty-lab live run
Run `labd-perf run --scenario P2 --n 20 --hold 5m` in the VM while a human watches the admin live page: gauge moves, per-session RSS updates, killing one session ends that vuser (the driver logs `state ended reason admin_kill`). Take a screenshot into `docs/metrics/`.
**Done when:** STATUS.md records the check and the screenshot exists.

### 7.9 Wire the gate
**Done when:** `./run.sh gate --phase 7` exits 0.

## Tests

Go unit: sampler parsing, event emission. Go integration: samples written. Django: rollup, retention, dashboards, admin views, event emission. Human: live run.

## Gate

```
./run.sh gate --phase 7
```
1. Go and Django test suites green.
2. Integration: after a 60 s run with 2 sessions, `SELECT count(DISTINCT metric) FROM samples` ≥ 10.
3. `manage.py rollup --minute` on that data produces rows; `--hour` produces rows.
4. STATUS.md has `Phase 7 human check: <date> <who> OK` and the screenshot exists.

## Metrics to record

`events` and `samples` bytes per session-minute from the 20-lab run (`pg_total_relation_size` delta / session-minutes). Compare against the spec's 2–4 KB estimate and update `capacity.md` "Disk (data)".

## Non-goals

- Alerting. A dashboard is enough for MVP.
- Partitioning `events`/`samples` (spec: not in MVP).

## Handoff

- `docs/metrics/README.md` gets a section "What the dashboards show and where the numbers come from".
- STATUS.md updated.
