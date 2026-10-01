#!/usr/bin/env bash
# web-manage.sh ARGS... — `manage.py ARGS` on the lab host, against the dev Postgres as role web
# (the database web-stack.sh and labd use). ./run.sh manage ARGS... runs it from any host.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export UV_PROJECT_ENVIRONMENT="${UV_PROJECT_ENVIRONMENT:-/var/tmp/labs-web-venv}"
export DATABASE_URL="${WEB_DATABASE_URL:-postgres://web:web@127.0.0.1:5432/labs}"
export DJANGO_SETTINGS_MODULE=config.settings.dev
cd "$ROOT/web"
exec uv run -q python manage.py "$@"
