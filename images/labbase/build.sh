#!/usr/bin/env bash
# Build labbase for the lab host's architecture and import it into containerd (ns labs).
# Usage: images/labbase/build.sh        (LAB_PLATFORM=linux/amd64 to force a platform)
set -euo pipefail
# shellcheck source=../lib.sh disable=SC1091
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib.sh"

build_and_import gdblabs/labbase:dev "$IMAGES_ROOT/labbase"
img_say "labbase uncompressed size: $(image_size_mb gdblabs/labbase:dev) MB (spec target < 45 MB)"
