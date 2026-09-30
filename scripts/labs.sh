#!/usr/bin/env bash
# labs.sh ls|clean [--yes]|preflight — labs in containerd namespace `labs` on this Linux lab
# host (run.sh routes it into the VM on macOS as `./run.sh labs ...`).
#
#   ls         list the lab containers, their session id, user and task status
#   clean      remove every lab container, after one confirmation (--yes skips it). Refuses
#              while a labd runs: that labd owns the labs, and its session rows would disagree.
#              The sessions' rows are closed by the next labd's reconcile (reason `reconciled`).
#   preflight  exit 1, saying what to do, if a labd is running or any lab is left. The gate
#              and the perf scripts that start a private labd run it first: two labds would
#              reconcile each other's labs away, and a leftover lab would be adopted and
#              counted in the measurements.
#
# ctr needs root even for the containerd group (ADR 0007), so this authenticates sudo once,
# visibly, before any captured output.
set -euo pipefail

NS=labs
say() { printf '==> %s\n' "$*" >&2; }
die() { printf 'labs: %s\n' "$*" >&2; exit 1; }

[[ "$(uname -s)" == Linux ]] || die "run on the Linux lab host: ./run.sh labs ${1:-ls}"

sudo_once() {
  if ! sudo -n true 2>/dev/null; then say "ctr needs root to see namespace $NS; authenticating sudo once"; sudo -v; fi
}

# labd_pids — every labd process on the host, any user, any config.
labd_pids() { pgrep -x labd | tr '\n' ' ' | sed 's/ $//' || true; }

containers() { sudo ctr -n "$NS" containers ls -q; }

# task_status ID — RUNNING, STOPPED, ... or "no task".
task_status() {
  sudo ctr -n "$NS" tasks ls 2>/dev/null | awk -v id="$1" '$1 == id {print $3; found=1} END{if(!found) print "no task"}'
}

label() { # ID KEY
  sudo ctr -n "$NS" containers info "$1" 2>/dev/null | jq -r --arg k "$2" '.Labels[$k] // "-"'
}

list() {
  local ids id
  ids="$(containers)"
  if [[ -z "$ids" ]]; then echo "namespace $NS is empty"; return; fi
  printf '%-44s  %-8s  %-10s  %s\n' CONTAINER USER TASK CREATED
  for id in $ids; do
    printf '%-44s  %-8s  %-10s  %s\n' "$id" "$(label "$id" lab.user_id)" "$(task_status "$id")" "$(label "$id" lab.created_at)"
  done
}

running_labd_help() {
  cat >&2 <<EOF
labd is running (pid $1). Stop it first:
  - started with ./run.sh labd: stop its sessions (curl -X DELETE .../internal/sessions/<id>), then Ctrl-C in its terminal;
  - otherwise: kill -TERM $1
Labs outlive labd on purpose; after it stops, ./run.sh labs clean removes what is left.
EOF
}

cmd_ls() { sudo_once; list; }

cmd_clean() {
  local yes=0 pids ids id n=0 st i
  [[ "${1:-}" == --yes ]] && yes=1
  pids="$(labd_pids)"
  if [[ -n "$pids" ]]; then running_labd_help "$pids"; exit 1; fi
  sudo_once
  ids="$(containers)"
  if [[ -z "$ids" ]]; then echo "namespace $NS is empty; nothing to do"; return; fi
  list
  if [[ $yes == 0 ]]; then
    read -r -p "Remove these $(wc -w <<<"$ids" | tr -d ' ') labs? [y/N] " ans
    [[ "$ans" == y || "$ans" == Y ]] || die "nothing removed"
  fi
  for id in $ids; do
    st="$(task_status "$id")"
    if [[ "$st" != "no task" ]]; then
      [[ "$st" == STOPPED ]] || sudo ctr -n "$NS" tasks kill -s SIGKILL "$id" >/dev/null 2>&1 || true
      for ((i = 0; i < 50; i++)); do [[ "$(task_status "$id")" =~ ^(STOPPED|no\ task)$ ]] && break; sleep 0.2; done
      sudo ctr -n "$NS" tasks delete -f "$id" >/dev/null 2>&1 || true
    fi
    if sudo ctr -n "$NS" containers delete "$id" >/dev/null; then n=$((n + 1)); echo "removed $id"
    else echo "could not remove $id" >&2; fi
  done
  echo "removed $n labs; the next labd closes their session rows (reason reconciled)"
  [[ -z "$(containers)" ]] || die "namespace $NS is still not empty: $(containers | tr '\n' ' ')"
}

cmd_preflight() {
  local pids ids bad=0
  pids="$(labd_pids)"
  if [[ -n "$pids" ]]; then
    printf 'preflight FAIL: a labd is running (pid %s); a second labd in namespace %s would reconcile its labs away.\n' "$pids" "$NS" >&2
    running_labd_help "$pids"
    bad=1
  fi
  sudo_once
  ids="$(containers)"
  if [[ -n "$ids" ]]; then
    printf 'preflight FAIL: %s labs left in namespace %s:\n' "$(wc -w <<<"$ids" | tr -d ' ')" "$NS" >&2
    list >&2
    printf 'After labd has stopped, remove them: ./run.sh labs clean\n' >&2
    bad=1
  fi
  [[ $bad == 0 ]] || exit 1
  say "preflight ok: no labd running, namespace $NS empty"
}

case "${1:-ls}" in
  ls) cmd_ls ;;
  clean) shift; cmd_clean "$@" ;;
  preflight) cmd_preflight ;;
  *) die "usage: labs.sh ls|clean [--yes]|preflight" ;;
esac
