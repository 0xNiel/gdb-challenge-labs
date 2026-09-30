#!/usr/bin/env bash
# scenario.sh — Phase 4: one perf scenario against a private labd (labd.perf.yaml).
#
#   labd/perf/scenario.sh --scenario P1..P9 [--n N] [--hold 20m] [--ramp 5] [--cap 100]
#       [--queue 50] [--runtime runsc|runc] [--platform kvm] [--partial REASON] [--out DIR]
#
# Starts labd (lib.sh: preflight, own ports, containerd group), runs `labd-perf run`, and does
# the host actions labd-perf cannot: P5 kills labd with SIGKILL once the sessions are up and
# restarts it; P7 removes the lab images from containerd first and times their re-import (the
# perf image is local only, so an import from images/out stands in for a registry pull);
# P2, P4 and P9 sample disk with disk.sh. --runtime runc is the gVisor reference (dev hosts
# only); --platform kvm switches runsc to KVM for the run and restores systrap after.
# Output: DIR/run-<scenario>-<date>-<host>[-label].json (default docs/metrics).
set -euo pipefail
# shellcheck source=lib.sh disable=SC1091
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

SCEN="" N="" HOLD="" RAMP=5 CAP=100 QUEUE=50 RUNTIME=runsc PLATFORM="" PARTIAL="" OUT="$PERF_ROOT/docs/metrics"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --scenario) SCEN="$2"; shift 2 ;;
    --n) N="$2"; shift 2 ;;
    --hold) HOLD="$2"; shift 2 ;;
    --ramp) RAMP="$2"; shift 2 ;;
    --cap) CAP="$2"; shift 2 ;;
    --queue) QUEUE="$2"; shift 2 ;;
    --runtime) RUNTIME="$2"; shift 2 ;;
    --platform) PLATFORM="$2"; shift 2 ;;
    --partial) PARTIAL="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    *) echo "usage: scenario.sh --scenario P1..P9 [--n N] [--hold D] [--ramp R] [--cap N] [--queue N] [--runtime runsc|runc] [--platform kvm] [--partial WHY] [--out DIR]" >&2; exit 2 ;;
  esac
done
[[ "$SCEN" =~ ^P[1-9]$ ]] || die "--scenario P1..P9 is required"

RT_ID=io.containerd.runsc.v1 LABEL=""
case "$RUNTIME" in
  runsc) ;;
  runc)
    command -v runc >/dev/null || die "--runtime runc needs runc (dev hosts only; production has none, S1)"
    [[ "$SCEN" =~ ^P[1-3]$ ]] || die "the runc reference covers P1-P3 only"
    RT_ID=io.containerd.runc.v2 LABEL=runc ;;
  *) die "--runtime must be runsc or runc" ;;
esac

PERF_BASE_CFG="$PERF_ROOT/labd/labd.perf.yaml" perf_init "$CAP" "$QUEUE"
trap 'restore_platform; perf_cleanup' EXIT
sed -i "s|^runtime:.*|runtime: $RT_ID|" "$LABD_CFG"

RUNSC_TOML=/etc/containerd/runsc.toml
restore_platform() {
  if [[ -f "$WORK/runsc.toml.orig" ]]; then
    sudo cp "$WORK/runsc.toml.orig" "$RUNSC_TOML" && rm -f "$WORK/runsc.toml.orig" && say "runsc platform restored"
  fi
}
if [[ -n "$PLATFORM" ]]; then
  [[ "$RUNTIME" == runsc ]] || die "--platform applies to runsc only"
  [[ "$PLATFORM" != kvm || -e /dev/kvm ]] || die "--platform kvm needs /dev/kvm"
  sudo cp "$RUNSC_TOML" "$WORK/runsc.toml.orig"
  sudo sed -i "s|^\( *platform *= *\).*|\1\"$PLATFORM\"|" "$RUNSC_TOML"
  grep -q "platform = \"$PLATFORM\"" "$RUNSC_TOML" || die "could not set platform $PLATFORM in $RUNSC_TOML"
  LABEL="${LABEL:+$LABEL-}$PLATFORM"
  say "runsc platform $PLATFORM for this run"
fi
PLATFORM_META="${PLATFORM:-$(sed -n 's/^ *platform *= *"\(.*\)"/\1/p' "$RUNSC_TOML" 2>/dev/null | head -n1)}"
[[ "$RUNTIME" == runsc ]] || PLATFORM_META=""

# ctr and du need root inside long runs; keep the sudo timestamp fresh (never prompts).
( while sleep 60; do sudo -n true 2>/dev/null || exit 0; done ) &
KEEPALIVE=$!
trap 'kill $KEEPALIVE 2>/dev/null; restore_platform; perf_cleanup' EXIT

say "building labd-perf"
(cd "$PERF_ROOT/labd" && go build -o bin/labd-perf ./cmd/labd-perf)

PERF_ARGS=(--scenario "$SCEN" --ramp "$RAMP" --api "$API" --ws ws://127.0.0.1:18082
  --script "$PERF_ROOT/images/perf/session.gdb" --out "$OUT" --host "$(host_label)"
  --runtime "$RT_ID" --platform "$PLATFORM_META" --label "$LABEL" --partial "$PARTIAL")
[[ -n "$N" ]] && PERF_ARGS+=(--n "$N")
[[ -n "$HOLD" ]] && PERF_ARGS+=(--hold "$HOLD")
case "$SCEN" in
  P2|P4|P9) PERF_ARGS+=(--disk-cmd "bash $PERF_ROOT/labd/perf/disk.sh $LABD_CFG") ;;
esac

labd_perf() { "$PERF_ROOT/scripts/with-containerd-group.sh" "$PERF_ROOT/labd/bin/labd-perf" run "${PERF_ARGS[@]}" "$@"; }

ARCH="$(uname -m)"; case "$ARCH" in x86_64) ARCH=amd64 ;; aarch64) ARCH=arm64 ;; esac
PERF_TAR="$PERF_ROOT/images/out/gdblabs_perf_dev_$ARCH.tar"
BASE_TAR="$PERF_ROOT/images/out/gdblabs_labbase_dev_$ARCH.tar"

case "$SCEN" in
  P5)
    start_labd
    READY="$WORK/ready"
    labd_perf --ready-file "$READY" &
    perf_pid=$!
    have_ready() { [[ -f "$READY" ]] || ! kill -0 "$perf_pid" 2>/dev/null; }
    wait_until 900 "labd-perf to bring the sessions up" have_ready
    [[ -f "$READY" ]] || { wait "$perf_pid" || true; die "labd-perf ended before the sessions were up"; }
    say "SIGKILL labd with $(stat_field .running) labs running, then restart it"
    stop_labd KILL
    start_labd
    wait "$perf_pid" || die "labd-perf failed (labd log: $LABD_LOG)"
    ;;
  P7)
    [[ -f "$PERF_TAR" ]] || die "$PERF_TAR missing: ./run.sh images perf builds it"
    say "removing the lab images from containerd (cold start)"
    sudo ctr -n labs images rm docker.io/gdblabs/perf:dev docker.io/gdblabs/labbase:dev >/dev/null 2>&1 || true
    sudo ctr -n labs content prune references >/dev/null 2>&1 || true
    start_labd
    labd_perf --pull-cmd "sudo -n ctr -n labs images import --platform linux/$ARCH --local $PERF_TAR >/dev/null && $PERF_ROOT/scripts/with-containerd-group.sh $LABD_BIN -config $LABD_CFG pull" \
      || die "labd-perf failed (labd log: $LABD_LOG)"
    if [[ -f "$BASE_TAR" ]]; then
      say "re-importing labbase (not timed)"
      sudo ctr -n labs images import --platform "linux/$ARCH" --local "$BASE_TAR" >/dev/null
    fi
    ;;
  *)
    start_labd
    labd_perf || die "labd-perf failed (labd log: $LABD_LOG)"
    ;;
esac
say "labd log kept only on failure; done"
