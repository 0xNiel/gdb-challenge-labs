# 0003 — labd owns `sessions`, `events`, `samples` with embedded SQL migrations; Django maps them as unmanaged models

Date: 2026-09-26 · Status: accepted · Phase: 2

## Context

The spec gives one writer per table: `web` owns users, curriculum and progress; `labd` owns sessions, events and samples (with `web` also appending to `events`). Both need the schema. Two migration systems touching the same tables would race on deploy and blur ownership.

## Decision

- `labd/migrations/NNNN_*.sql` are embedded in the `labd` binary and applied by `labd migrate` (and on `labd serve` boot, idempotently, using a `labd_schema_migrations` table). They create `sessions`, `events`, `samples`, `rollups_1m`, `rollups_1h` and their indexes.
- Django declares those tables as models with `class Meta: managed = False; db_table = "..."` so the ORM can read them and `web` can insert into `events`. Django migrations never create, alter or drop them; `makemigrations --check` must stay clean with these models present.
- Django owns everything else through normal migrations.
- Deploy order: `labd migrate` before `manage.py migrate` before restarting services.
- Roles: `labd` has full rights on its tables and `SELECT` on the user table; `web` has full rights on its tables, `INSERT/SELECT` on `events`, `SELECT` on `sessions` and `samples`, and full rights on `rollups_*` (it writes them).

## Consequences

- Clear ownership and no double migration. Schema changes to labd tables require a coordinated change to the unmanaged model, caught by tests in Phase 6 that query each column.
- Two migration tools to run on deploy, wrapped in `scripts/deploy.sh`.

## Amendment (2026-10-01, Phase 7)

- **`rollups_1m` and `rollups_1h` are web's tables**, created by Django migrations (`web/analytics`), not by labd. web is their only writer (`manage.py rollup`), and the spec's rule is one writer per table. labd never reads or writes them.
- **web may delete from `samples` and `events`** for raw retention (`manage.py retention`: 7 and 90 days). labd's migration `0002_web_retention.sql` grants `DELETE` on both to `web`. web still never updates them, and never touches `sessions`.
- `samples` has no primary key. Django's unmanaged `Sample` model names `ts` as its key only because Django needs one. web reads samples in bulk and deletes by time range in SQL.
