# Phase 6 — Django web app

| | |
| --- | --- |
| Depends on | Phases 3 and 5 |
| Unblocks | Phase 7 |
| Spec sections | "Web app (Django)" (entire), "Start-a-lab flow" steps 1–3, 5, 8, "Data model" (web-owned tables), "Terminal gateway → Auth, Client", "Flag mechanism" (submission side) |
| Effort | two to three weeks |

## Objective

A user signs up, sees the tier 1 curriculum, reads a lesson, starts a lab, debugs in xterm.js in the browser, submits the flag, and sees the next challenge unlock. Admin uses Django admin for challenges and users. Analytics and the live admin view come in Phase 7.

## Design fixed by this document

- Django 5.2 LTS, Python 3.13, `uv` (ADR 0002). Apps exactly as the spec table. Settings split `base/dev/test/prod`.
- **Auth**: `django-allauth` email-only (QUESTIONS Q4). Console email backend in dev and test; provider in Phase 8. Email verification `mandatory` in prod settings, `optional` in dev.
- **Models** (web-owned): `Tier(slug, title, order)`, `Lesson(tier, slug, title, body_md, order)`, `Challenge(tier, slug, title, difficulty, order, image_digest, limits jsonb, hints jsonb, tags, estimated_minutes, enabled, lesson, solution_md)`, `Progress(user, challenge, state, solved_at, hints_used, attempts)`, `FlagAttempt(user, challenge, ts, correct)`. labd-owned `Session` and `Event` as `managed = False` models (ADR 0003) with a read-only DB role in prod.
- **Import**: `manage.py import_challenges challenges.json --repo <path>` upserts tiers, lessons (from `lesson.md` beside each manifest; the lesson slug is the challenge slug), challenges; disables challenges absent from the file; never deletes. Idempotent.
- **Unlock rules**: first challenge of tier 1 unlocked at signup; solving challenge N unlocks N+1 in the tier; solving the last unlocks the first of the next tier. `progress` rows are created lazily.
- **Flag submission**: `POST /lab/<slug>/flag`. Normalise: strip whitespace. Compare with `hmac.compare_digest` against `flag.derive(settings.DEPLOY_SECRET, slug)`. Rate limit 10 attempts per challenge per 10 minutes per user, stored in `FlagAttempt` (count rows in window; no cache dependency). Record `FlagAttempt` and event `flag_submitted`; on success `Progress.solved`, event `challenge_solved` with `time_to_solve_s` (first `lab_started` for this user+challenge to now), `hints_used`, `sessions_used`.
- **labd client** `labs/labd_client.py`: `start(user_id, slug)`, `stop(session_id, reason)`, `token(session_id, user_id)`, `list()`, `stats()`. Bearer from `LABD_INTERNAL_SECRET`. Timeout 3 s on start (spec). In test settings a fake client is injected.
- **Token minting** moves to `web`: `GET /lab/<slug>/session/token` re-checks the Django session and that the session row belongs to the user, then mints with `WS_TOKEN_KEY` (same algorithm as Phase 3; test vectors in `challenges/schema/ws_token_vectors.json` shared with Go). `labd` stops minting in `POST /internal/sessions` (remove; ADR 0004 amendment noted in Phase 3 README).
- **Lab page** `/lab/<slug>/session`: terminal left, tabs right (lesson, source with line numbers rendered server-side, hints, flag form, attempts). JS is the Phase 3 dev page adapted: fetch token, connect, handle frames, TTL countdown, Extend at 2 min, Stop button, queued position banner, "Commands are recorded" notice once per session (S19). xterm.js vendored under `web/static/vendor/`.
- **Markdown**: `markdown-it-py` + `nh3` sanitiser; code blocks highlighted server-side with Pygments-free CSS only (no extra dependency).
- **Pages**: exactly the spec list. `/dashboard` shows per-tier completion, solved count, time spent (sum of `sessions` durations), hints used, streak (days with a solve).

## Deliverables

| File | Purpose |
| --- | --- |
| `web/config/settings/{base,dev,test,prod}.py` | Settings; every secret from env |
| `web/accounts/` | allauth wiring, signup template, profile |
| `web/curriculum/` | models, import command, `/learn` views, markdown renderer |
| `web/labs/` | labd client, start/stop/token views, lab page, JS |
| `web/progress/` | flag.py, submission view, unlock service, dashboard |
| `web/analytics/`, `web/adminpanel/` | empty apps registered (Phase 7 fills) |
| `web/templates/` | base layout, HTMX partials |
| `web/static/vendor/xterm-5.x/` | vendored |
| `web/tests/` | unit, view, and e2e (Playwright) |
| `web/README.md` | how to run, import, test |

## Tasks

### 6.1 Settings, database, models, migrations
**Done when:** `manage.py migrate` on Postgres in the VM succeeds; `manage.py makemigrations --check` clean; unmanaged `Session`/`Event` models query the labd tables.

### 6.2 Flag derivation in Python
**Done when:** `pytest web/progress/tests/test_flag.py` passes the 10 vectors from `flag_vectors.json`.

### 6.3 Import command
**Done when:** tests: import creates 5 challenges and 5 lessons; re-import changes nothing; removing one from the JSON disables it; changing a digest updates it.

### 6.4 Accounts
**Done when:** view tests: signup creates user and unlocks tier1-01; login; logout; password reset sends a console email.

### 6.5 Curriculum pages
**Done when:** view tests: anonymous redirected; `/learn` shows tiers with lock states; lesson renders markdown safely (script tag stripped test).

### 6.6 Lab start/stop/token
**Done when:** view tests with fake labd client: start when unlocked → redirect to session page; start when locked → 403; start with existing session → same session; stop → client called with `user_stop`; token endpoint refuses another user's session; token verifies with Go (cross-check via vectors).

### 6.7 Lab page and JS
Adapt Phase 3 dev page. **Human check** in the VM against real labd and `01-off-by-one`.
**Done when:** the human solves lab 1 in the browser via the Django page; recorded in STATUS.md.

### 6.8 Flag submission and unlocks
**Done when:** tests: correct flag → solved, next unlocked, events written; wrong flag → attempt recorded, not solved; 11th attempt in 10 min → 429; solving the last in tier unlocks next tier's first; constant-time compare used (assert `compare_digest` in source via a unit test of `flag.check`).

### 6.9 Hints
**Done when:** revealing hint N records `hint_viewed` and increments `hints_used`; hints render in order and cannot be revealed out of order; boss challenge shows "no hints".

### 6.10 Dashboard
**Done when:** view test with fixture data shows the right totals.

### 6.11 Django admin
Register all web models; `Challenge` list with enable toggle; `Progress` read-only.
**Done when:** admin loads for a staff user in a view test.

### 6.12 End to end
Playwright test in the VM (`web/tests/e2e/test_tier1.py`): sign up, open lab 1, start, type the oracle commands from `solve.gdb` into the terminal with pacing, read the flag from the terminal output, submit, assert lab 2 unlocked. Then stop and assert the session row is `ended/user_stop`.
**Done when:** `./run.sh test --e2e` passes in the VM.

### 6.13 Wire the gate
**Done when:** `./run.sh gate --phase 6` exits 0.

## Tests

Unit: flag, unlock service, rate limit, import, markdown sanitiser, token minting vectors. View: every page and action with fake labd. E2E: tier 1 lab 1 through the real stack. Human: lab page usability.

## Gate

```
./run.sh gate --phase 6
```
1. `uv run ruff check . && uv run pytest -q` green.
2. `manage.py makemigrations --check` clean.
3. Python flag vectors and WS token vectors pass.
4. `./run.sh test --e2e` green in the VM.
5. STATUS.md has `Phase 6 human check: <date> <who> OK`.
6. Template test asserts the command-recording notice is present on the lab page (S19).

## Metrics to record

Django request latency p95 for `/lab/<slug>` start (from the e2e run) and the total click-to-prompt time seen by the browser (the spec's success criterion: p95 < 2 s). Record in `docs/metrics/web-<date>-<host>.md`.

## Non-goals

- Analytics dashboards, live admin, kill switch (Phase 7).
- Email provider (Phase 8).
- Landing page design polish; a plain page with a login link is enough for the gate.

## Handoff

- `web/README.md`; STATUS.md; QUESTIONS Q4 marked resolved or defaulted.
