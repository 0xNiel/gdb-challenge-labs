#!/usr/bin/env bash
# live-run.sh — Phase 7, task 7.8: twenty labs from labd-perf against the web stack, while a
# person watches /admin/live (the human check). Run on the lab host with the stack up
# (./run.sh web-stack up):
#
#   scripts/live-run.sh [--n 20] [--hold 5m]
#
# It makes a dev-only staff login, starts labd-perf (P2 readers) in the background, takes a
# screenshot of /admin/live into docs/metrics/admin-live-<date>-<host>.png once the labs are
# up, and when the run ends writes docs/metrics/data-per-session-<date>-<host>.{json,md}: how
# many bytes events and samples grow per session-minute (Phase 7 "Metrics to record"). It
# reports whether a session was killed with admin_kill during the run (the person's click).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
W="$ROOT/.scratch/web-stack"
N=20 HOLD=5m
while [[ $# -gt 0 ]]; do
  case "$1" in
    --n) N="$2"; shift 2 ;;
    --hold) HOLD="$2"; shift 2 ;;
    *) echo "usage: live-run.sh [--n 20] [--hold 5m]" >&2; exit 2 ;;
  esac
done
say() { printf '==> %s\n' "$*" >&2; }
die() { printf 'live-run: %s\n' "$*" >&2; exit 1; }
if ! { [[ -s "$W/labd.pid" ]] && kill -0 "$(cat "$W/labd.pid")" 2>/dev/null; }; then
  die "the stack is not up: ./run.sh web-stack up"
fi

export LABD_INTERNAL_SECRET="${LABD_INTERNAL_SECRET:-dev-internal-secret}"
export UV_PROJECT_ENVIRONMENT="${UV_PROJECT_ENVIRONMENT:-/var/tmp/labs-web-venv}"
export DATABASE_URL="${WEB_DATABASE_URL:-postgres://web:web@127.0.0.1:5432/labs}"
export DJANGO_SETTINGS_MODULE=config.settings.dev
if [[ -n "${LAB_HOST:-}" ]]; then HOST="$LAB_HOST"
elif [[ "$(hostname)" == lima-* ]]; then HOST=dev-vm
else HOST="$(hostname -s)"; fi
STAMP="$(date -u +%Y-%m-%d)"
STAFF=staff@example.com STAFF_PW=staff-dev-only-pw
OUT="$ROOT/.scratch/live-run"
mkdir -p "$OUT"

say "staff login for this dev stack: $STAFF / $STAFF_PW"
(cd "$ROOT/web" && uv run -q python manage.py shell -c "
from django.contrib.auth import get_user_model
from allauth.account.models import EmailAddress
U = get_user_model()
u, _ = U.objects.get_or_create(username='staff', defaults={'email': '$STAFF'})
u.email, u.is_staff, u.is_superuser = '$STAFF', True, True
u.set_password('$STAFF_PW'); u.save()
EmailAddress.objects.update_or_create(user=u, email='$STAFF', defaults={'verified': True, 'primary': True})
")

sizes() { psql "$DATABASE_URL" -tAc "SELECT pg_total_relation_size('events'), pg_total_relation_size('samples')"; }
IFS='|' read -r EV0 SA0 <<<"$(sizes)"

say "building labd-perf"
(cd "$ROOT/labd" && go build -o bin/labd-perf ./cmd/labd-perf)
say "labd-perf: $N labs (readers) for $HOLD against the stack's labd; open http://127.0.0.1:8000/admin/live as $STAFF"
"$ROOT/scripts/with-containerd-group.sh" "$ROOT/labd/bin/labd-perf" run --scenario P2 --n "$N" --ramp 2 \
  --hold "$HOLD" --api http://127.0.0.1:8081 --ws ws://127.0.0.1:8082 \
  --script "$ROOT/images/perf/session.gdb" --out "$OUT" --host "$HOST" --label live \
  >"$OUT/perf.log" 2>&1 &
PERF=$!
# Interrupted: stop labd-perf and the labs it started, so the stack is clean for another run.
stop_labs() {
  kill "$PERF" 2>/dev/null || true
  local id
  for id in $(curl -s -H "Authorization: Bearer $LABD_INTERNAL_SECRET" http://127.0.0.1:8081/internal/sessions | jq -r '.sessions[].session_id'); do
    curl -s -X DELETE -H "Authorization: Bearer $LABD_INTERNAL_SECRET" -d '{"reason":"user_stop"}' \
      "http://127.0.0.1:8081/internal/sessions/$id" >/dev/null || true
  done
}
trap stop_labs EXIT

# The screenshot, once every lab is running.
for _ in $(seq 1 120); do
  running="$(curl -s -H "Authorization: Bearer $LABD_INTERNAL_SECRET" http://127.0.0.1:8081/internal/stats | jq -r .running)"
  [[ "$running" == "$N" ]] && break
  sleep 2
done
sleep 15 # a sampler tick or two, so memory shows
SHOT="$ROOT/docs/metrics/admin-live-$STAMP-$HOST.png"
(cd "$ROOT/web" && uv run -q python "$ROOT/scripts/admin-screenshot.py" http://127.0.0.1:8000 "$STAFF" "$STAFF_PW" "$SHOT")
say "kill one session from the page now, if you are doing the human check (the run lasts $HOLD)"

wait "$PERF" || die "labd-perf failed: $OUT/perf.log"
trap - EXIT
IFS='|' read -r EV1 SA1 <<<"$(sizes)"
HOLD_MIN="$(jq -r '.meta.hold_s / 60' "$OUT"/run-P2-*-"$HOST"-live.json | tail -n1)"
jq -n --arg host "$HOST" --arg date "$STAMP" --argjson n "$N" --argjson hold "$HOLD_MIN" \
  --argjson ev $((EV1 - EV0)) --argjson sa $((SA1 - SA0)) '{
    host: $host, date: $date, labs: $n, hold_min: $hold, session_minutes: ($n * $hold),
    events_bytes: $ev, samples_bytes: $sa,
    events_per_session_minute: ($ev / ($n * $hold) | floor),
    samples_per_session_minute: ($sa / ($n * $hold) | floor),
    source: "pg_total_relation_size before and after scripts/live-run.sh (reader profile, labd sampler every 10 s)"}' \
  > "$ROOT/docs/metrics/data-per-session-$STAMP-$HOST.json"
D="$ROOT/docs/metrics/data-per-session-$STAMP-$HOST"
{
  echo "# Data per session-minute — $HOST, $STAMP"
  echo
  echo "From \`scripts/live-run.sh\`: $N labs (P2 readers) for $HOLD_MIN minutes against the web stack, labd's sampler every 10 s. \`pg_total_relation_size\` (table, indexes, TOAST) before and after."
  echo
  echo "| Table | Growth | Per session-minute | Spec estimate |"
  echo "| --- | --- | --- | --- |"
  jq -r '"| events | \(.events_bytes) B | \(.events_per_session_minute) B | 2–4 KB for both |"' "$D.json"
  jq -r '"| samples | \(.samples_bytes) B | \(.samples_per_session_minute) B | |"' "$D.json"
} > "$D.md"
say "wrote ${D#"$ROOT"/}.{json,md} and ${SHOT#"$ROOT"/}"
if grep -q admin_kill "$OUT/perf.log"; then say "seen: a session ended with admin_kill during the run"
else say "no admin_kill seen in the run: kill one on /admin/live next time for the human check"; fi
