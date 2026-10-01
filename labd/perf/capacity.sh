#!/usr/bin/env bash
# capacity.sh — Phase 7, task 7.11 (ADR 0017): the capacity search. Runs P10 (90 % learners on
# the real tier-1 labs, 10 % abusers on the perf image) at increasing counts against a private
# labd, stops at the first count that misses a criterion, then writes
# docs/metrics/capacity-search-<date>-<host>.{json,md} with `labd-perf capacity`.
#
#   labd/perf/capacity.sh [--cpus 8] [--steps "60 90 120 150 180"] [--hold 8m] [--ramp 2]
#       [--mix learner=90,abuser=10] [--runtime runsc|runc] [--keep-going] [--out DIR]
#
# --cpus N takes every CPU from N up offline for the run (sysfs, needs sudo) so the host has N,
# like the 8-vCPU VPS, and brings them back on exit, whatever happens. On the i5-13450HX laptop
# CPUs 0-7 are four performance cores with their hyperthreads. Without --cpus the host's own
# CPUs are used, and the record says how many. --restore-cpus brings every CPU back online
# (after an interrupted run) and exits.
#
# Needs the tier-1 dev images on this host (scripts/challenge-build.sh; .scratch/local-images.json)
# and the perf image (./run.sh images perf). Each count needs about 55 MB of RAM per lab; a
# count that would not fit in 90 % of RAM is refused before it starts.
set -euo pipefail
# shellcheck source=lib.sh disable=SC1091
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

CPUS="" STEPS="60 90 120 150 180" HOLD=8m RAMP=2 MIX="learner=90,abuser=10" RUNTIME=runsc KEEP_GOING=0 RESTORE=0
OUT="$PERF_ROOT/docs/metrics"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --cpus) CPUS="$2"; shift 2 ;;
    --steps) STEPS="$2"; shift 2 ;;
    --hold) HOLD="$2"; shift 2 ;;
    --ramp) RAMP="$2"; shift 2 ;;
    --mix) MIX="$2"; shift 2 ;;
    --runtime) RUNTIME="$2"; shift 2 ;;
    --keep-going) KEEP_GOING=1; shift ;;
    --restore-cpus) RESTORE=1; shift ;;
    --out) OUT="$2"; shift 2 ;;
    *) echo "usage: capacity.sh [--cpus N] [--steps \"60 90 ...\"] [--hold 8m] [--ramp 2] [--mix learner=90,abuser=10] [--runtime runsc|runc] [--keep-going] [--out DIR]" >&2; exit 2 ;;
  esac
done
case "$RUNTIME" in
  runsc) RT_ID=io.containerd.runsc.v1 ;;
  runc) RT_ID=io.containerd.runc.v2 ;;
  *) die "--runtime must be runsc or runc" ;;
esac
LOCAL="$PERF_ROOT/.scratch/local-images.json"
[[ -s "$LOCAL" ]] || die "no tier-1 dev images on this host: for d in challenges/tier1-c-fundamentals/0*; do bash scripts/challenge-build.sh \"\$d\"; done"
MAXN=0
for n in $STEPS; do [[ "$n" =~ ^[0-9]+$ ]] || die "--steps: $n is not a count"; ((n > MAXN)) && MAXN=$n; done

# ---- CPUs: offline everything from --cpus up, restore on exit. Through sysfs, not chcpu:
# chcpu lives in /usr/sbin, often not on a user's PATH (found on the laptop).
CPUDIR=/sys/devices/system/cpu
OFFLINED=""
set_cpus() { # 0|1 FIRST LAST
  local n
  for ((n = $2; n <= $3; n++)); do
    [[ -e "$CPUDIR/cpu$n/online" ]] || die "CPU $n cannot be taken offline (no $CPUDIR/cpu$n/online)"
    echo "$1" | sudo tee "$CPUDIR/cpu$n/online" >/dev/null
  done
}
restore_cpus() {
  if [[ -n "$OFFLINED" ]]; then
    set_cpus 1 "${OFFLINED%-*}" "${OFFLINED#*-}" && say "CPUs $OFFLINED back online ($(nproc) online)"
    OFFLINED=""
  fi
}
if [[ "$RESTORE" == 1 ]]; then # after an interrupted run
  total="$(nproc --all)"
  set_cpus 1 1 $((total - 1))
  say "$(nproc) of $total CPUs online"
  exit 0
fi
if [[ -n "$CPUS" ]]; then
  total="$(nproc --all)"
  ((CPUS >= 1 && CPUS < total)) || die "--cpus must be between 1 and $((total - 1)) (this host has $total)"
  [[ "$(nproc)" == "$total" ]] || die "some CPUs are already offline ($(nproc) of $total): labd/perf/capacity.sh --restore-cpus first"
fi

PERF_BASE_CFG="$PERF_ROOT/labd/labd.perf.yaml" perf_init "$MAXN" 10
trap 'restore_cpus; perf_cleanup' EXIT
if [[ -n "$CPUS" ]]; then
  OFFLINED="$CPUS-$(( $(nproc --all) - 1 ))"
  set_cpus 0 "${OFFLINED%-*}" "${OFFLINED#*-}"
  say "CPUs $OFFLINED offline: $(nproc) online for the run"
fi

# Challenges: the perf image for abusers plus this host's tier-1 dev images for learners.
"$PERF_ROOT/scripts/challenges-json.sh" --local > "$WORK/tier1.json"
jq -s '{version: 1, challenges: (.[0].challenges + [.[1].challenges[] | select(.enabled)])}' \
  "$PERF_ROOT/labd/challenges.dev.json" "$WORK/tier1.json" > "$WORK/challenges.json"
[[ "$(jq '.challenges | length' "$WORK/challenges.json")" -ge 6 ]] || die "expected perf and five tier-1 labs in $WORK/challenges.json"
sed -i -e "s|^challenges_file:.*|challenges_file: $WORK/challenges.json|" -e "s|^runtime:.*|runtime: $RT_ID|" "$LABD_CFG"

( while sleep 60; do sudo -n true 2>/dev/null || exit 0; done ) &
KEEPALIVE=$!
trap 'kill $KEEPALIVE 2>/dev/null; restore_cpus; perf_cleanup' EXIT

say "building labd-perf"
(cd "$PERF_ROOT/labd" && go build -o bin/labd-perf ./cmd/labd-perf)
HOST="$(host_label)"
mem_total_mb() { awk '/^MemTotal/{print int($2/1024)}' /proc/meminfo; }
mem_used_mb() { awk '/^MemTotal/{t=$2} /^MemAvailable/{a=$2} END{print int((t-a)/1024)}' /proc/meminfo; }

start_labd
for n in $STEPS; do
  need=$(( $(mem_used_mb) + n * 55 ))
  if ((need > $(mem_total_mb) * 9 / 10)); then
    say "stopping before $n labs: they need about $need MB, more than 90 % of $(mem_total_mb) MB"
    break
  fi
  say "P10 at $n labs ($(nproc) CPUs online, labs under $RUNTIME, hold $HOLD)"
  out="$WORK/p10-$n.log"
  "$PERF_ROOT/scripts/with-containerd-group.sh" "$PERF_ROOT/labd/bin/labd-perf" run --scenario P10 --n "$n" \
    --ramp "$RAMP" --hold "$HOLD" --mix "$MIX" --api "$API" --ws ws://127.0.0.1:18082 \
    --script "$PERF_ROOT/images/perf/session.gdb" --learner "$PERF_ROOT/labd/perf/learner.txt" \
    --out "$OUT" --host "$HOST" --runtime "$RT_ID" --label "n$n" 2>&1 | tee "$out" >&2 \
    || die "labd-perf failed at $n labs (labd log: $LABD_LOG)"
  [[ "$(stat_field .active)" == 0 ]] || stop_all
  if grep -q '  MISS  ' "$out" && ((KEEP_GOING == 0)); then
    say "$n labs missed a criterion: that is the knee; stopping (--keep-going runs the rest)"
    break
  fi
done

"$PERF_ROOT/labd/bin/labd-perf" capacity --in "$OUT" --host "$HOST"
say "done; commit docs/metrics"
