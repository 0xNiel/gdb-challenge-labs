#!/usr/bin/env bash
# single-lab.sh — Phase 1, task 1.6: the first measurements of one lab, runsc vs runc.
#
#   labd/perf/single-lab.sh [--runtime runsc|runc|both] [--host LABEL] [--out DIR]
#                           [--runs 10] [--hold 30]
#
# For each runtime:
#   start_to_prompt   container create + task start + time to gdb's first "(gdb) " prompt,
#                     over --runs fresh containers (gdb reads "quit" right after the prompt)
#   at_prompt         gdb loaded, program not started, held --hold seconds, sampled each second
#   at_breakpoint     program stopped at `break main`, held and sampled the same way
#                     (null where the program cannot reach main, e.g. gdb under gVisor on arm64)
#   session_gdb       wall time of `gdb -batch -x session.gdb` (3 runs)
# Memory is read from the host: the lab cgroup's memory.current (the whole sandbox) and the
# RSS of the runsc-sandbox (Sentry) and runsc-gofer processes for that container.
# Output: DIR/single-lab-<date>-<host>.json and .md.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LABD="$ROOT/labd"
SPECRUN="$LABD/bin/specrun"
SPEC="$LABD/sandbox/sandbox-base.json"
IMAGE="${P0_IMAGE:-docker.io/gdblabs/perf:dev}"

RUNTIMES="runsc runc" HOST="" OUT="$ROOT/docs/metrics" RUNS=10 HOLD=30
while [[ $# -gt 0 ]]; do
  case "$1" in
    --runtime) RUNTIMES="$2"; [[ "$2" == both ]] && RUNTIMES="runsc runc"; shift 2 ;;
    --host) HOST="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    --runs) RUNS="$2"; shift 2 ;;
    --hold) HOLD="$2"; shift 2 ;;
    *) echo "usage: single-lab.sh [--runtime runsc|runc|both] [--host L] [--out D] [--runs N] [--hold S]" >&2; exit 2 ;;
  esac
done
[[ "$(uname -s)" == Linux ]] || { echo "run on the Linux lab host (./run.sh perf --scenario single-lab)" >&2; exit 2; }
if [[ -z "$HOST" ]]; then if [[ "$(hostname)" == lima-* ]]; then HOST=dev-vm; else HOST="$(hostname -s)"; fi; fi
if [[ "$RUNTIMES" == *runc* ]] && ! command -v runc >/dev/null; then RUNTIMES=runsc; fi

say() { printf '==> %s\n' "$*" >&2; }
(cd "$LABD" && go build -o bin/specrun ./cmd/specrun)
sudo -n true 2>/dev/null || { say "specrun needs root; authenticating sudo once"; sudo -v; }

# pct P list... — nearest-rank percentile of numbers on stdin
pct() { sort -n | awk -v p="$1" '{a[NR]=$1} END{if(NR==0){print "null"; exit} i=int((p/100)*NR+0.999999); if(i<1)i=1; if(i>NR)i=NR; print a[i]}'; }
maxof() { sort -n | tail -n1; }
rss_mb() { # rss of the process whose cmdline contains PATTERN and ID, in MiB (0 if none)
  local pid; pid="$(pgrep -f -- "$1.*$2" | head -n1 || true)"
  [[ -n "$pid" ]] || { echo 0; return; }
  awk '/^VmRSS:/{printf "%.1f", $2/1024}' "/proc/$pid/status" 2>/dev/null || echo 0
}

# start_to_prompt RT — prints one JSON object
start_to_prompt() {
  local rt="$1" vals=() i st
  for ((i = 1; i <= RUNS; i++)); do
    st="$(printf 'quit\n' | sudo "$SPECRUN" --spec "$SPEC" --image "$IMAGE" --runtime "$rt" --stdin --quiet \
          --timeout 30s --mark '\(gdb\) ' --id "sl-p-$rt-$$-$i" -- gdb -q /opt/perf/perf 2>&1 >/dev/null \
          | sed -n 's/^specrun-stats: //p' | tail -n1)"
    if [[ "$(jq -r .mark_found <<<"$st")" == true ]]; then
      vals+=("$(jq -r '.create_ms + .start_ms + .mark_ms' <<<"$st")")
    fi
  done
  local n=${#vals[@]}
  if [[ $n -eq 0 ]]; then echo '{"runs":0,"p50":null,"p95":null,"max":null}'; return; fi
  printf '%s\n' "${vals[@]}" > /tmp/sl-vals.$$
  jq -n --argjson n "$n" --argjson p50 "$(pct 50 </tmp/sl-vals.$$)" --argjson p95 "$(pct 95 </tmp/sl-vals.$$)" \
    --argjson max "$(maxof </tmp/sl-vals.$$)" '{runs:$n, p50:$p50, p95:$p95, max:$max}'
  rm -f /tmp/sl-vals.$$
}

# hold RT NAME GDB_ARGS... — run gdb interactively for HOLD seconds and sample memory
hold() {
  local rt="$1" name="$2"; shift 2
  local id="sl-h-$name-$rt-$$" cg="/sys/fs/cgroup/labs/sl-h-$name-$rt-$$"
  # shellcheck disable=SC2024  # the output file is ours, written as the invoking user on purpose
  sudo "$SPECRUN" --spec "$SPEC" --image "$IMAGE" --runtime "$rt" --timeout "$((HOLD + 5))s" \
    --id "$id" -- gdb -q "$@" /opt/perf/perf >/tmp/sl-hold.$$ 2>&1 &
  local bg=$! i cgv=() sen=() gof=()
  sleep 3 # let gdb load (and the program reach its breakpoint)
  for ((i = 0; i < HOLD; i++)); do
    # The cgroup disappears when the container exits at the end of the hold; skip that sample.
    local m; m="$(sudo awk '{printf "%.1f", $1/1048576}' "$cg/memory.current" 2>/dev/null || true)"
    if [[ -n "$m" ]]; then
      cgv+=("$m")
      if [[ "$rt" == runsc ]]; then sen+=("$(rss_mb runsc-sandbox "$id")"); gof+=("$(rss_mb runsc-gofer "$id")"); fi
    fi
    sleep 1
  done
  wait "$bg" || true
  local ok=true
  # Interactive gdb colours its output; strip ANSI codes before matching.
  if [[ "$name" == at_breakpoint ]] && ! tr -d '\r' </tmp/sl-hold.$$ | sed -E 's/\x1b\[[0-9;?]*[A-Za-z]//g' | grep -q 'Breakpoint 1, main'; then
    ok=false
  fi
  rm -f /tmp/sl-hold.$$
  if [[ ${#cgv[@]} -eq 0 || "$ok" == false ]]; then echo null; return; fi
  local s='{"samples":'"${#cgv[@]}"
  s+=',"cgroup_mem_mb":{"p50":'"$(printf '%s\n' "${cgv[@]}" | pct 50)"',"p95":'"$(printf '%s\n' "${cgv[@]}" | pct 95)"',"max":'"$(printf '%s\n' "${cgv[@]}" | maxof)"'}'
  if [[ "$rt" == runsc && ${#sen[@]} -gt 0 ]]; then
    s+=',"sentry_rss_mb":{"p50":'"$(printf '%s\n' "${sen[@]}" | pct 50)"',"p95":'"$(printf '%s\n' "${sen[@]}" | pct 95)"',"max":'"$(printf '%s\n' "${sen[@]}" | maxof)"'}'
    s+=',"gofer_rss_mb":{"p50":'"$(printf '%s\n' "${gof[@]}" | pct 50)"',"max":'"$(printf '%s\n' "${gof[@]}" | maxof)"'}'
  fi
  echo "$s}"
}

session_gdb() {
  local rt="$1" vals=() i st
  for i in 1 2 3; do
    st="$(sudo "$SPECRUN" --spec "$SPEC" --image "$IMAGE" --runtime "$rt" --quiet --timeout 120s \
          --id "sl-s-$rt-$$-$i" -- gdb -batch -x /opt/perf/session.gdb /opt/perf/perf 2>&1 >/dev/null \
          | sed -n 's/^specrun-stats: //p' | tail -n1)"
    [[ "$(jq -r .exit_code <<<"$st")" == 0 ]] && vals+=("$(jq -r .run_ms <<<"$st")")
  done
  if [[ ${#vals[@]} -eq 0 ]]; then echo null; return; fi
  echo "{\"runs\":${#vals[@]},\"wall_ms_p50\":$(printf '%s\n' "${vals[@]}" | pct 50)}"
}

image_mb="$(sudo ctr -n labs images ls "name==$IMAGE" | awk 'NR==2{print $4, $5}')"
results=()
for rt in $RUNTIMES; do
  say "$rt: start to prompt ($RUNS runs)"; sp="$(start_to_prompt "$rt")"
  say "$rt: gdb at prompt, ${HOLD}s";     ap="$(hold "$rt" at_prompt)"
  say "$rt: at breakpoint, ${HOLD}s";     ab="$(hold "$rt" at_breakpoint -ex 'break main' -ex run)"
  say "$rt: session.gdb (3 runs)";        sg="$(session_gdb "$rt")"
  results+=("$(jq -cn --arg rt "$rt" --argjson sp "$sp" --argjson ap "$ap" --argjson ab "$ab" --argjson sg "$sg" \
    '{runtime:$rt, start_to_prompt_ms:$sp, at_prompt:$ap, at_breakpoint:$ab, session_gdb:$sg}')")
done

stamp="$(date -u +%Y-%m-%d)"
mkdir -p "$OUT"
json="$OUT/single-lab-$stamp-$HOST.json" md="$OUT/single-lab-$stamp-$HOST.md"
printf '%s\n' "${results[@]}" | jq -s --arg host "$HOST" --arg arch "$(uname -m)" --arg kernel "$(uname -r)" \
  --arg date "$(date -u +%FT%TZ)" --arg image "$IMAGE" --arg image_size "$image_mb" \
  --arg runsc "$(runsc --version 2>/dev/null | sed -n 1p)" \
  '{host:$host, arch:$arch, kernel:$kernel, date:$date, runsc:$runsc, image:$image, image_size:$image_size, runtimes:.}' > "$json"

fmt() { jq -r "$1 // \"n/a\"" "$json"; }
{
  echo "# Single lab — $HOST, $stamp"
  echo
  echo "Host \`$HOST\` ($(uname -m), kernel $(uname -r)), $(runsc --version 2>/dev/null | sed -n 1p). Image \`$IMAGE\` ($image_mb). Generated by \`labd/perf/single-lab.sh\`; raw data in \`$(basename "$json")\`."
  echo
  echo "| Metric | $(jq -r '[.runtimes[].runtime] | join(" | ")' "$json") |"
  echo "| --- |$(jq -r '[.runtimes[] | " ---"] | join(" |")' "$json") |"
  for row in \
    "Start to gdb prompt p50 (ms)|.start_to_prompt_ms.p50" \
    "Start to gdb prompt p95 (ms)|.start_to_prompt_ms.p95" \
    "Memory, gdb at prompt, cgroup p95 (MiB)|.at_prompt.cgroup_mem_mb.p95" \
    "Memory, stopped at breakpoint, cgroup p95 (MiB)|.at_breakpoint.cgroup_mem_mb.p95" \
    "Sentry RSS at prompt p95 (MiB)|.at_prompt.sentry_rss_mb.p95" \
    "Sentry RSS at breakpoint p95 (MiB)|.at_breakpoint.sentry_rss_mb.p95" \
    "Gofer RSS at prompt max (MiB)|.at_prompt.gofer_rss_mb.max" \
    "session.gdb wall p50 (ms)|.session_gdb.wall_ms_p50"; do
    label="${row%%|*}" expr="${row#*|}"
    echo "| $label | $(jq -r "[.runtimes[] | ($expr // \"n/a\") | tostring] | join(\" | \")" "$json") |"
  done
  echo
  echo "n/a means the state could not be reached on this host (see the P0 report for why)."
  echo "The cgroup figure is the whole sandbox as the host sees it; under gVisor it includes the Sentry and gofer."
} > "$md"
say "wrote ${json#"$ROOT"/} and ${md#"$ROOT"/}"
cat "$md" >&2
