# web

The Django app (Django 5.2 LTS, Python 3.13, `uv`; ADR 0002). It owns users, the curriculum, progress and flags, and issues the browser's terminal tokens. It never touches containers: it asks labd over the loopback internal API (S11) and reads labd's `sessions` rows.

| App | Responsibility | Phase |
| --- | --- | --- |
| `config/` | settings (`base`, `dev`, `test`, `prod`), urls, the landing page, `/healthz` | 0, 6 |
| `accounts/` | django-allauth, email only (Q4): `/signup`, `/login`, the rest under `/accounts/`. Signup unlocks tier 1's first challenge | 6 |
| `curriculum/` | tiers, lessons, challenges; `import_challenges`; `/learn` and lessons; safe markdown (`markdown.py`) | 6 |
| `labs/` | labd client (`labd_client.py`, fake in `fake_labd.py`), token minting (`tokens.py`), challenge page, start/stop, the terminal page and `static/js/lab.js`; labd's `sessions` and `events` as unmanaged models | 6 |
| `progress/` | flag check (`flag.py`), unlocks, submission and hints (`services.py`), `/dashboard` | 6 |
| `analytics/`, `adminpanel/` | registered, empty | 7 |

## Pages

| URL | What |
| --- | --- |
| `/` | landing |
| `/signup`, `/login`, `/accounts/...` | allauth (logout, password reset, email) |
| `/learn` | tiers and challenges with their state: locked, unlocked, solved |
| `/learn/<tier>/<lesson>` | a lesson, once its challenge is unlocked |
| `/lab/<slug>` | challenge page: description, Start or Stop, hints, flag form, past attempts |
| `/lab/<slug>/session` | the terminal page, while the session is live |
| `/lab/<slug>/session/token?session=<id>` | a fresh WebSocket token (JSON), for the page's own session only |
| `POST /lab/<slug>/start`, `/stop`, `/flag`, `/hint` | actions; `/flag` and `/hint` return a partial when sent `X-Partial: 1`. A correct flag stops the user's lab for that challenge (ADR 0016) |
| `/dashboard` | solved, time in labs, hints, streak, tier completion |
| `/admin/` | Django admin |

## Run it

Unit and view tests use SQLite and a fake labd; nothing else is needed:

```
./run.sh test --web          # or: cd web && uv run pytest -q
```

The real thing needs labd, containerd and the dev Postgres on the Linux lab host (the Mac's VM or the laptop), plus the tier-1 images built there (`scripts/challenge-build.sh`):

```
./run.sh web-stack up        # labd + Django on http://127.0.0.1:8000; sign up and open lab 1
./run.sh web-stack down
./run.sh test --e2e          # the same stack, a headless browser solving lab 1, then down
LAB_HOST=linux-laptop scripts/web-metrics.sh   # record the e2e start latency in docs/metrics
```

`web-stack` imports `.scratch/web-stack/challenges.json` (this host's dev images, `scripts/challenges-json.sh --local`) with `manage.py import_challenges <file> --repo <repo>`. Labs run under runsc on x86-64 and under runc on arm64 (QUESTIONS Q13). The first `--e2e` on a host needs Chromium: `cd web && uv run playwright install --with-deps chromium`.

## Settings

Every secret comes from the environment (`deploy/env.example`): `DJANGO_SECRET_KEY`, `DATABASE_URL`, `DEPLOY_SECRET` (flags, ADR 0005), `LABD_INTERNAL_SECRET` (labd's API), `WS_TOKEN_KEY` (terminal tokens, ADR 0015). Others: `LABD_INTERNAL_URL`, `LABD_WS_BASE` (empty in production: the page's own host), `SITE_HOST`, `CHALLENGES_REPO`.

Tables owned by labd (`sessions`, `events`, `samples`) are unmanaged models (ADR 0003); `labs/tests/test_models.py` checks their columns against labd's migrations.
