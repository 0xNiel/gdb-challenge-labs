#!/usr/bin/env bash
# p4lite.sh — Phase 2, task 2.12: churn against a private labd with max_sessions 20.
#
#   labd/perf/p4lite.sh [--duration 600] [--interval 2] [--cap 20] [--out DIR]
#
# Every --interval seconds for --duration seconds: start a session for a new user, and once
# the cap is reached stop a random running one, so labs are created and destroyed
# continuously at the cap. Checks, failing the run on any miss:
#   - every 30 s: active <= cap and containers in namespace labs <= cap;
#   - at the end, once nothing is creating or ending: containers == active;
#   - after stopping everything: 0 active, 0 containers, goroutines back to the idle count.
# Records create latency (slot acquired to task running, labd's start_latency_ms) over every
# session that ran, labd's own RSS and goroutines at 0 and at --cap sessions, and the API
# response time of POST /internal/sessions.
# Output: DIR/create-latency-<date>-<host>.json and .md (default docs/metrics).
set -euo pipefail
# shellcheck source=lib.sh disable=SC1091
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

DURATION=600 INTERVAL=2 CAP=20 OUT="$PERF_ROOT/docs/metrics"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --duration) DURATION="$2"; shift 2 ;;
    --interval) INTERVAL="$2"; shift 2 ;;
    --cap) CAP="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    *) echo "usage: p4lite.sh [--duration S] [--interval S] [--cap N] [--out DIR]" >&2; exit 2 ;;
  esac
done

perf_init "$CAP"
trap perf_cleanup EXIT
start_labd
[[ "$(ctr_count)" == 0 ]] || die "namespace labs had containers left after labd's reconcile: $(sudo ctr -n labs c ls -q | tr '\n' ' ')"
G_IDLE="$(stat_field .labd.goroutines)"
RSS_IDLE="$(stat_field .labd.rss_mb)"
say "labd idle: $G_IDLE goroutines, $RSS_IDLE MiB; churning for ${DURATION}s at cap $CAP, one start every ${INTERVAL}s"

LAT=()     # start_latency_ms of every session that reached running
HTTP=()    # POST /internal/sessions response time, ms
STARTS=0 STOPS=0 user=0 tick=0
record_latency() { # SESSIONS_JSON ID — the session's start latency, if it ran
  local v; v="$(jq -r --arg id "$2" '.sessions[] | select(.session_id == $id) | .start_latency_ms // empty' <<<"$1")"
  [[ -n "$v" && "$v" != 0 ]] && LAT+=("$v")
  return 0
}

end_at=$((SECONDS + DURATION))
while ((SECONDS < end_at)); do
  tick_start=$SECONDS
  user=$((user + 1))
  read -r _ state ms <<<"$(start_session "$((1000000 + user))")"
  STARTS=$((STARTS + 1)); HTTP+=("$ms")
  [[ "$state" == creating || "$state" == queued ]] || die "start returned state $state"

  sessions="$(api GET /internal/sessions)"
  mapfile -t running < <(jq -r '.sessions[] | select(.state == "running") | .session_id' <<<"$sessions")
  if ((${#running[@]} >= CAP - 1)); then
    victim="${running[RANDOM % ${#running[@]}]}"
    record_latency "$sessions" "$victim"
    api DELETE "/internal/sessions/$victim" '{"reason":"user_stop"}' >/dev/null
    STOPS=$((STOPS + 1))
  fi

  tick=$((tick + 1))
  if ((tick % (30 / INTERVAL) == 0)); then
    st="$(api GET /internal/stats)"; n="$(ctr_count)"
    active="$(jq .active <<<"$st")"
    ((active <= CAP)) || die "active $active over the cap $CAP"
    ((n <= CAP)) || die "$n containers over the cap $CAP"
    say "t=$((DURATION - end_at + SECONDS))s starts=$STARTS stops=$STOPS active=$active queued=$(jq .queued <<<"$st") containers=$n"
  fi
  sleep "$(( INTERVAL - (SECONDS - tick_start) > 0 ? INTERVAL - (SECONDS - tick_start) : 0 ))"
done

say "churn done; waiting for creates and teardowns to finish"
quiet() { [[ "$(api GET /internal/stats | jq '.creating + .ending + .queued')" == 0 ]]; }
wait_until 60 "no session creating, ending or queued" quiet
active="$(stat_field .active)"; n="$(ctr_count)"
[[ "$n" == "$active" ]] || die "containers $n != active $active after churn (leak)"
say "containers == active == $active"

say "topping up to exactly $CAP sessions to measure labd"
while (( $(stat_field .active) < CAP )); do
  user=$((user + 1)); start_session "$((1000000 + user))" >/dev/null; STARTS=$((STARTS + 1))
done
at_cap() { [[ "$(stat_field .running)" == "$CAP" ]]; }
wait_until 60 "$CAP running sessions" at_cap
sleep 3
st="$(api GET /internal/stats)"
G_CAP="$(jq .labd.goroutines <<<"$st")"; RSS_CAP="$(jq .labd.rss_mb <<<"$st")"
LAB_RSS_P50="$(jq '.sessions[].rss_mb' <<<"$st" | pct 50)"
[[ "$(ctr_count)" == "$CAP" ]] || die "containers $(ctr_count) != $CAP at the cap"
sessions="$(api GET /internal/sessions)"
for id in $(jq -r '.sessions[].session_id' <<<"$sessions"); do record_latency "$sessions" "$id"; done

say "stopping everything"
stop_all
[[ "$(ctr_count)" == 0 ]] || die "$(ctr_count) containers left after stopping every session"
sleep 2
G_END="$(stat_field .labd.goroutines)"
((G_END <= G_IDLE + 2)) || die "goroutine leak: $G_IDLE idle before, $G_END after every session ended"

n_lat=${#LAT[@]}
p50="$(printf '%s\n' "${LAT[@]}" | pct 50)"; p95="$(printf '%s\n' "${LAT[@]}" | pct 95)"
mx="$(printf '%s\n' "${LAT[@]}" | sort -n | tail -n1)"
h50="$(printf '%s\n' "${HTTP[@]}" | pct 50)"; h95="$(printf '%s\n' "${HTTP[@]}" | pct 95)"
host="$(host_label)" stamp="$(date -u +%Y-%m-%d)"
mkdir -p "$OUT"
json="$OUT/create-latency-$stamp-$host.json" md="$OUT/create-latency-$stamp-$host.md"
jq -n --arg host "$host" --arg arch "$(uname -m)" --arg kernel "$(uname -r)" --arg date "$(date -u +%FT%TZ)" \
  --arg runtime "$(sed -n 's/^runtime: *//p' "$LABD_CFG")" --arg runsc "$(runsc --version 2>/dev/null | sed -n 1p)" \
  --argjson cap "$CAP" --argjson duration "$DURATION" --argjson interval "$INTERVAL" \
  --argjson starts "$STARTS" --argjson stops "$STOPS" --argjson n "$n_lat" \
  --argjson p50 "$p50" --argjson p95 "$p95" --argjson max "$mx" --argjson h50 "$h50" --argjson h95 "$h95" \
  --argjson g0 "$G_IDLE" --argjson gcap "$G_CAP" --argjson gend "$G_END" \
  --argjson rss0 "$RSS_IDLE" --argjson rsscap "$RSS_CAP" --argjson lab50 "$LAB_RSS_P50" \
  '{host:$host, arch:$arch, kernel:$kernel, date:$date, runtime:$runtime, runsc:$runsc,
    scenario:"P4-lite", cap:$cap, duration_s:$duration, interval_s:$interval, starts:$starts, stops:$stops,
    create_to_running_ms:{n:$n, p50:$p50, p95:$p95, max:$max},
    api_start_response_ms:{p50:$h50, p95:$h95},
    labd:{goroutines_idle:$g0, goroutines_at_cap:$gcap, goroutines_after:$gend, rss_mib_idle:$rss0, rss_mib_at_cap:$rsscap},
    lab_cgroup_mib_p50_at_cap:$lab50, leaks:{containers:0, goroutines:($gend - $g0)}}' > "$json"
{
  echo "# Create latency (P4-lite) — $host, $stamp"
  echo
  echo "Host \`$host\` ($(uname -m), kernel $(uname -r)), $(runsc --version 2>/dev/null | sed -n 1p). Runtime \`$(sed -n 's/^runtime: *//p' "$LABD_CFG")\`, image \`docker.io/gdblabs/perf:dev\`."
  echo "Generated by \`labd/perf/p4lite.sh --duration $DURATION --interval $INTERVAL --cap $CAP\`; raw data in \`$(basename "$json")\`."
  echo
  echo "| Metric | Value |"
  echo "| --- | --- |"
  echo "| Sessions started / stopped by the script (churn plus top-up) | $STARTS / $STOPS |"
  echo "| Create to running (slot acquired to task running), n | $n_lat |"
  echo "| Create to running p50 / p95 / max (ms) | $p50 / $p95 / $mx |"
  echo "| POST /internal/sessions response p50 / p95 (ms) | $h50 / $h95 |"
  echo "| labd goroutines: idle / at $CAP sessions / after all ended | $G_IDLE / $G_CAP / $G_END |"
  echo "| labd RSS: idle / at $CAP sessions (MiB) | $RSS_IDLE / $RSS_CAP |"
  echo "| Lab cgroup memory p50 at $CAP idle sessions (MiB) | $LAB_RSS_P50 |"
  echo "| Leaks: containers after all ended | 0 |"
  echo
  echo "Checks that passed: active and containers never above $CAP; containers == active once quiet; 0 containers and no goroutine growth after all sessions ended."
} > "$md"
say "wrote ${json#"$PERF_ROOT"/} and ${md#"$PERF_ROOT"/}"
say "create to running: n=$n_lat p50=${p50}ms p95=${p95}ms max=${mx}ms; labd at cap: $G_CAP goroutines, $RSS_CAP MiB"
