#!/usr/bin/env bash
# challenge-new.sh <tier-dir> <NN-slug> — scaffolds challenges/<tier-dir>/<NN-slug>/ from
# challenges/TEMPLATE with the slug, tier, order and entry filled in.
#
#   scripts/challenge-new.sh tier1-c-fundamentals 06-double-free
#
# Then follow challenges/README.md and build with scripts/challenge-build.sh <dir>.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ $# -eq 2 ]] || { echo "usage: challenge-new.sh <tierN-name> <NN-slug>" >&2; exit 2; }
TIER_DIR="$1" NAME="$2"
[[ "$TIER_DIR" =~ ^tier([1-9])-[a-z0-9-]+$ ]] || { echo "tier directory must look like tier1-c-fundamentals" >&2; exit 2; }
TIER="${BASH_REMATCH[1]}"
[[ "$NAME" =~ ^([0-9]{2})-([a-z0-9]+(-[a-z0-9]+)*)$ ]] || { echo "challenge must look like 06-double-free" >&2; exit 2; }
NN="${BASH_REMATCH[1]}" SHORT="${BASH_REMATCH[2]}"
DEST="$ROOT/challenges/$TIER_DIR/$NAME"
[[ ! -e "$DEST" ]] || { echo "$DEST exists" >&2; exit 1; }
mkdir -p "$(dirname "$DEST")"
cp -R "$ROOT/challenges/TEMPLATE" "$DEST"
ENTRY="${SHORT//-/_}"
sed -i.bak \
  -e "s|^# TEMPLATE .*|# $TIER_DIR/$NAME — challenges/README.md explains every key.|" \
  -e '/^# (challenges\/schema/d' -e '/^# explains each one/d' \
  -e "s|^slug: .*|slug: tier$TIER-$NN-$SHORT|" \
  -e "s|^title: .*|title: ${SHORT//-/ }|" \
  -e "s|^tier: .*|tier: $TIER|" \
  -e "s|^order: .*|order: $((10#$NN))|" \
  -e "s|^tags: .*|tags: [c]|" \
  -e "s|^  entry: .*|  entry: $ENTRY|" \
  "$DEST/manifest.yaml"
rm -f "$DEST/manifest.yaml.bak"
sed -i.bak -e "s|/opt/lab/template|/opt/lab/$ENTRY|g" -e "s|template|$ENTRY|g" "$DEST/README.md" && rm -f "$DEST/README.md.bak"
echo "created challenges/$TIER_DIR/$NAME (slug tier$TIER-$NN-$SHORT, binary /opt/lab/$ENTRY)"
echo "next: write src/main.c, manifest key_expr/key_value/hints, solve.gdb, README.md, lesson.md, solution.md;"
echo "      then scripts/challenge-build.sh challenges/$TIER_DIR/$NAME"
