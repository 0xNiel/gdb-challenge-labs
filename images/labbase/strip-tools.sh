#!/bin/sh
# strip-tools.sh — run as the LAST build step of labbase. Keeps an allow-list of commands and
# deletes every other executable or applet link on PATH, plus the package manager.
#
# Spec ("Lab images → Base image"): remove apk, wget and network applets; keep sh, ls, cat,
# less, grep, head, tail, wc, hexdump, strings, file, plus objdump, readelf, nm and gdb.
# An allow-list is stricter than a deny-list: new packages cannot slip tools in.
#
# Limitation (documented in labbase/README.md): /bin/busybox itself stays because it IS
# /bin/sh, so `busybox wget` still exists as a code path. The sandbox has no network
# interface (S2), which is the control that matters.
set -eu

# The loop deletes /bin/rm and friends, so call the kept busybox binary directly.
BB=/bin/busybox

KEEP="sh busybox ls cat less grep head tail wc hexdump strings file gdb objdump readelf nm"

keep() {
  for k in $KEEP; do [ "$1" = "$k" ] && return 0; done
  return 1
}

removed=0
for dir in /bin /sbin /usr/bin /usr/sbin /usr/local/bin /usr/local/sbin; do
  [ -d "$dir" ] || continue
  for f in "$dir"/* "$dir"/.[!.]*; do
    [ -e "$f" ] || [ -L "$f" ] || continue
    name="${f##*/}"
    if keep "$name"; then continue; fi
    $BB rm -f "$f"
    removed=$((removed + 1))
  done
done

# Package manager and its state.
$BB rm -rf /etc/apk /lib/apk /usr/share/apk /var/cache/apk /usr/lib/apk

echo "strip-tools: removed $removed entries; kept: $KEEP"
$BB rm -f "$0"
