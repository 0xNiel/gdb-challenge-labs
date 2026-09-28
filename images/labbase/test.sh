#!/usr/bin/env bash
# SC2016: single quotes are deliberate; expansion happens in the container's shell.
# SC2015: `cond && pass || fail` is safe here because pass always succeeds.
# shellcheck disable=SC2016,SC2015
# images/labbase/test.sh — assert the CONTENTS of labbase (invariant S9 and the spec's
# keep-list). Runtime behaviour under the sandbox (network, read-only root, limits) is
# checked by labd/perf/p0/p0.sh. Runs the image with Docker on the developer machine.
#
# Exit 0 when every check passes. Image size over the 45 MB target is reported, not failed:
# the plan records it and moves the Python-free gdb build to task 1.9.
set -uo pipefail

IMG="${1:-gdblabs/labbase:dev}"
FAILED=0
pass() { printf '  PASS  %s\n' "$*"; }
fail() { printf '  FAIL  %s\n' "$*"; FAILED=1; }

docker image inspect "$IMG" >/dev/null 2>&1 || { echo "image $IMG not found; run images/labbase/build.sh" >&2; exit 1; }

# Run a command in the image as the lab user, no network, like the sandbox.
in_img() { docker run --rm --network none "$IMG" /bin/sh -c "$1" 2>&1; }

echo "==> labbase contents ($IMG)"

# S9: absent. `command -v` is an ash builtin, so this works without `which`.
for t in apk wget nc ftpget telnet httpd udhcpc ifconfig route ssl_client \
         gcc cc as ld ld.bfd ar python3 python pip curl sudo su; do
  if in_img "command -v $t" | grep -q .; then fail "$t is present"; else pass "$t absent"; fi
done

# Spec keep-list: present.
for t in sh ls cat less grep head tail wc hexdump strings file gdb objdump readelf nm; do
  if in_img "command -v $t" | grep -q .; then pass "$t present"; else fail "$t missing"; fi
done

# Nothing outside the allow-list on PATH.
extra="$(in_img 'for d in /bin /sbin /usr/bin /usr/sbin; do ls -1 $d 2>/dev/null; done' \
  | grep -vxE 'sh|busybox|ls|cat|less|grep|head|tail|wc|hexdump|strings|file|gdb|objdump|readelf|nm' || true)"
if [[ -z "$extra" ]]; then pass "no commands outside the allow-list"; else fail "unexpected commands: $(echo "$extra" | tr '\n' ' ')"; fi

[[ "$(in_img 'ls -d /etc/apk 2>/dev/null')" == "" ]] && pass "/etc/apk removed" || fail "/etc/apk still present"

# User and home.
status="$(in_img 'grep -E "^(Uid|Gid):" /proc/self/status')"
if grep -qE '^Uid:\s+1000\s' <<<"$status" && grep -qE '^Gid:\s+1000\s' <<<"$status"; then pass "runs as uid/gid 1000"; else fail "not uid/gid 1000: $status"; fi
[[ "$(in_img 'echo $HOME')" == "/home/lab" ]] && pass "HOME=/home/lab" || fail "HOME is not /home/lab"

# gdb picks up the lab settings through XDG_CONFIG_HOME (not ~/.gdbinit; /home/lab is tmpfs).
settings="$(in_img 'gdb -batch -ex "show disable-randomization" -ex "show can-use-hw-watchpoints" -ex "show pagination" -ex "show confirm"')"
grep -q 'Disabling randomization of debuggee.s virtual address space is on' <<<"$settings" && pass "gdb: disable-randomization on" || fail "gdb: disable-randomization not on: $settings"
grep -q 'willingness to use watchpoint hardware is 0' <<<"$settings" && pass "gdb: can-use-hw-watchpoints 0" || fail "gdb: hw watchpoints not 0"
grep -q 'State of pagination is off' <<<"$settings" && pass "gdb: pagination off" || fail "gdb: pagination not off"
grep -q 'Whether to confirm potentially dangerous operations is off' <<<"$settings" && pass "gdb: confirm off" || fail "gdb: confirm not off"
# Same result with an empty HOME, which is what the tmpfs gives at runtime.
if docker run --rm --network none --tmpfs /home/lab "$IMG" gdb -batch -ex "show disable-randomization" 2>&1 | grep -q ' is on'; then
  pass "gdb settings apply with an empty tmpfs home"
else
  fail "gdb settings lost with an empty tmpfs home"
fi

# Size (reported against the spec target; not a failure, see header).
size_mb="$(docker image inspect "$IMG" --format '{{.Size}}' | awk '{printf "%.1f", $1/1000000}')"
compressed_mb="$(docker save "$IMG" | gzip -c | wc -c | awk '{printf "%.1f", $1/1000000}')"
if awk -v s="$size_mb" 'BEGIN{exit !(s < 45)}'; then pass "size ${size_mb} MB < 45 MB"
else printf '  INFO  size %s MB uncompressed, %s MB gzipped; over the 45 MB target (task 1.9: gdb without Python)\n' "$size_mb" "$compressed_mb"; fi

echo
if [[ $FAILED -eq 0 ]]; then echo "==> labbase: all checks passed"; else echo "==> labbase: FAILED"; exit 1; fi
