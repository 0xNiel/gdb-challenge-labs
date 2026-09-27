#!/usr/bin/env bash
# verify-runtime.sh — prove a container runs under gVisor in namespace `labs` on this host.
# Prints "runsc ok" and "cgroup2fs" on success (Phase 0 gate greps for both).
set -euo pipefail

IMAGE="docker.io/library/alpine:3.20"
NS=labs
RUNTIME=io.containerd.runsc.v1
ID="verify-$$"

fail() { echo "verify-runtime: $*" >&2; exit 1; }

[[ "$(uname -s)" == Linux ]] || fail "run on Linux (./run.sh vm verify does this for you on macOS)"
command -v ctr >/dev/null || fail "ctr not found — ./run.sh vm up"
command -v runsc >/dev/null || fail "runsc not found — ./run.sh vm up"

CTR=(ctr)
[[ -w /run/containerd/containerd.sock ]] || CTR=(sudo ctr)

cg="$(stat -fc %T /sys/fs/cgroup)"
[[ "$cg" == cgroup2fs ]] || fail "cgroup v2 required, found $cg"
echo "$cg"

"${CTR[@]}" -n "$NS" images pull -q "$IMAGE" >/dev/null 2>&1 || "${CTR[@]}" -n "$NS" images pull "$IMAGE" >/dev/null
cleanup() {
  "${CTR[@]}" -n "$NS" tasks kill -s KILL "$ID" >/dev/null 2>&1 || true
  "${CTR[@]}" -n "$NS" tasks rm -f "$ID" >/dev/null 2>&1 || true
  "${CTR[@]}" -n "$NS" containers rm "$ID" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Terminal mode is required: with gVisor's shim, non-terminal I/O (FIFO or null) hangs in
# `create` on our hosts (ADR 0007). `script` supplies the TTY that `ctr run -t` needs.
command -v script >/dev/null || fail "script (util-linux) not found"
out="$(timeout 60 script -qec "${CTR[*]} -n $NS run -t --rm --runtime $RUNTIME $IMAGE $ID /bin/sh -c 'echo runsc ok; dmesg | head -n1'" /dev/null \
        | tr -d '\r\000')" || fail "container did not finish within 60 s (see ADR 0007)"
echo "$out"
grep -qx 'runsc ok' <<<"$out" || fail "container did not print 'runsc ok'"
# Under gVisor, dmesg shows the Sentry's boot messages ("Starting gVisor...").
grep -qi gvisor <<<"$out" || fail "container did not run under gVisor (no gVisor dmesg line)"
echo "runtime: $RUNTIME on $(uname -m), runsc $(runsc --version | head -n1 | awk '{print $NF}')"
