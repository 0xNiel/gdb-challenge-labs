#!/usr/bin/env bash
# gate.sh <phase> — the exit test for a phase. Exit 0 only when every check passes.
# Each phase_N function mirrors the "Gate" section of docs/plan/phase-N-*.md. Keep them in sync.
# A phase whose gate is not wired yet exits 2 with a pointer to its document (ADR 0006).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PHASE="${1:?usage: gate.sh <phase 0..8>}"
RUN="$ROOT/run.sh"
STATUS="$ROOT/docs/STATUS.md"
FAILED=0

say()  { printf '\n==> [gate %s] %s\n' "$PHASE" "$*"; }
pass() { printf '  PASS  %s\n' "$*"; }
fail() { printf '  FAIL  %s\n' "$*"; FAILED=1; }

# check "description" command args...
check() {
  local desc="$1"; shift
  if "$@" >/tmp/gate-$$.log 2>&1; then pass "$desc"
  else fail "$desc"; sed 's/^/        /' /tmp/gate-$$.log | tail -n 20; fi
  rm -f /tmp/gate-$$.log
}

# check_file "description" glob   — at least one file matches
check_file() {
  local desc="$1" pattern="$2"
  if compgen -G "$pattern" >/dev/null; then pass "$desc ($(compgen -G "$pattern" | tail -n1 | xargs basename))"
  else fail "$desc — no file matches $pattern"; fi
}

# check_status_line "Phase 3 human check"  — STATUS.md must contain a line with that text and "OK"
check_status_line() {
  local text="$1"
  if grep -Eq "^.*${text}.*OK" "$STATUS"; then pass "STATUS.md records '$text'"
  else fail "STATUS.md lacks a line matching '$text ... OK'"; fi
}

check_clean_tree() {
  if [[ -z "$(git -C "$ROOT" status --porcelain)" ]]; then pass "working tree clean"
  else fail "working tree has uncommitted changes"; git -C "$ROOT" status --short | head -n 20; fi
}

check_no_containers() {
  local count
  count="$("$RUN" vm ssh -- sudo ctr -n labs c ls -q 2>/dev/null | wc -l | tr -d ' ')" || count="?"
  if [[ "$count" == "0" ]]; then pass "no containers left in namespace labs"
  else fail "$count containers left in namespace labs"; fi
}

not_wired() {
  local doc
  doc="$(compgen -G "$ROOT/docs/plan/phase-$PHASE-*.md" | head -n1 || true)"
  printf 'gate for phase %s is not wired yet.\nImplement phase_%s in scripts/gate.sh as the last task of the phase.\nSee: %s (section "Gate").\n' "$PHASE" "$PHASE" "${doc:-docs/plan/}"
  exit 2
}

# ---------------------------------------------------------------- phases

phase_0() {
  say "toolchain";        check "run.sh check" "$RUN" check
  say "unit tests";       check "run.sh test --all" "$RUN" test --all
  say "runtime in VM";    check "run.sh vm verify prints runsc ok + cgroup2fs" \
                            bash -c "'$RUN' vm verify 2>&1 | grep -q 'runsc ok' && '$RUN' vm verify 2>&1 | grep -q cgroup2fs"
  say "repository";       check_clean_tree
}

phase_1() { not_wired; }   # Phase 1, task 1.8
phase_2() { not_wired; }   # Phase 2, task 2.13
phase_3() { not_wired; }   # Phase 3, task 3.11
phase_4() { not_wired; }   # Phase 4, task 4.13
phase_5() { not_wired; }   # Phase 5, task 5.15
phase_6() { not_wired; }   # Phase 6, task 6.13
phase_7() { not_wired; }   # Phase 7, task 7.9
phase_8() { not_wired; }   # Phase 8, task 8.12

# ---------------------------------------------------------------- main

[[ "$PHASE" =~ ^[0-8]$ ]] || { echo "phase must be 0..8" >&2; exit 1; }
printf '==> gate for phase %s — %s — %s\n' "$PHASE" "$(date -u +%Y-%m-%dT%H:%MZ)" "$(hostname)"
"phase_$PHASE"
echo
if [[ $FAILED -eq 0 ]]; then
  printf '==> GATE %s PASSED. Paste this output into docs/STATUS.md.\n' "$PHASE"
else
  printf '==> GATE %s FAILED. Fix the FAIL lines above; do not start the next phase.\n' "$PHASE"
  exit 1
fi
