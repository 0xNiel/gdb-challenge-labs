"""Raw retention (spec "Two kinds of data"; Phase 7, task 7.5): samples 7 days, events 90 days,
rollups_1m 90 days; rollups_1h forever."""

from datetime import timedelta

from django.db import connection
from django.utils import timezone

from .models import Rollup1m

KEEP = {
    "samples": timedelta(days=7),
    "events": timedelta(days=90),
    "rollups_1m": timedelta(days=90),
}


def apply(now=None) -> dict[str, int]:
    """Delete rows older than their window; returns rows deleted per table."""
    now = now or timezone.now()
    out = {}
    with connection.cursor() as c:
        # labd's tables, by time range in SQL: samples has no primary key (labs.models.Sample).
        for table in ("samples", "events"):
            c.execute(f"DELETE FROM {table} WHERE ts < %s", [now - KEEP[table]])  # noqa: S608  fixed names
            out[table] = c.rowcount
    out["rollups_1m"], _ = Rollup1m.objects.filter(bucket_ts__lt=now - KEEP["rollups_1m"]).delete()
    return out
