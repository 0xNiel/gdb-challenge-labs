#!/usr/bin/env bash
# lib.sh — shared by labd/perf/p1.sh, p4lite.sh, p5.sh and scenario.sh. Source it; do not run it.
#
# Runs a private labd on the Linux lab host: its own config (ports 18081/18082, so a dev
# labd on 8081 is not disturbed), the dev Postgres, the dev challenges.json (perf image),
# and the containerd group rather than root (scripts/with-containerd-group.sh, S10).

PERF_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LABD_BIN="$PERF_ROOT/labd/bin/labd"
API="http://127.0.0.1:18081"
export LABD_INTERNAL_SECRET="${LABD_INTERNAL_SECRET:-perf-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')}"
export WS_TOKEN_KEY="${WS_TOKEN_KEY:-perf-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')}"
WORK="$(mktemp -d)"
LABD_CFG="$WORK/labd.yaml"
LABD_LOG="$WORK/labd.log"

say() { printf '==> %s\n' "$*" >&2; }
die() { printf 'FAIL: %s\n' "$*" >&2; PERF_FAILED=1; exit 1; }

# perf_init CAP [QUEUE] — checks the host (scripts/labs.sh preflight), builds labd, writes
# the config from $PERF_BASE_CFG (default labd.dev.yaml; the Phase 4 suite uses
# labd.perf.yaml) with CAP slots and QUEUE queued (default 2 x CAP), authenticates sudo once
# (ctr needs root to list containers; labd itself does not).
perf_init() {
  local cap="$1" queue="${2:-$(( $1 * 2 ))}"
  [[ "$(uname -s)" == Linux ]] || die "run on the Linux lab host (./run.sh perf --scenario ...)"
  # No other labd (the two would reconcile each other's labs away) and no leftover lab (this
  # labd would adopt it and count it). Stops here, before the build, with what to do.
  "$PERF_ROOT/scripts/labs.sh" preflight || die "preflight failed (above); nothing was started"
  say "building labd"
  (cd "$PERF_ROOT/labd" && go build -o bin/labd ./cmd/labd)
  sed -e "s|^listen_internal:.*|listen_internal: 127.0.0.1:18081|" \
      -e "s|^listen_ws:.*|listen_ws: 127.0.0.1:18082|" \
      -e "s|^max_sessions:.*|max_sessions: $cap|" \
      -e "s|^max_queue:.*|max_queue: $queue|" \
      -e "s|^challenges_file:.*|challenges_file: $PERF_ROOT/labd/challenges.dev.json|" \
      "${PERF_BASE_CFG:-$PERF_ROOT/labd/labd.dev.yaml}" > "$LABD_CFG"
  if ! sudo -n true 2>/dev/null; then say "ctr needs root to count containers; authenticating sudo once"; sudo -v; fi
}

# labd_pid — the labd process itself (not the sudo wrapping it), or empty.
labd_pid() {
  local p
  for p in $(pgrep -f -- "-config $LABD_CFG serve" || true); do
    [[ "$(ps -o comm= -p "$p" 2>/dev/null)" == labd ]] && { echo "$p"; return; }
  done
}

# start_labd — starts labd in the background and waits for /healthz (30 s).
start_labd() {
  "$PERF_ROOT/scripts/with-containerd-group.sh" "$LABD_BIN" -config "$LABD_CFG" serve >>"$LABD_LOG" 2>&1 &
  disown # P5 kills it with SIGKILL on purpose; no "Killed" job notice
  local i
  for ((i = 0; i < 150; i++)); do
    curl -sf "$API/healthz" >/dev/null 2>&1 && return 0
    sleep 0.2
  done
  tail -n 30 "$LABD_LOG" >&2
  die "labd did not answer /healthz within 30 s"
}

# stop_labd [SIGNAL] — signals labd (default TERM) and waits for it to exit.
stop_labd() {
  local sig="${1:-TERM}" pid i
  pid="$(labd_pid)"
  [[ -n "$pid" ]] || return 0
  sudo kill -s "$sig" "$pid" 2>/dev/null || kill -s "$sig" "$pid"
  for ((i = 0; i < 100; i++)); do kill -0 "$pid" 2>/dev/null || return 0; sleep 0.1; done
  die "labd (pid $pid) did not exit after SIG$sig"
}

# api METHOD PATH [BODY] — the response body on stdout.
api() {
  curl -sS -X "$1" -H "Authorization: Bearer $LABD_INTERNAL_SECRET" -H 'Content-Type: application/json' \
    ${3:+--data "$3"} "$API$2"
}

# start_session USER — prints "<session_id> <state> <http_ms>".
start_session() {
  local out code ms body
  out="$(curl -sS -o - -w '\n%{http_code} %{time_total}' -X POST -H "Authorization: Bearer $LABD_INTERNAL_SECRET" \
    --data "{\"user_id\":$1,\"challenge_slug\":\"perf\"}" "$API/internal/sessions")"
  body="$(sed '$d' <<<"$out")"
  read -r code ms <<<"$(tail -n1 <<<"$out")"
  [[ "$code" == 200 ]] || die "start user $1: HTTP $code $body"
  printf '%s %s %s\n' "$(jq -r .session_id <<<"$body")" "$(jq -r .state <<<"$body")" \
    "$(awk -v s="$ms" 'BEGIN{printf "%d", s*1000}')"
}

stat_field() { api GET /internal/stats | jq -r "$1"; }
ctr_count() { sudo ctr -n labs c ls -q | grep -c . || true; }

# wait_until SECONDS DESCRIPTION CMD... — polls CMD every 0.2 s.
wait_until() {
  local t="$1" what="$2" i; shift 2
  for ((i = 0; i < t * 5; i++)); do "$@" && return 0; sleep 0.2; done
  die "timed out after ${t}s waiting for $what"
}

# stop_all — stops every live session and waits until nothing is active or left in containerd.
stop_all() {
  local id
  for id in $(api GET /internal/sessions | jq -r '.sessions[].session_id'); do
    api DELETE "/internal/sessions/$id" '{"reason":"admin_kill"}' >/dev/null
  done
  wait_until 60 "active == 0" active_is 0
}

active_is() { [[ "$(stat_field .active)" == "$1" ]]; }

# host_label — docs/metrics host label, as P0 uses.
host_label() {
  if [[ -n "${LAB_HOST:-}" ]]; then echo "$LAB_HOST"
  elif [[ "$(hostname)" == lima-* ]]; then echo dev-vm
  else hostname -s; fi
}

# pct P — nearest-rank percentile of the numbers on stdin ("null" if none).
pct() { sort -n | awk -v p="$1" '{a[NR]=$1} END{if(NR==0){print "null"; exit} i=int((p/100)*NR+0.999999); if(i<1)i=1; if(i>NR)i=NR; print a[i]}'; }

perf_cleanup() {
  if [[ -n "$(labd_pid)" ]]; then
    stop_all 2>/dev/null || true
    stop_labd TERM 2>/dev/null || true
  fi
  if [[ "${KEEP_LOG:-0}" == 1 || "${PERF_FAILED:-0}" == 1 ]]; then say "labd log: $LABD_LOG"; else rm -rf "$WORK"; fi
}
