# web

Django 5.2 LTS project (Python 3.13, `uv`). Created in Phase 0 (skeleton with `/healthz`), filled in Phase 6 (product) and Phase 7 (analytics and admin live view).

| App | Responsibility | Phase |
| --- | --- | --- |
| `config/` | settings (`base`, `dev`, `test`, `prod`), urls, wsgi | 0 |
| `accounts/` | signup, login, verification, reset via django-allauth | 6 |
| `curriculum/` | tiers, lessons, challenges; `import_challenges` command; markdown rendering | 6 |
| `labs/` | labd client, start/stop/token views, lab page with xterm.js | 6 |
| `progress/` | flag derivation and submission, unlock rules, hints, dashboard | 6 |
| `analytics/` | rollup and retention commands, four dashboards, SVG charts | 7 |
| `adminpanel/` | live sessions, kill, drain, challenge toggles | 7 |

Tables owned by `labd` (`sessions`, `events`, `samples`) are unmanaged models (ADR 0003).

Commands: `./run.sh web`, `./run.sh test --web`, `./run.sh test --e2e` (VM), `uv run python manage.py import_challenges ../challenges.json --repo ..`.
