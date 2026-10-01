# Phase 7 — Metrics, rollups, admin live view

| | |
| --- | --- |
| Depends on | Phase 6 |
| Unblocks | Phase 8 (optional, ADR 0017) |
| Spec sections | "Metrics & analytics" (entire), "Data model" rows `samples`, `rollups_1m`, `rollups_1h`, "Web app → adminpanel", "Orchestrator → Internal HTTP API" (`/internal/stats`, `DELETE`) |
| Effort | one to two weeks |

## Objective

`labd` writes resource samples and all its events; `web` writes its events; a rollup command summarises them every minute and hour; Django renders the four dashboards; the admin live page shows sessions with kill buttons and a capacity gauge, and the kill switch (`max_sessions: 0` drain) is one click. Raw retention is enforced.

## As built (2026-10-01) — read this before the design below

- **Order**: 7.10 and 7.11 (the capacity search, ADR 0017) were built first, because the owner needs the VPS estimate most and P10 needs nothing else from this phase. Then 7.1–7.8; the gate last.
- **Sampler** (`labd/internal/metrics`): the spec's metrics plus `session.sentry_rss_mb`, `host.cpu_pressure` and `host.mem_pressure`. One `COPY` per tick. The `/proc` and cgroup readers moved here from labd-perf's collector, which now calls them. The gateway counts terminal bytes per session (`term.Server.Bytes`). The peak memory goes to `sessions.peak_rss_mb`.
- **Events**: labd and web already wrote every event (Phases 2 and 6). 7.2 and 7.3 added the tests. Emission stays where the transitions are (`manager.go`); there is no separate `events.go`.
- **Rollups** are web's own tables (Django migrations). ADR 0003 is amended to say so, and labd's migration `0002` grants web `DELETE` on `samples` and `events` for retention.
  - Percentiles are computed in Python with `percentile_cont`'s interpolation, so the tests run on SQLite.
  - Hour rows: exact count, sum, min and max; p50 count-weighted; p95 the largest minute p95, which errs high.
  - Each run re-rolls the last 10 minutes and 3 hours, partial buckets included.
- **Dashboards** are at `/admin/analytics/{live,usage,learning,capacity}` and refresh with a meta refresh: no HTMX is vendored.
  - Usage and Learning read raw `events` and `sessions` (exact within 90 days); charts over time read the rollups.
  - Charts are inline SVG (`analytics/svg.py`).
- **Drain** goes through `POST /internal/drain` (ADR 0018), not an override file. Stats add `draining`, `configured_max_sessions` and `pending_pull`.
- **Admin live** is at `/admin/live`: it refreshes every 10 s and has kill, drain and resume, and challenge toggles. An e2e test (`web/tests/e2e/test_admin.py`) kills a real lab and drains and resumes, in the real stack.
- **7.8** is `scripts/live-run.sh`, with the stack up. It writes the screenshot and `docs/metrics/data-per-session-*.{json,md}` (events and samples bytes per session-minute) and reports the kill.
- **Helpers**: `./run.sh manage ARGS` runs `manage.py` on the lab host against the dev Postgres; `deploy/systemd/{rollup,retention}.{service,timer}` are for Phase 8.

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

### 7.10 Learner profile (ADR 0017)
A `learner` profile in labd-perf that works the five real tier-1 labs, not the perf image. A script file, `labd/perf/learner.txt`, holds one episode per lab: a plain run of the binary; `gdb -q`; a look around (`list`, `info locals`, `bt`); the lab's own path to the fix (its `solve.gdb`); a watchpoint where the lesson teaches one; `quit`. It repeats, at about 6 commands a minute with 20 % idle. Shell lines wait for the shell's prompt, gdb lines for gdb's. Each learner works one lab, round-robin over the five. The command timings record `gdb_start`, `shell_run`, `run`, `next` and `watch` separately.
**Done when:** unit tests parse the script and drive a fake terminal through one episode; a 3-learner P10 run at a single count in the VM finishes with no command errors.

### 7.11 P10: capacity search on an 8-CPU host
`labd/perf/capacity.sh --steps "60 90 120 150 180" --hold 8m` runs P10 at each count: 90 % learners, 10 % abusers (perf image), labs under runsc. It stops early once a count fails a criterion. On the laptop, CPUs 8–15 are offline for the whole run (`--cpus 8`, through `/sys/devices/system/cpu/cpuN/online`; `--restore-cpus` after an interrupted run), so the host has 8, like the VPS. `labd-perf capacity` reads the run files and writes `docs/metrics/capacity-search-<date>-<host>.{json,md}`: one row per count, and the largest count that passes. Criteria per count:
- lab start p95 < 2 s;
- echo p95 < 100 ms;
- `next` p95 < 250 ms;
- no command errors and no OOM kills.

**Done when:** the record exists from the laptop with `cpus == 8`, x86-64, runsc.

### 7.12 The VPS estimate
Rewrite `docs/metrics/vps-capacity.md` from the P10 record: the measured learner CPU, memory per lab under learners, the CPU knee on 8 CPUs, and a margin for a VPS vCPU. Propose `max_sessions` for the VPS; if it differs from 100, write an ADR that supersedes ADR 0013.
**Done when:** `vps-capacity.md` has no *est.* in its CPU section.

### 7.9 Wire the gate
**Done when:** `./run.sh gate --phase 7` exits 0. (Done last, after 7.10–7.12.)

## Tests

Go unit: sampler parsing, event emission. Go integration: samples written. Django: rollup, retention, dashboards, admin views, event emission. Human: live run.

## Gate

```
./run.sh gate --phase 7
```
1. Go and Django test suites green; `makemigrations --check` clean.
2. Integration (`./run.sh test --integration`): with 2 labs running, `samples` holds every metric name and at least 10 distinct (`TestMetrics_SamplesFromTwoLabs`).
3. `./run.sh test --e2e`: lab 1 in the browser; a staff user kills a lab, drains and resumes. No container left afterwards.
4. `manage.py rollup --minute` and `--hour` on the dev Postgres (the e2e run's data) write rows; `retention` runs as role web.
5. STATUS.md has `Phase 7 human check: <date> <who> OK` and `docs/metrics/admin-live-*.png` exists.
6. A P10 capacity-search record from x86-64 with 8 CPUs online and labs under runsc (`docs/metrics/capacity-search-*.json`, task 7.11).
7. `docs/metrics/vps-capacity.md` has no *est.* left in its CPU section (task 7.12).

## Metrics to record

`events` and `samples` bytes per session-minute from the 20-lab run (`pg_total_relation_size` delta / session-minutes). Compare against the spec's 2–4 KB estimate and update `capacity.md` "Disk (data)".

## Non-goals

- Alerting. A dashboard is enough for MVP.
- Partitioning `events`/`samples` (spec: not in MVP).

## Handoff

- `docs/metrics/README.md` gets a section "What the dashboards show and where the numbers come from".
- STATUS.md updated.
