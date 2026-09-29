#!/usr/bin/env bash
# Build the toolchain image into Docker only. It compiles lab binaries on developer machines
# and is never imported into containerd or shipped anywhere.
set -euo pipefail
# shellcheck source=../lib.sh disable=SC1091
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib.sh"

docker_init
platform="$(lab_platform)"
img_say "building gdblabs/build:dev for $platform (Docker only)"
"${DOCKER[@]}" build --platform "$platform" -t gdblabs/build:dev "$IMAGES_ROOT/build" >&2
