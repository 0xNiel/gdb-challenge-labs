"""The unmanaged models must match labd's tables column for column (ADR 0003)."""

import re
from pathlib import Path

import pytest

from labs.models import Event, Sample, Session, record_event

MIGRATIONS = Path(__file__).resolve().parents[3] / "labd" / "internal" / "store" / "migrations"


def labd_columns(table: str) -> set[str]:
    """Column names of `table` across labd's SQL migrations (CREATE TABLE and ADD COLUMN)."""
    cols: set[str] = set()
    for f in sorted(MIGRATIONS.glob("*.sql")):
        sql = f.read_text()
        m = re.search(rf"CREATE TABLE {table} \((.*?)\n\);", sql, re.S)
        if m:
            for line in m.group(1).splitlines():
                word = line.strip().split(" ", 1)[0]
                if word and word.isidentifier() and word.upper() not in ("PRIMARY", "CHECK"):
                    cols.add(word)
        cols |= set(re.findall(rf"ALTER TABLE {table} ADD COLUMN (?:IF NOT EXISTS )?(\w+)", sql))
    return cols


@pytest.mark.parametrize("model", [Session, Event, Sample])
def test_unmanaged_model_matches_labd(model):
    want = labd_columns(model._meta.db_table)
    assert want, f"no CREATE TABLE {model._meta.db_table} in {MIGRATIONS}"
    got = {f.column for f in model._meta.concrete_fields}
    assert got == want


@pytest.mark.django_db
def test_query_every_column():
    record_event("lab_requested", user_id=1, challenge_slug="tier1-01-off-by-one", n=1)
    e = Event.objects.values(*[f.attname for f in Event._meta.concrete_fields]).get()
    assert e["data"] == {"n": 1}
    assert list(Session.objects.values(*[f.attname for f in Session._meta.concrete_fields])) == []
