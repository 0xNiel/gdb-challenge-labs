#!/usr/bin/env bash
# web-metrics.sh — writes docs/metrics/web-<date>-<host>.{json,md} from the newest end-to-end
# run on this host (.scratch/e2e/web-metrics.json, written by ./run.sh test --e2e): the time
# from clicking Start to the terminal page (Django's start request and redirect) and to the
# shell prompt (Phase 6 "Metrics to record"; spec target p95 < 2 s). Label the host with
# LAB_HOST (e.g. linux-laptop). Only x86-64 under runsc counts (ADR 0001).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/.scratch/e2e/web-metrics.json"
[[ -s "$SRC" ]] || { echo "no end-to-end run recorded yet: ./run.sh test --e2e first" >&2; exit 1; }
HOST="${LAB_HOST:-$(hostname -s)}" STAMP="$(date -u +%Y-%m-%d)"
OUT="$ROOT/docs/metrics/web-$STAMP-$HOST"
jq --arg host "$HOST" --arg date "$STAMP" '{host: $host, date: $date} + .' "$SRC" > "$OUT.json"
{
  echo "# Web start latency — $HOST, $STAMP"
  echo
  echo "From \`./run.sh test --e2e\` (\`web/tests/e2e/test_tier1.py\`): headless Chromium on the lab host, Django and labd from \`scripts/web-stack.sh\`, challenge \`$(jq -r .challenge "$OUT.json")\`, labs under $(jq -r .runtime "$OUT.json") on $(jq -r .arch "$OUT.json"), $(jq -r .runs "$OUT.json") runs. Raw data in \`$(basename "$OUT").json\`."
  echo
  echo "| Measure | p50 | p95 | max | Spec |"
  echo "| --- | --- | --- | --- | --- |"
  jq -r '"| Click Start to the terminal page (Django start request, labd create call, redirect) | \(.start_to_terminal_page_ms.p50) ms | \(.start_to_terminal_page_ms.p95) ms | \(.start_to_terminal_page_ms.max) ms | — |"' "$OUT.json"
  jq -r '"| Click Start to the shell prompt in the browser | \(.click_to_prompt_ms.p50) ms | \(.click_to_prompt_ms.p95) ms | \(.click_to_prompt_ms.max) ms | p95 < 2000 ms |"' "$OUT.json"
} > "$OUT.md"
echo "wrote ${OUT#"$ROOT"/}.{json,md}" >&2
