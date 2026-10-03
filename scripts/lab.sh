#!/usr/bin/env bash
# lab.sh up|down|status — start and stop the whole app with one command (make lab-up/lab-down).
#
#   up      check, in order, everything the app needs: host tools, the lab host (the Lima VM
#           on macOS, this machine on Linux), its services, the five lab images, and that
#           nothing is in the way. Stops at the first failed check and prints the command that
#           fixes it. When every check passes, starts the app (scripts/web-stack.sh up).
#   down    stop Django and labd and remove any lab left (scripts/web-stack.sh down).
#   status  what is running.
#
# Runs on the host (macOS or Linux); everything on the lab host goes through ./run.sh vm ssh.
# Checks only: it never installs, builds or starts anything except the app itself in the last
# step. Works on macOS's bash 3.2.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN="$ROOT/run.sh"
VM_NAME="${LABS_VM:-labs}"
LOCAL_IMAGES="$ROOT/.scratch/local-images.json"
URL="http://127.0.0.1:${WEB_PORT:-8000}"
STEPS=7

is_darwin() { [[ "$(uname -s)" == Darwin ]]; }
on_host() { "$RUN" vm ssh -- "$@"; }  # run on the lab host: the VM on macOS, locally on Linux

step() { printf '==> [%d/%d] %s\n' "$1" "$STEPS" "$2" >&2; }
ok()   { printf '    ok    %s\n' "$*" >&2; }

# fail WHAT FIX... — say what is wrong, print each FIX line as a command to run, exit 1.
fail() {
  local what="$1"; shift
  printf '    FAIL  %s\n\nTo fix it, run:\n\n' "$what" >&2
  local line
  for line in "$@"; do printf '    %s\n' "$line" >&2; done
  printf '\nThen run make lab-up again.\n' >&2
  exit 1
}

vm_state() { # Running, Stopped, ... or "absent"
  limactl list --format '{{.Name}} {{.Status}}' 2>/dev/null | awk -v n="$VM_NAME" '$1 == n {print $2; f=1} END{if(!f) print "absent"}'
}

# stack_state — "up", "down" or "partial", from web-stack.sh status on the lab host.
stack_state() {
  local s n
  s="$("$RUN" web-stack status 2>/dev/null || true)"
  n="$(grep -c ': running' <<<"$s" || true)"
  case "$n" in 2) echo up ;; 0) echo down ;; *) echo partial ;; esac
}

check_tools() {
  step 1 "host tools"
  "$RUN" check >/dev/null 2>&1 || fail "a required tool is missing on this machine" \
    "./run.sh doctor          # lists what is missing and the command that installs each"
  ok "every required tool is installed"
}

check_lab_host() {
  step 2 "lab host"
  if ! is_darwin; then ok "this Linux machine is the lab host"; return; fi
  case "$(vm_state)" in
    Running) ok "Lima VM '$VM_NAME' is running" ;;
    absent)  fail "the Lima VM '$VM_NAME' does not exist" \
               "./run.sh vm up           # creates and provisions it (the first run downloads Ubuntu)" ;;
    *)       fail "the Lima VM '$VM_NAME' is not running" \
               "./run.sh vm up           # starts it; provisioning again is safe" ;;
  esac
}

check_provisioned() {
  step 3 "lab host provisioned"
  local missing
  # shellcheck disable=SC2016  # the script expands on the lab host, not here
  missing="$(on_host bash -c 'for c in containerd ctr runsc go uv jq; do command -v "$c" >/dev/null || printf "%s " "$c"; done
    getent group containerd >/dev/null || printf "containerd-group "' 2>/dev/null)" \
    || fail "cannot run commands on the lab host" "./run.sh vm up"
  [[ -z "$missing" ]] || fail "the lab host is missing: $missing" \
    "./run.sh vm up           # installs containerd, gVisor, Postgres, Go and uv (sudo asks once)"
  ok "containerd, runsc, Go, uv and jq are installed"
}

check_services() {
  step 4 "lab host services"
  local prefix=""
  is_darwin && prefix="./run.sh vm ssh -- "
  on_host systemctl is-active --quiet containerd \
    || fail "containerd is not running" "${prefix}sudo systemctl start containerd"
  on_host systemctl is-active --quiet postgresql \
    || fail "Postgres is not running" "./run.sh db up"
  ok "containerd and Postgres are running"
}

# docker_fix — the command that starts Docker, if its daemon is not reachable (challenge builds need it).
docker_fix() {
  docker info >/dev/null 2>&1 && return 0
  if is_darwin; then echo "open -a Docker            # start Docker Desktop, wait until it is running"
  else echo "sudo systemctl start docker"; fi
}

check_images() {
  step 5 "lab images"
  local arch
  case "$(on_host uname -m)" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) arch="$(on_host uname -m)" ;; esac

  # Digests containerd holds in namespace labs. ctr needs root on the lab host; when sudo
  # would prompt, skip this half of the check (web-stack up's preflight asks for sudo anyway).
  local digests="" can_list=0
  # shellcheck disable=SC2016  # the awk program runs on the lab host
  if digests="$(on_host bash -c 'sudo -n ctr -n labs images ls 2>/dev/null | awk "NR>1 {print \$3}"' 2>/dev/null)" \
     && [[ -n "$digests" ]]; then
    can_list=1
  fi

  local manifest dir slug ref missing=() fixes=()
  for manifest in "$ROOT"/challenges/tier*/*/manifest.yaml; do
    [[ -f "$manifest" ]] || continue
    dir="${manifest%/manifest.yaml}"; dir="${dir#"$ROOT"/}"
    slug="$(awk '/^slug:/ {print $2; exit}' "$manifest")"
    ref=""
    [[ -s "$LOCAL_IMAGES" ]] && ref="$(jq -r --arg s "$slug" --arg a "$arch" \
      '.[$s] | select(. != null and .arch == $a) | .image' "$LOCAL_IMAGES")"
    if [[ -z "$ref" ]] || { ((can_list)) && ! grep -qx "${ref##*@}" <<<"$digests"; }; then
      missing+=("$slug")
      fixes+=("./run.sh images challenge $dir")
    fi
  done

  if ((${#missing[@]})); then
    local d; d="$(docker_fix || true)"
    if [[ -n "$d" ]]; then fixes=("$d" "${fixes[@]}"); fi
    fail "${#missing[@]} lab image(s) not built for this lab host ($arch): ${missing[*]}" "${fixes[@]}"
  fi
  if ((can_list)); then ok "every lab image is built for $arch and loaded in containerd"
  else ok "every lab image is built for $arch (containerd not checked: sudo would prompt)"; fi
}

check_free() {
  step 6 "nothing in the way"
  case "$(stack_state)" in
    up)      ok "the app is already running"; printf '\n==> up: %s\n' "$URL" >&2; exit 0 ;;
    partial) fail "the app is half running (one of labd and Django is up)" "make lab-down" ;;
  esac
  local pids
  pids="$(on_host pgrep -x labd 2>/dev/null | tr '\n' ' ' || true)"
  [[ -z "$pids" ]] || fail "a labd started outside make lab-up is running (pid $pids)" \
    "./run.sh vm ssh -- kill $pids    # or Ctrl-C in the terminal that runs it"

  local busy
  busy="$(on_host ss -ltnH 2>/dev/null | awk '{print $4}' | grep -oE ':(8000|8081|8082)$' | tr -d : | sort -u | tr '\n' ' ' || true)"
  [[ -z "$busy" ]] || fail "port(s) $busy already in use on the lab host" \
    "./run.sh vm ssh -- ss -ltnp    # find the process, then stop it"
  if is_darwin && command -v lsof >/dev/null; then
    local port="${URL##*:}" mac
    mac="$(lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | awk 'NR>1 && $1 !~ /^(limactl|ssh)$/ {print $1, $2}' | sort -u || true)"
    [[ -z "$mac" ]] || fail "port $port is in use on this Mac by: $(awk '{printf "%s%s (pid %s)", s, $1, $2; s=", "}' <<<"$mac")" \
      "kill $(awk '{print $2}' <<<"$mac" | tr '\n' ' ')   # the VM forwards $port to this Mac, so it must be free"
  fi
  ok "ports 8000, 8081 and 8082 are free; no stray labd"
}

start() {
  step 7 "start the app"
  "$RUN" web-stack up || {
    printf '\n    FAIL  the app did not start (message above; logs in .scratch/web-stack/)\n\nTo fix it, run:\n\n    make lab-down\n\nThen read the message above and run make lab-up again.\n' >&2
    exit 1
  }
  printf '\nOpen %s, sign up with any email, and start lab 1. Stop with: make lab-down\n' "$URL" >&2
}

up() {
  check_tools
  check_lab_host
  check_provisioned
  check_services
  check_images
  check_free
  start
}

down() {
  if is_darwin && [[ "$(vm_state)" != Running ]]; then
    printf '==> the Lima VM is not running, so neither is the app\n' >&2
    return 0
  fi
  "$RUN" web-stack down
  printf '==> down\n' >&2
  if is_darwin; then printf '    The VM is still running. make vm-down stops it too.\n' >&2; fi
}

status() {
  if is_darwin; then
    printf 'lima vm %s: %s\n' "$VM_NAME" "$(vm_state)"
    [[ "$(vm_state)" == Running ]] || return 0
  fi
  "$RUN" web-stack status
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  status) status ;;
  *) printf 'usage: lab.sh up|down|status\n' >&2; exit 2 ;;
esac
