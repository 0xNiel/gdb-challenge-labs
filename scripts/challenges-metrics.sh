#!/usr/bin/env bash
# challenges-metrics.sh — writes docs/metrics/challenges-<date>-<host>.{json,md} from the
# newest build of each challenge on this host (.scratch/challenge-metrics.jsonl, appended by
# scripts/challenge-build.sh): image layer over labbase, build time, oracle time and runtime
# (Phase 5 "Metrics to record"). Label the host with LAB_HOST (e.g. linux-laptop).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/.scratch/challenge-metrics.jsonl"
[[ -s "$SRC" ]] || { echo "no builds recorded yet: run scripts/challenge-build.sh first" >&2; exit 1; }
HOST="${LAB_HOST:-$(hostname -s)}" STAMP="$(date -u +%Y-%m-%d)"
OUT="$ROOT/docs/metrics/challenges-$STAMP-$HOST"
slugs=()  # (no mapfile: macOS ships bash 3.2)
for m in "$ROOT"/challenges/tier*/*/manifest.yaml; do slugs+=("$(sed -n 's/^slug: *//p' "$m")"); done
jq -s --arg host "$HOST" --arg date "$STAMP" --arg slugs "${slugs[*]}" '
  ($slugs | split(" ")) as $want
  | [ $want[] as $s | (map(select(.slug == $s)) | last) ] | map(select(. != null)) as $rows
  | {host: $host, date: $date, arch: ($rows[0].arch // null), challenges: $rows}' "$SRC" > "$OUT.json"
n="$(jq '.challenges | length' "$OUT.json")"
{
  echo "# Challenge builds — $HOST, $STAMP"
  echo
  echo "Newest \`scripts/challenge-build.sh\` run of each challenge on \`$HOST\` (all steps passed). Raw data in \`$(basename "$OUT").json\`."
  echo
  echo "| Challenge | Arch | Oracle runtime | Layer over labbase | Build, all steps | Oracle (solve.gdb) | Binary sha256 |"
  echo "| --- | --- | --- | --- | --- | --- | --- |"
  jq -r '.challenges[] | "| `\(.slug)` | \(.arch) | \(.runtime) | \(.layer_kib) KiB | \(.build_s) s | \(.oracle_s) s | `\(.sha256[0:16])…` |"' "$OUT.json"
} > "$OUT.md"
echo "wrote ${OUT#"$ROOT"/}.{json,md}: $n of ${#slugs[@]} challenges" >&2
