#!/usr/bin/env bash
# challenges-changed.sh [<base-ref>] [--list] — rebuilds the challenges that changed since
# <base-ref> (default origin/main), the local replacement for the spec's CI job (ADR 0008).
#
# Changed means: committed since the merge base with <base-ref>, staged, unstaged, or
# untracked. A change under one challenge's directory rebuilds that challenge. A change to
# anything every challenge is built from rebuilds all of them: images/labbase/,
# images/build/, scripts/challenge-build.sh, labd/internal/flag/, labd/cmd/flagblob/.
# challenges/TEMPLATE is never built here. --list prints the plan and builds nothing.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE=origin/main LIST=0
for a in "$@"; do
  case "$a" in
    --list) LIST=1 ;;
    -*) echo "usage: challenges-changed.sh [<base-ref>] [--list]" >&2; exit 2 ;;
    *) BASE="$a" ;;
  esac
done
cd "$ROOT"
git rev-parse --verify -q "$BASE" >/dev/null || { echo "no such ref: $BASE" >&2; exit 2; }
changed="$( { git diff --name-only "$(git merge-base "$BASE" HEAD)" HEAD; git diff --name-only --cached; git diff --name-only;
  git ls-files --others --exclude-standard; } | sort -u)"

all=()
while IFS= read -r m; do all+=("$(dirname "$m")"); done < <(ls -1 challenges/tier*/*/manifest.yaml 2>/dev/null)

todo=()
if grep -qE '^(images/labbase/|images/build/|scripts/challenge-build\.sh$|labd/internal/flag/|labd/cmd/flagblob/)' <<<"$changed"; then
  echo "==> the base image, toolchain or build tooling changed: rebuilding all ${#all[@]} challenges" >&2
  todo=("${all[@]}")
else
  for d in "${all[@]}"; do
    grep -q "^$d/" <<<"$changed" && todo+=("$d")
  done
fi
if ((${#todo[@]} == 0)); then echo "==> no challenge changed since $BASE" >&2; exit 0; fi
printf '%s\n' "${todo[@]}"
((LIST)) && exit 0
failed=()
for d in "${todo[@]}"; do
  bash "$ROOT/scripts/challenge-build.sh" "$d" || failed+=("$d")
done
if ((${#failed[@]})); then echo "==> failed: ${failed[*]}" >&2; exit 1; fi
echo "==> rebuilt ${#todo[@]}; regenerate challenges.json (scripts/challenges-json.sh) if images changed" >&2
