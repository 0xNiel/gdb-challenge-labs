#!/usr/bin/env bash
# disk.sh LABD_CFG — one disk sample as JSON on stdout, for labd-perf --disk-cmd (P2, P4, P9):
# containerd's snapshots (the writable layers of running labs, plus unpacked images), the whole
# containerd root, the systemd journal, and labd's tables (size and rows). Needs a sudo
# timestamp for du (scenario.sh keeps one alive); never prompts.
set -euo pipefail
cfg="${1:?usage: disk.sh LABD_CFG}"
dsn="$(sed -n 's/^postgres_dsn: *//p' "$cfg" | awk '{print $1}')"
mb() { sudo -n du -sm "$1" 2>/dev/null | awk '{print $1}' || echo null; }
snap="$(mb /var/lib/containerd/io.containerd.snapshotter.v1.overlayfs)"
root="$(mb /var/lib/containerd)"
journal="$(journalctl --disk-usage 2>/dev/null | grep -oE '[0-9.]+[KMGT]' | head -n1 | awk '
  /K$/{printf "%.1f", $0/1024; next} /M$/{printf "%.1f", $0+0; next} /G$/{printf "%.1f", $0*1024; next} {print "null"}')"
q() { psql "$dsn" -XAtq -c "$1" 2>/dev/null || echo null; }
tables="$(q "select json_object_agg(t, json_build_object('mb', round(pg_total_relation_size(t::regclass)/1048576.0, 2), 'rows', (xpath('/row/c/text()', query_to_xml('select count(*) as c from '||t, false, true, '')))[1]::text::bigint)) from unnest(array['sessions','events','samples']) t")"
jq -cn --argjson snap "${snap:-null}" --argjson root "${root:-null}" --argjson journal "${journal:-null}" \
  --argjson tables "${tables:-null}" \
  '{snapshots_mb:$snap, containerd_root_mb:$root, journal_mb:$journal, tables:$tables}'
