#!/usr/bin/env bash
# web-stack.sh up|down|status — labd and Django together on the Linux lab host, for the lab
# page by hand (Phase 6 human check) and the end-to-end test (./run.sh test --e2e).
#
#   up      build labd; enable this host's dev challenge images (scripts/challenges-json.sh
#           --local); start labd with site_host = Django's origin; migrate and import into
#           the dev Postgres as role web; start Django. Prints the URL.
#   down    stop Django and labd, then remove any lab left in namespace labs.
#   e2e     up, then web/tests/e2e in a headless browser, then down (./run.sh test --e2e).
#   status  what is running.
#
# Ports: Django 127.0.0.1:8000, labd 8081 (internal) and 8082 (WebSocket), as labd.dev.yaml.
# Labs run under runsc on x86-64 and runc on arm64 (Q13); LABD_RUNTIME overrides.
# Dev secrets only. Work files and logs: .scratch/web-stack/.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
W="$ROOT/.scratch/web-stack"
WEB_PORT="${WEB_PORT:-8000}"
ORIGIN="http://127.0.0.1:$WEB_PORT"
export LABD_INTERNAL_SECRET="${LABD_INTERNAL_SECRET:-dev-internal-secret}"
export WS_TOKEN_KEY="${WS_TOKEN_KEY:-dev-ws-token-key}"
export DEPLOY_SECRET="${DEPLOY_SECRET:-dev-deploy-secret}" # the challenge build's dev secret
export DATABASE_URL="${WEB_DATABASE_URL:-postgres://web:web@127.0.0.1:5432/labs}"
export DJANGO_SETTINGS_MODULE=config.settings.dev
export LABD_WS_BASE="ws://127.0.0.1:8082"
export UV_PROJECT_ENVIRONMENT="${UV_PROJECT_ENVIRONMENT:-/var/tmp/labs-web-venv}" # not the Mac's .venv

say() { printf '==> %s\n' "$*" >&2; }
die() { printf 'web-stack: %s\n' "$*" >&2; exit 1; }
alive() { [[ -s "$1" ]] && kill -0 "$(cat "$1")" 2>/dev/null; }

wait_http() { # URL SECONDS LOG
  local i
  for ((i = 0; i < $2 * 5; i++)); do curl -sf -o /dev/null "$1" && return 0; sleep 0.2; done
  tail -n 30 "$3" >&2
  die "$1 did not answer within $2 s (log: $3)"
}

up() {
  [[ "$(uname -s)" == Linux ]] || die "run on the Linux lab host (./run.sh web-stack up)"
  mkdir -p "$W"
  if alive "$W/labd.pid" || alive "$W/web.pid"; then die "already up (scripts/web-stack.sh down first)"; fi
  "$ROOT/scripts/labs.sh" preflight || die "preflight failed (above); nothing was started"
  [[ -s "$ROOT/.scratch/local-images.json" ]] || die "no dev lab images: build them with scripts/challenge-build.sh"

  say "challenges: this host's dev images (.scratch/web-stack/challenges.json)"
  "$ROOT/scripts/challenges-json.sh" --local > "$W/challenges.json"
  say "building labd"
  (cd "$ROOT/labd" && go build -o bin/labd ./cmd/labd)
  # gdb under gVisor on arm64 cannot resume from a breakpoint (QUESTIONS Q13): the Mac's VM
  # uses runc, as the challenge build's oracle does. x86-64 (the laptop) is authoritative: runsc.
  local runtime="${LABD_RUNTIME:-}"
  if [[ -z "$runtime" ]]; then
    case "$(uname -m)" in x86_64) runtime=io.containerd.runsc.v1 ;; *) runtime=io.containerd.runc.v2 ;; esac
  fi
  say "lab runtime: $runtime"
  sed -e "s|^challenges_file:.*|challenges_file: $W/challenges.json|" \
      -e "s|^runtime:.*|runtime: $runtime|" \
      -e "s|^site_host:.*|site_host: $ORIGIN   # Django's origin: the lab page opens the WebSocket|" \
      "$ROOT/labd/labd.dev.yaml" > "$W/labd.yaml"
  say "starting labd (log: .scratch/web-stack/labd.log)"
  "$ROOT/scripts/with-containerd-group.sh" "$ROOT/labd/bin/labd" -config "$W/labd.yaml" serve >"$W/labd.log" 2>&1 &
  echo $! > "$W/labd.pid"
  wait_http http://127.0.0.1:8081/healthz 30 "$W/labd.log"

  cd "$ROOT/web"
  say "migrate and import into the dev Postgres as role web"
  uv run -q python manage.py migrate --no-input >"$W/migrate.log"
  uv run -q python manage.py import_challenges "$W/challenges.json" --repo "$ROOT"
  say "starting Django on $ORIGIN (log: .scratch/web-stack/web.log)"
  uv run -q python manage.py runserver --noreload "127.0.0.1:$WEB_PORT" >"$W/web.log" 2>&1 &
  echo $! > "$W/web.pid"
  wait_http "$ORIGIN/healthz" 30 "$W/web.log"
  say "up: $ORIGIN (sign up, open lab 1). Stop with: scripts/web-stack.sh down"
}

stop_pid() { # PIDFILE NAME
  local f="$1" pid i
  [[ -s "$f" ]] || return 0
  pid="$(cat "$f")"
  # labd runs under sudo when the shell lacks the containerd group: signal the real process.
  for p in $(pgrep -P "$pid" 2>/dev/null) "$pid"; do sudo -n kill -TERM "$p" 2>/dev/null || kill -TERM "$p" 2>/dev/null || true; done
  for ((i = 0; i < 100; i++)); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
  rm -f "$f"
  say "$2 stopped"
}

down() {
  stop_pid "$W/web.pid" Django
  stop_pid "$W/labd.pid" labd
  # labs outlive labd on purpose; nothing should be left behind by a dev stack.
  "$ROOT/scripts/labs.sh" clean --yes >/dev/null 2>&1 || true
}

# e2e — up, the browser test (web/tests/e2e), down; the stack is always taken down.
e2e() {
  up
  trap down EXIT
  cd "$ROOT/web"
  uv run -q playwright install chromium >/dev/null \
    || die "playwright could not install chromium; once per host: (cd web && uv run playwright install --with-deps chromium)"
  say "end-to-end test (labs under $(awk '/^runtime:/{print $2}' "$W/labd.yaml"))"
  E2E=1 uv run pytest -q -p no:cacheprovider tests/e2e
}

status() {
  alive "$W/labd.pid" && echo "labd: running (pid $(cat "$W/labd.pid"))" || echo "labd: stopped"
  alive "$W/web.pid" && echo "django: running on $ORIGIN (pid $(cat "$W/web.pid"))" || echo "django: stopped"
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  e2e) e2e ;;
  status) status ;;
  *) die "usage: web-stack.sh up|down|e2e|status" ;;
esac
