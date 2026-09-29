#!/usr/bin/env bash
# p0.sh — P0: gdb feature checks and sandbox checks under gVisor (and runc for reference).
# Spec: "Local performance test suite → Scenarios P0". Plan: docs/plan/phase-1-gvisor-gdb-spike.md 1.5.
#
# Usage (on the Linux lab host; ./run.sh perf --scenario P0 does this for you):
#   labd/perf/p0/p0.sh [--runtime runsc|runc|both] [--host LABEL] [--out DIR]
#
# Every check runs in a fresh container from the perf image under the real sandbox spec,
# via labd/bin/specrun (always with a terminal, ADR 0007). Each row gets a status:
#   PASS      works as required
#   FALLBACK  does not work, and a documented fallback is in force (text in the row)
#   FAIL      does not work and nothing covers it
#   INFO      recorded for reference, no verdict
# Output: DIR/p0-<date>-<host>.json and .md. Exit 0 when the script ran (the verdict is in the
# files; the Phase 1 gate reads them).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
LABD="$ROOT/labd"
SPECRUN="$LABD/bin/specrun"
SPEC="$LABD/sandbox/sandbox-base.json"
IMAGE="${P0_IMAGE:-docker.io/gdblabs/perf:dev}"
SRC="$ROOT/images/perf/src/perf.c"

RUNTIMES="both"
HOST=""
OUT="$ROOT/docs/metrics"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --runtime) RUNTIMES="$2"; shift 2 ;;
    --host) HOST="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    *) echo "usage: p0.sh [--runtime runsc|runc|both] [--host LABEL] [--out DIR]" >&2; exit 2 ;;
  esac
done
[[ "$(uname -s)" == Linux ]] || { echo "p0.sh runs on the Linux lab host (./run.sh perf --scenario P0)" >&2; exit 2; }
if [[ -z "$HOST" ]]; then
  if [[ "$(hostname)" == lima-* ]]; then HOST=dev-vm; else HOST="$(hostname -s)"; fi
fi
case "$RUNTIMES" in
  both) RUNTIMES="runsc runc" ;;
  runsc|runc) ;;
  *) echo "--runtime must be runsc, runc or both" >&2; exit 2 ;;
esac
if [[ "$RUNTIMES" == *runc* ]] && ! command -v runc >/dev/null; then
  echo "runc not installed (prod host?); running runsc only" >&2
  RUNTIMES=runsc
fi

say() { printf '==> %s\n' "$*" >&2; }
say "building specrun"
(cd "$LABD" && go build -o bin/specrun ./cmd/specrun)
if ! sudo -n true 2>/dev/null; then
  say "specrun needs root (containerd FIFOs and snapshots); authenticating sudo once"
  sudo -v
fi
sudo ctr -n labs images ls -q | grep -qx "$IMAGE" || { echo "image $IMAGE not in containerd; run images/perf/build.sh" >&2; exit 2; }

# Line numbers of the jump markers in perf.c.
JUMP_FROM="$(grep -n 'P0-JUMP-FROM' "$SRC" | cut -d: -f1)"
JUMP_TO="$(grep -n 'P0-JUMP-TO' "$SRC" | cut -d: -f1)"

ROWS=()
RT=""
# row CHECK CATEGORY STATUS DETAIL [FALLBACK]
row() {
  ROWS+=("$(jq -cn --arg check "$1" --arg cat "$2" --arg rt "$RT" --arg st "$3" --arg d "$4" --arg fb "${5:-}" \
    '{check:$check, category:$cat, runtime:$rt, status:$st, detail:$d, fallback:$fb}')")
  printf '  %-8s %-6s %-26s %s\n' "$3" "$RT" "$1" "$4" >&2
}

STATS_FILE="$(mktemp)"
trap 'rm -f "$STATS_FILE"' EXIT
# inbox TIMEOUT_S ARGS... — run in a fresh container, print PTY output with CR, NUL, the
# echoed "^@" and ANSI colour codes stripped. specrun's stats line lands in $STATS_FILE.
inbox() {
  local t="$1"; shift
  sudo "$SPECRUN" --spec "$SPEC" --image "$IMAGE" --runtime "$RT" --timeout "${t}s" \
    --id "p0-$RT-$$-$RANDOM" -- "$@" 2>"$STATS_FILE" \
    | tr -d '\r\000' | sed -E 's/\^@//g; s/\x1b\[[0-9;]*[A-Za-z]//g' || true
}
# stat JQ_EXPR — a field from the last container's specrun-stats line.
stat() { sed -n 's/^specrun-stats: //p' "$STATS_FILE" | tail -n1 | jq -r "$1 // empty"; }
g() { inbox 60 gdb -batch "$@" /opt/perf/perf; }            # gdb on perf
has() { grep -qE -- "$1" <<<"$2"; }
first() { grep -m1 -E -- "$1" <<<"$2" | cut -c1-160 || true; } # first matching line, for details
why() { # short reason for a failed gdb check; the ASLR warning is reported by its own row
  local o r
  o="$(grep -v '^warning:' <<<"$1")" # gdb warnings are reported by their own rows
  r="$(first 'SIGSEGV|SIGKILL|Cannot|Could not|Unable|No symbol|not found|Error|error' "$o")"
  echo "${r:-no expected output (last line: $(tail -n1 <<<"$o" | cut -c1-100))}"
}

check_gdb() {
  local o
  o="$(g -ex 'break sum_scores' -ex run -ex bt)"
  if has 'Breakpoint 1, sum_scores' "$o" && has '#1 .* main' "$o"; then row breakpoint gdb PASS "stops in sum_scores; bt shows main"
  else row breakpoint gdb FAIL "$(why "$o")"; fi

  o="$(g -ex 'break sum_scores' -ex run -ex next -ex next -ex step -ex finish)"
  if has 'Value returned' "$o"; then row next-step-finish gdb PASS "finish reports the return value"
  else row next-step-finish gdb FAIL "$(why "$o")"; fi

  o="$(g -ex 'break sum_scores' -ex run -ex 'display n' -ex 'x/5dw scores' -ex 'print accounts[1]' -ex 'ptype struct account')"
  if has '10\s+20\s+30\s+40' "$o" && has 'name = "bob' "$o" && has 'unsigned int checksum' "$o"; then
    row print-display-x gdb PASS "x/5dw, print struct, ptype"
  else row print-display-x gdb FAIL "$(why "$o")"; fi

  o="$(g -ex 'break report' -ex run -ex 'set var key = 42' -ex continue)"
  if has 'report key=42' "$o"; then row set-var gdb PASS "set var changed the argument"; else row set-var gdb FAIL "$(why "$o")"; fi

  o="$(g -ex 'break sum_scores' -ex run -ex 'return (int)777' -ex continue)"
  if has 'total=777' "$o"; then row return gdb PASS "forced return value reached main"; else row return gdb FAIL "$(why "$o")"; fi

  o="$(g -ex "break perf.c:$JUMP_FROM" -ex run -ex "jump perf.c:$JUMP_TO")"
  # Require the breakpoint hit first: after a crash, `jump` alone can still print jumped=1.
  if has "Breakpoint 1, main .* at perf.c:$JUMP_FROM|^$JUMP_FROM\s" "$o" && has 'jumped=1' "$o"; then row jump gdb PASS "skipped line $JUMP_FROM"
  else row jump gdb FAIL "$(why "$o")"; fi

  o="$(g -ex 'break main' -ex run -ex 'call report(7)')"
  if has 'report key=7' "$o"; then row call gdb PASS "call report(7)"; else row call gdb FAIL "$(why "$o")"; fi

  o="$(g -ex 'break step_loop' -ex run -ex 'set can-use-hw-watchpoints 0' -ex 'watch counter' -ex continue -ex continue)"
  if has '^Watchpoint [0-9]+: counter' "$o" && has 'Old value' "$o" && has 'New value' "$o"; then
    row sw-watchpoint gdb PASS "software watchpoint triggers with old/new values"
  else row sw-watchpoint gdb FAIL "$(why "$o")"; fi

  o="$(g -ex 'break step_loop' -ex run -ex 'set can-use-hw-watchpoints 1' -ex 'watch counter' -ex continue)"
  if has 'Hardware watchpoint [0-9]+: counter' "$o" && has 'New value' "$o"; then
    row hw-watchpoint gdb PASS "hardware watchpoints work on this host"
  elif has 'Hardware watchpoint [0-9]+: counter' "$o"; then
    # Worse than a refusal: gdb believes it is set, and the program runs past every write.
    row hw-watchpoint gdb FALLBACK "hardware watchpoint accepted but never triggered" "gdbinit sets can-use-hw-watchpoints 0 (software watchpoints)"
  else
    row hw-watchpoint gdb FALLBACK "$(why "$o")" "gdbinit sets can-use-hw-watchpoints 0 (software watchpoints)"
  fi

  o="$(g -ex 'break threads_ready' -ex run -ex 'info threads' -ex 'thread apply all bt')"
  local n
  n="$(grep -cE '^\*?\s+[0-9]+\s+(Thread|LWP|process)' <<<"$o" || true)"
  if [[ "$n" -ge 4 ]] && has 'worker' "$o"; then row threads gdb PASS "info threads shows $n threads; workers in bt"
  else row threads gdb FAIL "info threads shows ${n:-0} threads; $(why "$o")"; fi

  # Delete the breakpoint first: resuming with a signal re-executes the breakpointed insn.
  o="$(g -ex 'break step_loop' -ex run -ex delete -ex 'handle SIGUSR1 nostop noprint pass' -ex 'signal SIGUSR1')"
  if has 'usr1=1' "$o"; then row signal gdb PASS "SIGUSR1 delivered; handler ran"; else row signal gdb FAIL "$(why "$o")"; fi

  o="$(inbox 60 gdb -batch -ex bt /opt/perf/perf /opt/perf/perf.core)"
  if has 'SIGSEGV' "$o" && has 'crash' "$o"; then row core-file gdb PASS "core loads; bt shows crash()"
  else row core-file gdb FAIL "$(why "$o")"; fi

  # ASLR. Code and globals must be fixed (-no-pie); the stack is only fixed if gdb's
  # personality(ADDR_NO_RANDOMIZE) works under this runtime.
  local mains=() sps=() warn=""
  for _ in 1 2 3; do
    # shellcheck disable=SC2016  # $sp is a gdb register, not a shell variable
    o="$(g -ex 'break main' -ex run -ex 'printf "P0 main=%p sp=%p\n", &main, $sp')"
    mains+=("$(sed -n 's/^P0 main=\([^ ]*\) .*/\1/p' <<<"$o")")
    sps+=("$(sed -n 's/.* sp=\(.*\)$/\1/p' <<<"$o")")
    has 'Error disabling address space randomization' "$o" && warn=yes
  done
  if [[ -n "${mains[0]}" && "${mains[0]}" == "${mains[1]}" && "${mains[1]}" == "${mains[2]}" ]]; then
    row aslr-gdb-main gdb PASS "&main=${mains[0]} in 3 runs"
  else row aslr-gdb-main gdb FAIL "&main varies: ${mains[*]:-none}"; fi
  if [[ -n "$warn" ]]; then
    row disable-randomization gdb FALLBACK "gdb: personality(ADDR_NO_RANDOMIZE) fails (EINVAL)" \
      "binaries are -no-pie so code and globals are fixed; challenges must not depend on stack or heap addresses"
  else row disable-randomization gdb PASS "gdb disabled ASLR without warnings"; fi
  if [[ -n "${sps[0]}" && "${sps[0]}" == "${sps[1]}" && "${sps[1]}" == "${sps[2]}" ]]; then
    row aslr-gdb-stack gdb PASS "\$sp=${sps[0]} in 3 runs"
  else
    row aslr-gdb-stack gdb FALLBACK "\$sp varies: ${sps[*]:-none}" "challenges must not depend on stack addresses"
  fi

  local am=() as=()
  for _ in 1 2 3; do
    o="$(inbox 30 /opt/perf/perf addr)"
    am+=("$(sed -n 's/^addr main=\([^ ]*\) .*/\1/p' <<<"$o")")
    as+=("$(sed -n 's/.* stack=\([^ ]*\) .*/\1/p' <<<"$o")")
  done
  if [[ -n "${am[0]}" && "${am[0]}" == "${am[1]}" && "${am[1]}" == "${am[2]}" ]]; then
    row aslr-direct-main gdb PASS "&main=${am[0]} in 3 direct runs"
  else row aslr-direct-main gdb FAIL "&main varies: ${am[*]:-none}"; fi
  if [[ "${as[0]}" == "${as[1]}" && "${as[1]}" == "${as[2]}" ]]; then row aslr-direct-stack gdb INFO "stack fixed without gdb: ${as[0]}"
  else row aslr-direct-stack gdb INFO "stack randomized without gdb (expected): ${as[*]}"; fi

  o="$(g -ex 'shell wget http://1.1.1.1/' -ex 'shell nc 1.1.1.1 80' -ex 'python print(12345)')"
  if has 'wget: not found' "$o" && has 'nc: not found' "$o"; then row gdb-shell-tools gdb PASS "wget and nc not found from gdb's shell"
  else row gdb-shell-tools gdb FAIL "$(first 'wget|nc' "$o")"; fi
  if has '^12345$' "$o"; then row gdb-python gdb INFO "gdb's python works (expected until task 1.9 builds gdb without Python)"
  else row gdb-python gdb INFO "gdb has no working python"; fi

  o="$(inbox 120 gdb -batch -x /opt/perf/session.gdb /opt/perf/perf)"
  if has 'report key=42' "$o"; then row session-gdb gdb PASS "scripted session ran to the end"
  else row session-gdb gdb FAIL "$(why "$o")"; fi
}

kv() { sed -n "s/.*\b$1=\([^ ]*\).*/\1/p" <<<"$2" | head -n1; } # value of key=... in a probe line

check_sandbox() {
  local o
  o="$(inbox 30 /opt/perf/probe id)"
  if [[ "$(kv uid "$o")" == 1000 && "$(kv gid "$o")" == 1000 && "$(kv capeff "$o")" == 0000000000000000 && "$(kv nnp "$o")" == 1 ]]; then
    row user-caps sandbox PASS "uid/gid 1000, CapEff 0, no_new_privs"
  else row user-caps sandbox FAIL "$(first '^probe' "$o")"; fi

  o="$(inbox 30 /opt/perf/probe net)"
  if [[ -n "$(kv tcp "$o")" && "$(kv tcp "$o")" != OK && "$(kv udp "$o")" != OK ]]; then
    row no-network sandbox PASS "interfaces=$(kv interfaces "$o") tcp=$(kv tcp "$o") udp=$(kv udp "$o")"
  else row no-network sandbox FAIL "$(first '^probe' "$o")"; fi

  o="$(inbox 30 /opt/perf/probe rofs)"
  if [[ -n "$(kv root "$o")" && "$(kv root "$o")" != OK && "$(kv etc "$o")" != OK ]]; then
    row read-only-root sandbox PASS "create in /: $(kv root "$o"), in /etc: $(kv etc "$o")"
  else row read-only-root sandbox FAIL "$(first '^probe' "$o")"; fi

  o="$(inbox 30 /opt/perf/probe noexec)"
  if [[ "$(kv exec "$o")" == EACCES || "$(kv exec "$o")" == EPERM ]]; then row tmp-noexec sandbox PASS "exec from /tmp: $(kv exec "$o")"
  else row tmp-noexec sandbox FAIL "$(first '^probe' "$o")"; fi

  o="$(inbox 30 /opt/perf/probe fill)"
  local b; b="$(kv bytes "$o")"
  if [[ -n "$b" && "$b" -le 17825792 ]]; then row tmpfs-cap sandbox PASS "/tmp full at $((b / 1048576)) MiB ($(kv error "$o"))"
  else row tmpfs-cap sandbox FAIL "$(first '^probe' "$o")"; fi

  o="$(inbox 30 /bin/sh -c '/opt/perf/probe fork; echo P0-SURVIVED')"
  local f; f="$(kv forked "$o")"
  if [[ -n "$f" && "$f" -lt 32 ]] && has 'P0-SURVIVED' "$o"; then
    row pids-limit sandbox PASS "fork stopped after $f children ($(kv error "$o")); sandbox survived; host tasks peak $(stat .cgroup_stats.pids_peak)"
  else row pids-limit sandbox FAIL "forked=${f:-?}; survived=$(has P0-SURVIVED "$o" && echo yes || echo no); exit $(stat .exit_code); host tasks peak $(stat .cgroup_stats.pids_peak)"; fi

  # ADR 0009: under gVisor the lab's process limit is RLIMIT_NPROC=32; under runc it is the
  # cgroup (checked by pids-limit) and RLIMIT_NPROC is unset, because it would count host uid 1000.
  o="$(inbox 30 /opt/perf/probe rlimits)"
  local want_nproc=32; [[ "$RT" == runc ]] && want_nproc=inf
  if [[ "$(kv nofile "$o")" == 256 && "$(kv fsize "$o")" == 33554432 && "$(kv nproc "$o")" == "$want_nproc" && "$(kv core "$o")" == 0 ]]; then
    row rlimits sandbox PASS "nofile 256, fsize 32 MiB, nproc $want_nproc, core 0"
  else row rlimits sandbox FAIL "$(first '^probe' "$o") (want nproc=$want_nproc)"; fi

  o="$(inbox 60 /opt/perf/probe mem 200)"
  local last peak; last="$(grep -oE 'progress_mb=[0-9]+' <<<"$o" | tail -n1 | cut -d= -f2)"
  peak="$(( $(stat .cgroup_stats.memory_peak_bytes || echo 0) / 1048576 ))"
  if has 'allocated_mb=200 error=OK' "$o"; then row memory-limit sandbox FAIL "allocated 200 MiB under a 128 MiB limit"
  else row memory-limit sandbox PASS "killed after ${last:-0} MiB of 200; cgroup peak ${peak} MiB (limit 128 MiB)"; fi

  # Judge by host CPU from the cgroup. gVisor's in-sandbox CPU clock over-reports (Sentry view).
  o="$(inbox 30 /opt/perf/probe cpu 4)"
  local cores; cores="$(stat .cpu_cores_avg)"
  if [[ -n "$cores" ]] && awk -v c="$cores" 'BEGIN{exit !(c <= 0.6)}'; then
    row cpu-quota sandbox PASS "host CPU $(printf '%.2f' "$cores") cores for a busy loop (quota 0.5); in-sandbox view $(kv ratio "$o")"
  else row cpu-quota sandbox FAIL "host CPU ${cores:-?} cores for a busy loop with a 0.5 core quota"; fi

  o="$(inbox 4 /bin/sh -c 'read x')"
  row idle-footprint sandbox INFO "idle shell: cgroup memory peak $(( $(stat .cgroup_stats.memory_peak_bytes || echo 0) / 1048576 )) MiB, host tasks $(stat .cgroup_stats.pids_peak), create $(stat .create_ms) ms, start $(stat .start_ms) ms"

  o="$(inbox 30 /bin/sh -c 'ls /sys; echo; ls /sys/fs/cgroup 2>&1 | head -5')"
  row sysfs-view sandbox INFO "/sys shows: $(tr '\n' ' ' <<<"$o" | cut -c1-120)"
}

stamp="$(date -u +%Y-%m-%d)"
arch="$(uname -m)"
say "P0 on host '$HOST' ($arch, kernel $(uname -r)), runtimes: $RUNTIMES"
for RT in $RUNTIMES; do
  say "runtime $RT: gdb checks"
  check_gdb
  say "runtime $RT: sandbox checks"
  check_sandbox
done

mkdir -p "$OUT"
json="$OUT/p0-$stamp-$HOST.json"
md="$OUT/p0-$stamp-$HOST.md"
printf '%s\n' "${ROWS[@]}" | jq -s \
  --arg host "$HOST" --arg arch "$arch" --arg kernel "$(uname -r)" --arg date "$(date -u +%FT%TZ)" \
  --arg runsc "$(runsc --version 2>/dev/null | sed -n 1p)" --arg containerd "$(containerd --version 2>/dev/null | awk '{print $3}')" \
  --arg image "$IMAGE" \
  '{host:$host, arch:$arch, kernel:$kernel, date:$date, runsc:$runsc, containerd:$containerd, image:$image,
    summary: (group_by(.runtime) | map({runtime: .[0].runtime,
      pass: map(select(.status=="PASS"))|length, fallback: map(select(.status=="FALLBACK"))|length,
      fail: map(select(.status=="FAIL"))|length, info: map(select(.status=="INFO"))|length})),
    rows: .}' > "$json"

{
  echo "# P0 — $HOST, $stamp"
  echo
  echo "Host \`$HOST\`, $arch, kernel $(uname -r), $(runsc --version 2>/dev/null | sed -n 1p), containerd $(containerd --version 2>/dev/null | awk '{print $3}')."
  echo "Generated by \`labd/perf/p0/p0.sh\`; raw data in \`$(basename "$json")\`."
  echo
  jq -r '.summary[] | "- **\(.runtime)**: \(.pass) PASS, \(.fallback) FALLBACK, \(.fail) FAIL, \(.info) INFO"' "$json"
  echo
  echo "| Check | Category | Runtime | Status | Detail | Fallback |"
  echo "| --- | --- | --- | --- | --- | --- |"
  jq -r '.rows[] | "| \(.check) | \(.category) | \(.runtime) | \(.status) | \(.detail | gsub("\\|"; "\\\\|")) | \(.fallback) |"' "$json"
} > "$md"
say "wrote ${json#"$ROOT"/} and ${md#"$ROOT"/}"
jq -r '.summary[] | "    \(.runtime): \(.pass) pass, \(.fallback) fallback, \(.fail) FAIL, \(.info) info"' "$json" >&2
