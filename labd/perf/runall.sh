#!/usr/bin/env bash
# runall.sh — Phase 4, tasks 4.8 to 4.10: the whole perf suite on this host, then the report.
#
#   LAB_HOST=linux-laptop labd/perf/runall.sh [--only P2,P3,...] [--skip kvm,runc,report] [--out DIR]
#
# Order (plan 4.8): P1 P2 P3 P6 P4 P5 P7 P8 P9; then P1 and P2 on the KVM platform when
# /dev/kvm exists; then the runc reference P1-P3 (4.9); then `labd-perf report` (4.10).
# About 5 hours; P9 alone is 2. Each scenario starts with lib.sh's preflight, so a leftover
# lab or labd stops it at once. A scenario that fails to run is reported at the end and the
# rest still run; missed perf criteria are findings, not failures (ADR 0006).
#
# N: 100 unless this host cannot hold 100 labs with 25 % memory headroom; then the largest N
# that fits, and P2, P3 and P8 are marked partial (Phase 8, task 8.8 repeats them on the VPS).
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ONLY="" SKIP="" OUT="$ROOT/docs/metrics"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --only) ONLY=",$2,"; shift 2 ;;
    --skip) SKIP=",$2,"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    *) echo "usage: runall.sh [--only P1,P2,...] [--skip kvm,runc,report] [--out DIR]" >&2; exit 2 ;;
  esac
done
say() { printf '\n==> [runall] %s\n' "$*" >&2; }
[[ "$(uname -s)" == Linux ]] || { echo "run on the Linux lab host" >&2; exit 1; }
if [[ -n "${LAB_HOST:-}" ]]; then HOST="$LAB_HOST"
elif [[ "$(hostname)" == lima-* ]]; then HOST=dev-vm
else HOST="$(hostname -s)"; fi
export LAB_HOST="$HOST"
want() { [[ -z "$ONLY" || "$ONLY" == *",$1,"* ]] && [[ "$SKIP" != *",$1,"* ]]; }

# Per-lab host cost for sizing: the Phase 3 P1 measured about 28 MiB of cgroup memory; the
# shim adds about 24 MiB of RSS outside it. 60 MB per lab is a deliberately high guess.
PER_LAB_MB=60
total_mb=$(awk '/^MemTotal:/{print int($2/1024)}' /proc/meminfo)
used_mb=$(awk '/^MemTotal:/{t=$2} /^MemAvailable:/{a=$2} END{print int((t-a)/1024)}' /proc/meminfo)
fit=$(( (total_mb * 3 / 4 - used_mb) / PER_LAB_MB ))
N=100 PARTIAL=""
if (( fit < 100 )); then
  N=$fit
  PARTIAL="this host (${total_mb} MB, ${used_mb} MB used before the run) fits ${fit} labs at 25 % headroom"
  say "running the 100-lab scenarios at N=$N: $PARTIAL"
fi
say "host $HOST: ${total_mb} MB, ${used_mb} MB used, N=$N; results in ${OUT#"$ROOT"/}"

FAILED=()
run() { # NAME ARGS...
  local name="$1"; shift
  say "$name: ./run.sh perf $*"
  local t0=$SECONDS
  if "$ROOT/run.sh" perf --out "$OUT" "$@"; then say "$name done in $(( (SECONDS - t0) / 60 )) min"
  else FAILED+=("$name"); say "$name FAILED (continuing)"; fi
}
partial() { [[ -n "$PARTIAL" ]] && printf -- '--partial\n%s\n' "$PARTIAL"; }

want P1 && run P1 --scenario P1
if want P2; then mapfile -t p < <(partial); run P2 --scenario P2 --n "$N" "${p[@]}"; fi
if want P3; then mapfile -t p < <(partial); run P3 --scenario P3 --n "$N" "${p[@]}"; fi
want P6 && run P6 --scenario P6
want P4 && run P4 --scenario P4 --n "$N"
want P5 && run P5 --scenario P5 --n "$N"
want P7 && run P7 --scenario P7
if want P8; then mapfile -t p < <(partial); run P8 --scenario P8 --n "$N" "${p[@]}"; fi
want P9 && run P9 --scenario P9 --n "$N"
if want kvm; then
  if [[ -e /dev/kvm ]]; then
    run P1-kvm --scenario P1 --platform kvm
    run P2-kvm --scenario P2 --n "$N" --platform kvm
  else say "no /dev/kvm: KVM platform runs skipped"; fi
fi
if want runc; then
  if command -v runc >/dev/null; then
    run P1-runc --scenario P1 --runtime runc
    run P2-runc --scenario P2 --n "$N" --runtime runc
    run P3-runc --scenario P3 --n "$N" --runtime runc
  else say "no runc on this host: reference runs skipped"; fi
fi
if want report; then
  say "report"
  (cd "$ROOT/labd" && go build -o bin/labd-perf ./cmd/labd-perf) && \
    (cd "$ROOT" && labd/bin/labd-perf report --in "$OUT" --host "$HOST") || FAILED+=(report)
fi
if ((${#FAILED[@]})); then say "did not complete: ${FAILED[*]}"; exit 1; fi
say "all done. Commit docs/metrics (run-*, perf-report-*, capacity.md)."
