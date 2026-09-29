#!/usr/bin/env bash
# images/lib.sh — shared by images/*/build.sh. Source it; do not run it.
#
# Builds happen with Docker on the developer machine (macOS or Linux). The result is saved as
# an image tar and imported into containerd namespace `labs` on the lab host: the Lima VM on
# macOS, this machine on Linux (both via ./run.sh vm ssh). Default platform is the lab
# host's architecture, so each developer builds what their host runs (ADR 0001).

IMAGES_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$IMAGES_ROOT")"
RUN="$REPO_ROOT/run.sh"

img_say() { printf '==> %s\n' "$*" >&2; }
img_die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# DOCKER is how we invoke Docker: (docker) when this user can reach the socket, else
# (sudo docker) after one visible password prompt. Call docker_init before using it.
DOCKER=()
docker_init() {
  [[ ${#DOCKER[@]} -gt 0 ]] && return
  command -v docker >/dev/null || img_die "docker not found (./run.sh doctor)"
  local err
  if err="$(docker info 2>&1 >/dev/null)"; then DOCKER=(docker); return; fi
  if grep -qi 'permission denied' <<<"$err"; then
    img_say "$(id -un) cannot use the Docker socket; using sudo docker for this run."
    img_say "Permanent fix: sudo usermod -aG docker $(id -un), then log out and back in."
    img_say "(Membership of the docker group is equivalent to root on this machine.)"
    sudo -v || img_die "sudo authentication failed"
    sudo docker info >/dev/null 2>&1 \
      || img_die "docker daemon not reachable even with sudo: $(sudo docker info 2>&1 | tail -n1)"
    DOCKER=(sudo docker)
    return
  fi
  img_die "docker daemon not reachable: $(tail -n1 <<<"$err") (start it: sudo systemctl start docker)"
}

# lab_platform — linux/amd64 or linux/arm64 for the lab host (override with LAB_PLATFORM).
lab_platform() {
  if [[ -n "${LAB_PLATFORM:-}" ]]; then echo "$LAB_PLATFORM"; return; fi
  local m
  m="$("$RUN" vm ssh -- uname -m 2>/dev/null | tr -d '\r')" || img_die "lab host unreachable (./run.sh vm up)"
  case "$m" in
    x86_64) echo linux/amd64 ;;
    aarch64|arm64) echo linux/arm64 ;;
    *) img_die "unsupported lab host architecture: $m" ;;
  esac
}

# build_and_import TAG CONTEXT_DIR [docker build args...]
# Builds TAG for the lab platform, loads it into Docker (so later images can FROM it), saves
# it to images/out/, imports it into containerd namespace `labs`, and prints the digest.
build_and_import() {
  local tag="$1" ctx="$2"; shift 2
  docker_init
  local platform out safe
  platform="$(lab_platform)"
  safe="${tag//[\/:]/_}"
  out="$IMAGES_ROOT/out/${safe}_${platform#linux/}.tar"
  mkdir -p "$IMAGES_ROOT/out"

  img_say "building $tag for $platform"
  "${DOCKER[@]}" build --platform "$platform" -t "$tag" "$@" "$ctx" >&2
  img_say "saving to ${out#"$REPO_ROOT"/}"
  # Redirect in this shell (not `save -o`) so the file belongs to us even under sudo docker.
  "${DOCKER[@]}" save "$tag" >"$out"

  img_say "importing into containerd namespace labs on the lab host"
  "$RUN" vm ssh -- sudo ctr -n labs images import --platform "$platform" --local "$out" >&2 \
    || "$RUN" vm ssh -- sudo ctr -n labs images import --platform "$platform" "$out" >&2
  local ref="docker.io/${tag}"
  [[ "$tag" == */*/* ]] && ref="$tag"
  "$RUN" vm ssh -- sudo ctr -n labs images ls "name==$ref" | awk 'NR==2{print "digest:", $3, " size:", $4, $5}'
}

# image_size_mb TAG — uncompressed size in Docker, MB.
image_size_mb() { docker_init; "${DOCKER[@]}" image inspect "$1" --format '{{.Size}}' | awk '{printf "%.1f", $1/1000000}'; }
