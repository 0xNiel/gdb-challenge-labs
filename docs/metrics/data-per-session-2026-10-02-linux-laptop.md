# Data per session-minute — linux-laptop, 2026-10-02

From `scripts/live-run.sh`: 20 labs (P2 readers) for 5 minutes against the web stack, labd's sampler every 10 s. `pg_total_relation_size` (table, indexes, TOAST) before and after.

| Table | Growth | Per session-minute | Spec estimate |
| --- | --- | --- | --- |
| events | 147456 B | 1474 B | 2–4 KB for both |
| samples | 532480 B | 5324 B | |
