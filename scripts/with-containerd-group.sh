#!/usr/bin/env bash
# with-containerd-group.sh CMD [ARGS...] — run CMD as the current user with the containerd
# group, which is all labd needs to drive containerd (S10; no root). A shell started before
# provision.sh added the user to the group lacks it until the next login; then this falls
# back to `sudo -u <me> -g containerd`, which keeps the user and adds only the group.
# Used as `go test -exec` for integration tests and to start labd in the perf scripts.
set -euo pipefail
[[ $# -gt 0 ]] || { echo "usage: with-containerd-group.sh CMD [ARGS...]" >&2; exit 2; }
if id -nG | tr ' ' '\n' | grep -qx containerd; then
  exec "$@"
fi
getent group containerd >/dev/null || { echo "no containerd group on this host (./run.sh vm up)" >&2; exit 2; }
exec sudo --preserve-env -u "$(id -un)" -g containerd -- "$@"
