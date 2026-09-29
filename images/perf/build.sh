#!/usr/bin/env bash
# Build the perf image for the lab host's architecture and import it into containerd.
#   1. Build the toolchain image (images/build).
#   2. Compile perf and probe twice in it; the binaries must be byte-identical (task 1.3).
#   3. Generate perf.core by crashing perf under gdb in the build container.
#   4. Stage /opt/perf (binaries, source, core, session.gdb) and build FROM labbase.
set -euo pipefail
# shellcheck source=../lib.sh disable=SC1091
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib.sh"

PERF="$IMAGES_ROOT/perf"
STAGE="$IMAGES_ROOT/out/perf-context"
# -fdebug-prefix-map: sources are compiled from /src but live at /opt/perf in the image.
CFLAGS="-O0 -g -no-pie -fno-pie -fno-stack-protector -fdebug-prefix-map=/src=/opt/perf"

bash "$IMAGES_ROOT/build/build.sh"
platform="$(lab_platform)"

compile() { # compile OUTDIR — build perf and probe into OUTDIR using the toolchain image
  local out="$1"
  mkdir -p "$out"
  docker run --rm --platform "$platform" -e SOURCE_DATE_EPOCH=0 \
    -v "$PERF/src:/src:ro" -v "$out:/out" -w /src gdblabs/build:dev sh -ec "
      gcc $CFLAGS -pthread -o /out/perf perf.c
      gcc $CFLAGS -o /out/probe probe.c
    " >&2
}

img_say "compiling perf and probe twice (reproducibility check)"
rm -rf "$IMAGES_ROOT/out/perf-a" "$IMAGES_ROOT/out/perf-b"
compile "$IMAGES_ROOT/out/perf-a"
compile "$IMAGES_ROOT/out/perf-b"
for b in perf probe; do
  ha="$(shasum -a 256 "$IMAGES_ROOT/out/perf-a/$b" | awk '{print $1}')"
  hb="$(shasum -a 256 "$IMAGES_ROOT/out/perf-b/$b" | awk '{print $1}')"
  [[ "$ha" == "$hb" ]] || img_die "$b is not reproducible: $ha != $hb"
  img_say "$b reproducible: sha256 $ha"
done

img_say "generating perf.core (perf crash, under gdb in the build container)"
docker run --rm --platform "$platform" -v "$IMAGES_ROOT/out/perf-a:/out" -w /out gdblabs/build:dev \
  gdb -batch -ex run -ex "generate-core-file /out/perf.core" --args /out/perf crash >/dev/null 2>&1 || true
[[ -s "$IMAGES_ROOT/out/perf-a/perf.core" ]] || img_die "perf.core was not generated"

rm -rf "$STAGE"
mkdir -p "$STAGE/opt/perf"
cp "$IMAGES_ROOT/out/perf-a/perf" "$IMAGES_ROOT/out/perf-a/probe" "$IMAGES_ROOT/out/perf-a/perf.core" "$STAGE/opt/perf/"
cp "$PERF/src/perf.c" "$PERF/src/probe.c" "$PERF/session.gdb" "$STAGE/opt/perf/"
cp "$PERF/Dockerfile" "$STAGE/Dockerfile"
chmod 0755 "$STAGE/opt/perf/perf" "$STAGE/opt/perf/probe"
chmod 0644 "$STAGE/opt/perf/perf.core" "$STAGE/opt/perf/"*.c "$STAGE/opt/perf/session.gdb"

build_and_import gdblabs/perf:dev "$STAGE"
