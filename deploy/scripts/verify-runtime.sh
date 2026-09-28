#!/usr/bin/env bash
# verify-runtime.sh — prove a container runs under gVisor in namespace `labs` on this host.
# Prints "runsc ok" and "cgroup2fs" on stdout on success (the Phase 0 gate greps for both).
# Progress and diagnostics go to stderr so a hang is always visible.
#
# Env: VERIFY_TIMEOUT (seconds for the container step, default 60).
set -euo pipefail

IMAGE="docker.io/library/alpine:3.20"
NS=labs
RUNTIME=io.containerd.runsc.v1
ID="verify-$$"
TIMEOUT="${VERIFY_TIMEOUT:-60}"
SOCK=/run/containerd/containerd.sock

say()  { printf '  .. %s\n' "$*" >&2; }
fail() { printf 'verify-runtime: %s\n' "$*" >&2; exit 1; }

[[ "$(uname -s)" == Linux ]] || fail "run on Linux (./run.sh vm verify does this for you on macOS)"
for c in ctr runsc script timeout; do command -v "$c" >/dev/null || fail "$c not found — ./run.sh vm up"; done

cg="$(stat -fc %T /sys/fs/cgroup)"
[[ "$cg" == cgroup2fs ]] || fail "cgroup v2 required, found $cg"
echo "$cg"

# Root is needed unless this shell already has the containerd group. Ask for the password
# ONCE, here, on the real terminal. Everything later uses `sudo -n` (never prompts) or runs
# under a single `sudo script ...`, so no prompt can ever end up hidden inside captured output.
SUDO=()
if [[ ! -w "$SOCK" ]]; then
  SUDO=(sudo)
  if ! sudo -n true 2>/dev/null; then
    say "containerd socket is not writable by $(id -un) in this shell; using sudo."
    say "(to avoid this, log out and back in so your 'containerd' group membership applies)"
    sudo -v || fail "sudo authentication failed"
  fi
fi
ctr_() { "${SUDO[@]}" ctr -n "$NS" "$@"; }

cleanup() {
  ctr_ tasks kill -s KILL "$ID" >/dev/null 2>&1 || true
  ctr_ tasks rm -f "$ID" >/dev/null 2>&1 || true
  ctr_ containers rm "$ID" >/dev/null 2>&1 || true
}
trap cleanup EXIT

diagnose() {
  {
    echo "---- diagnostics ----"
    echo "tasks in namespace $NS:"; ctr_ tasks ls 2>&1 | sed 's/^/  /'
    echo "gVisor processes for $ID:"
    pgrep -af "runsc|containerd-shim-runsc" 2>/dev/null | grep -- "$ID" | cut -c1-200 | sed 's/^/  /' || echo "  none"
    local log="/run/containerd/io.containerd.runtime.v2.task/$NS/$ID/log.json"
    echo "runsc log ($log), last lines:"
    "${SUDO[@]}" tail -n 15 "$log" 2>/dev/null | cut -c1-250 | sed 's/^/  /' || echo "  (not found)"
    echo "containerd journal for $ID:"
    "${SUDO[@]}" journalctl -u containerd --since "-5 min" --no-pager 2>/dev/null \
      | grep -F -- "$ID" | tail -n 15 | cut -c1-250 | sed 's/^/  /' || echo "  (no entries)"
    echo "---------------------"
  } >&2
}

say "pulling $IMAGE into namespace $NS (first run downloads ~3 MB)"
ctr_ images pull "$IMAGE" >/dev/null || fail "image pull failed — offline, or docker.io blocked?"

# Terminal mode is required: with gVisor's shim, non-terminal I/O (FIFO or null) hangs in
# `create` (ADR 0007). `script` supplies the TTY that `ctr run -t` needs. The whole `script`
# runs under sudo when needed, so ctr inside it never has to authenticate.
say "starting a container under $RUNTIME (timeout ${TIMEOUT}s)"
raw="$(mktemp)"
trap 'cleanup; rm -f "$raw"' EXIT
set +e
timeout "$TIMEOUT" "${SUDO[@]}" script -qec \
  "ctr -n $NS run -t --rm --runtime $RUNTIME $IMAGE $ID /bin/sh -c 'echo runsc ok; dmesg | head -n1'" /dev/null >"$raw"
rc=$?
set -e
# PTY output: drop CR and NUL bytes, and the literal "^@" a terminal echoes for a NUL.
out="$(tr -d '\r\000' <"$raw" | sed 's/\^@//g')"
if [[ $rc -eq 124 ]]; then
  diagnose
  fail "container did not finish within ${TIMEOUT}s (see ADR 0007; diagnostics above)"
fi
echo "$out"
grep -q '^runsc ok$' <<<"$out" || { diagnose; fail "container did not print 'runsc ok' (exit $rc)"; }
# Under gVisor, dmesg shows the Sentry's boot messages ("Starting gVisor...").
grep -qi gvisor <<<"$out" || fail "container did not run under gVisor (no gVisor dmesg line)"
echo "runtime: $RUNTIME on $(uname -m), runsc $(runsc --version | sed -n 1p | awk '{print $NF}')"
