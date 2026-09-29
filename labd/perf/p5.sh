#!/usr/bin/env bash
# p5.sh — Phase 2, task 2.12: crash recovery (P5 at 20 sessions).
#
#   labd/perf/p5.sh [--n 20] [--deadline 15]
#
# Starts --n sessions against a private labd, kill -9s labd, restarts it, and requires within
# --deadline seconds of the restart: /internal/stats active == n, every original session id
# listed as running, and n containers in namespace labs. Then stops every session and
# requires 0 active and 0 containers. Exit 0 only if all of that holds.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

N=20 DEADLINE=15
while [[ $# -gt 0 ]]; do
  case "$1" in
    --n) N="$2"; shift 2 ;;
    --deadline) DEADLINE="$2"; shift 2 ;;
    *) echo "usage: p5.sh [--n N] [--deadline S]" >&2; exit 2 ;;
  esac
done

perf_init "$N"
trap perf_cleanup EXIT
start_labd
[[ "$(ctr_count)" == 0 ]] || die "namespace labs had containers left after labd's reconcile"

say "starting $N sessions"
IDS=()
for ((u = 1; u <= N; u++)); do
  read -r id _ _ <<<"$(start_session "$((2000000 + u))")"
  IDS+=("$id")
done
all_running() { [[ "$(stat_field .running)" == "$N" ]]; }
wait_until 60 "$N running sessions" all_running
[[ "$(ctr_count)" == "$N" ]] || die "$(ctr_count) containers for $N running sessions"

pid="$(labd_pid)"
say "kill -9 labd (pid $pid) with $N running labs"
stop_labd KILL
[[ "$(ctr_count)" == "$N" ]] || die "labs did not survive labd's death: $(ctr_count) containers"

t0=$(date +%s%N)
start_labd
recovered() {
  local list
  [[ "$(stat_field .active)" == "$N" ]] || return 1
  list="$(api GET /internal/sessions)"
  local id
  for id in "${IDS[@]}"; do
    [[ "$(jq -r --arg id "$id" '.sessions[] | select(.session_id == $id) | .state' <<<"$list")" == running ]] || return 1
  done
}
wait_until "$DEADLINE" "all $N sessions adopted and running" recovered
ms=$(( ($(date +%s%N) - t0) / 1000000 ))
[[ "$(ctr_count)" == "$N" ]] || die "$(ctr_count) containers after recovery, want $N"
grep -q "adopted=$N" "$LABD_LOG" || die "labd did not report adopting $N labs: $(grep reconcile "$LABD_LOG" | tail -n1)"
say "recovered: $N of $N sessions running, $N containers, ${ms} ms from restart to all adopted (deadline ${DEADLINE}s)"

say "stopping everything"
stop_all
[[ "$(ctr_count)" == 0 ]] || die "$(ctr_count) containers left after stopping every session"
say "P5 passed: restart to all $N adopted in ${ms} ms; 0 containers left"
