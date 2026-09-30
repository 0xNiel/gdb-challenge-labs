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

# newest_json PREFIX JQ_FILTER — newest docs/metrics/PREFIX-*.json (names carry the date, so
# name order is date order) for which JQ_FILTER is true; empty if none.
newest_json() {
  local files=() i
  shopt -s nullglob
  files=("$ROOT/docs/metrics/$1"-*.json)
  shopt -u nullglob
  for ((i = ${#files[@]} - 1; i >= 0; i--)); do
    if jq -e "$2" "${files[i]}" >/dev/null 2>&1; then echo "${files[i]}"; return; fi
  done
}

phase_1() {
  say "images"
  check "labbase builds"                     bash "$ROOT/images/labbase/build.sh"
  check "labbase contents (images/labbase/test.sh)" bash "$ROOT/images/labbase/test.sh"
  check "perf image builds; binaries reproducible" bash "$ROOT/images/perf/build.sh"
  say "unit tests (spec golden file, invariants, cgroup parsing)"
  check "run.sh test --all" "$RUN" test --all
  say "P0 and single-lab run on this host"
  # Scratch output: this only proves P0 runs here. Committed results come from explicit
  # `LAB_HOST=<label> ./run.sh perf --scenario P0` runs (docs/STATUS.md has the commands).
  # Inside the repo (git-ignored) so the Lima VM sees the same path as the Mac.
  local scratch="$ROOT/.scratch/gate-p0"
  mkdir -p "$scratch"
  check "P0 completes (runsc and runc)" "$RUN" perf --scenario P0 --out "$scratch"
  rm -rf "$scratch"
  say "authoritative results (x86-64, ADR 0001)"
  local p0 sl
  p0="$(newest_json p0 '.arch == "x86_64" and .host != "dev-vm"')"
  if [[ -z "$p0" ]]; then
    fail "no P0 from an x86-64 host yet: on the laptop run LAB_HOST=linux-laptop ./run.sh perf --scenario P0, commit docs/metrics"
  elif jq -e '[.rows[] | select(.runtime == "runsc" and .status == "FAIL")] | length == 0' "$p0" >/dev/null; then
    pass "x86-64 P0 has no runsc FAIL rows ($(basename "$p0"))"
  else
    fail "x86-64 P0 has runsc FAIL rows ($(basename "$p0")): $(jq -r '[.rows[] | select(.runtime=="runsc" and .status=="FAIL") | .check] | join(", ")' "$p0")"
  fi
  sl="$(newest_json single-lab '.arch == "x86_64" and .host != "dev-vm"')"
  if [[ -z "$sl" ]]; then
    fail "no single-lab measurement from an x86-64 host yet: LAB_HOST=linux-laptop ./run.sh perf --scenario single-lab"
  elif jq -e '.runtimes[] | select(.runtime == "runsc") | .start_to_prompt_ms.p95 != null and .at_breakpoint != null' "$sl" >/dev/null; then
    pass "x86-64 single-lab has runsc start latency and at-breakpoint memory ($(basename "$sl"))"
  else
    fail "x86-64 single-lab lacks runsc start latency or at-breakpoint memory ($(basename "$sl"))"
  fi
}
phase_2() {
  say "unit tests (go vet, go test -race)"
  check "run.sh test --go" "$RUN" test --go
  say "integration tests (real containerd, runsc, Postgres)"
  check "run.sh test --integration" "$RUN" test --integration
  say "P4-lite and P5-lite on this host"
  # Scratch output, as in phase 1: committed numbers come from an explicit
  # `LAB_HOST=<label> ./run.sh perf --scenario P4-lite --hold 10m` run (docs/STATUS.md).
  local scratch="$ROOT/.scratch/gate-p2"
  mkdir -p "$scratch"
  check "P4-lite: 10 min churn at cap 20, no leak" "$RUN" perf --scenario P4-lite --hold 10m --out "$scratch"
  rm -rf "$scratch"
  check "P5-lite: kill -9 labd at 20 labs, all adopted within 15 s" "$RUN" perf --scenario P5-lite
  check_no_containers
  say "recorded create latency (x86-64, ADR 0001)"
  check_file "create-latency report" "$ROOT/docs/metrics/create-latency-*.md"
  local cl
  cl="$(newest_json create-latency '.arch == "x86_64" and .host != "dev-vm"')"
  if [[ -n "$cl" ]]; then pass "x86-64 create latency recorded ($(basename "$cl"))"
  else fail "no create latency from an x86-64 host yet: on the laptop run LAB_HOST=linux-laptop ./run.sh perf --scenario P4-lite --hold 10m, commit docs/metrics"; fi
}
phase_3() {
  say "unit tests (go vet, go test -race)"
  check "run.sh test --go" "$RUN" test --go
  say "integration tests (includes real gdb over the WebSocket)"
  check "run.sh test --integration" "$RUN" test --integration
  say "P1 runs on this host (2 min, scratch output)"
  local scratch="$ROOT/.scratch/gate-p3"
  mkdir -p "$scratch"
  check "P1-lite: one session replaying session.gdb over the socket" "$RUN" perf --scenario P1-lite --hold 2m --out "$scratch"
  rm -rf "$scratch"
  check_no_containers
  say "recorded P1 (x86-64, ADR 0001) and the human check"
  local p1
  p1="$(newest_json p1 '.arch == "x86_64" and .host != "dev-vm" and .echo_latency_ms.p95 != null and .duration_s >= 540')"
  if [[ -n "$p1" ]]; then pass "x86-64 P1 with echo latency p95 recorded ($(basename "$p1"))"
  else fail "no 10-minute P1 from an x86-64 host yet: on the laptop run LAB_HOST=linux-laptop ./run.sh perf --scenario P1-lite --hold 10m, commit docs/metrics"; fi
  check_status_line "Phase 3 human check"
}
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
