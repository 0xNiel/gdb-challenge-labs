#!/usr/bin/env bash
# challenges-json.sh [--local] — writes challenges.json to stdout from every
# challenges/tier*/*/manifest.yaml, validated against challenges/schema/challenges.schema.json.
#
#   scripts/challenges-json.sh > challenges.json            # committed: pushed images only
#   scripts/challenges-json.sh --local > .scratch/challenges.local.json
#
# Without --local, a challenge whose manifest has no pushed image is listed disabled (labd
# ignores it). With --local, this host's dev builds (.scratch/local-images.json, written by
# scripts/challenge-build.sh) provide the images, for running labd against them.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
args=(--root "$ROOT")
if [[ "${1:-}" == --local ]]; then
  [[ -s "$ROOT/.scratch/local-images.json" ]] || { echo "no .scratch/local-images.json: build challenges first (scripts/challenge-build.sh)" >&2; exit 1; }
  args+=(--local "$ROOT/.scratch/local-images.json")
fi
(cd "$ROOT/labd" && go build -o bin/challengesjson ./cmd/challengesjson)
exec "$ROOT/labd/bin/challengesjson" "${args[@]}"
