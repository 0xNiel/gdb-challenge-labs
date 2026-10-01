"""Minute and hour rollups of `samples` and `events` (spec "Rollup"; Phase 7, task 7.4).

Minute buckets come from the raw rows; hour buckets from the minute rows. Every bucket is
written with an upsert on (bucket_ts, metric, dims_key), so running twice changes nothing and
re-rolling a window picks up late rows.

What is rolled up:

- every `samples` metric. Host and labd metrics (no session) get dims {}. Per-session metrics
  get two rows: dims {} over every lab, and {"challenge": slug} per challenge.
- `events`: one row per type, metric "events", dims {"type": t}, count = how many.
- from `lab_started` and `lab_ended`: "lab.start_latency_ms" and "lab.duration_s", over every
  lab and per challenge.

Percentiles are computed here, with percentile_cont's linear interpolation, so the same code
runs on Postgres and on the tests' SQLite. A minute holds a few thousand values at 100 labs.
"""

import json
import math
from collections import defaultdict
from datetime import datetime, timedelta

from django.utils import timezone

from labs.models import Event, Sample, Session

from .models import Rollup1h, Rollup1m

SAMPLE_FIELDS = ["count", "sum", "min", "max", "p50", "p95"]


def percentile(sorted_xs: list[float], p: float) -> float:
    """percentile_cont: linear interpolation between the closest ranks."""
    if not sorted_xs:
        return 0.0
    rank = p * (len(sorted_xs) - 1)
    lo, hi = math.floor(rank), math.ceil(rank)
    return sorted_xs[lo] + (sorted_xs[hi] - sorted_xs[lo]) * (rank - lo)


def stats(values: list[float]) -> dict:
    xs = sorted(values)
    return {
        "count": len(xs),
        "sum": math.fsum(xs),
        "min": xs[0],
        "max": xs[-1],
        "p50": percentile(xs, 0.5),
        "p95": percentile(xs, 0.95),
    }


def dims_key(dims: dict) -> str:
    return json.dumps(dims, sort_keys=True, separators=(",", ":"))


def floor_minute(t: datetime) -> datetime:
    return t.replace(second=0, microsecond=0)


def floor_hour(t: datetime) -> datetime:
    return t.replace(minute=0, second=0, microsecond=0)


def _upsert(model, groups: dict[tuple, dict]) -> int:
    rows = [
        model(bucket_ts=b, metric=m, dims=json.loads(k), dims_key=k, **vals)
        for (b, m, k), vals in groups.items()
    ]
    model.objects.bulk_create(
        rows,
        update_conflicts=True,
        unique_fields=["bucket_ts", "metric", "dims_key"],
        update_fields=["dims", *SAMPLE_FIELDS],
    )
    return len(rows)


def rollup_minutes(start: datetime, end: datetime) -> int:
    """Roll up the minutes from start's to end (exclusive). A partial last minute is written
    too, and completed by the next run. Returns the rows written."""
    start = floor_minute(start)
    values: dict[tuple, list[float]] = defaultdict(list)

    def add(bucket, metric, dims, v):
        values[(bucket, metric, dims_key(dims))].append(float(v))

    samples = list(
        Sample.objects.filter(ts__gte=start, ts__lt=end).values_list(
            "ts", "session_id", "metric", "value"
        )
    )
    slugs = dict(
        Session.objects.filter(id__in={s for _, s, _, _ in samples if s}).values_list(
            "id", "challenge_slug"
        )
    )
    for ts, sid, metric, value in samples:
        b = floor_minute(ts)
        add(b, metric, {}, value)
        if sid and sid in slugs:
            add(b, metric, {"challenge": slugs[sid]}, value)

    for e in Event.objects.filter(ts__gte=start, ts__lt=end).only(
        "ts", "type", "challenge_slug", "data"
    ):
        b = floor_minute(e.ts)
        add(b, "events", {"type": e.type}, 1)
        derived = {"lab_started": ("lab.start_latency_ms", "start_latency_ms"),
                   "lab_ended": ("lab.duration_s", "duration_s")}.get(e.type)  # fmt: skip
        if derived and isinstance((e.data or {}).get(derived[1]), int | float):
            v = e.data[derived[1]]
            if e.type == "lab_ended" and not v:
                continue  # never started (queue timeout, create failure): not a session length
            add(b, derived[0], {}, v)
            if e.challenge_slug:
                add(b, derived[0], {"challenge": e.challenge_slug}, v)

    return _upsert(Rollup1m, {k: stats(v) for k, v in values.items()})


def rollup_hours(start: datetime, end: datetime) -> int:
    """Roll up the hours from start's to end (exclusive) from the minute rows; a partial last
    hour is written too, and completed by later runs.

    count, sum, min and max are exact. p50 is the count-weighted mean of the minutes' p50s and
    p95 the largest minute p95: an hour's true percentiles need the raw values, which minute
    rows no longer hold; the p95 errs high, which is the safe side for capacity.
    """
    start = floor_hour(start)
    acc: dict[tuple, dict] = {}
    for r in Rollup1m.objects.filter(bucket_ts__gte=start, bucket_ts__lt=end).order_by("bucket_ts"):
        k = (floor_hour(r.bucket_ts), r.metric, r.dims_key)
        a = acc.get(k)
        if a is None:
            acc[k] = {"count": r.count, "sum": r.sum, "min": r.min, "max": r.max,
                      "p50w": r.p50 * r.count, "p95": r.p95}  # fmt: skip
            continue
        a["count"] += r.count
        a["sum"] += r.sum
        a["min"] = min(a["min"], r.min)
        a["max"] = max(a["max"], r.max)
        a["p50w"] += r.p50 * r.count
        a["p95"] = max(a["p95"], r.p95)
    groups = {
        k: {
            "count": a["count"],
            "sum": a["sum"],
            "min": a["min"],
            "max": a["max"],
            "p50": a["p50w"] / a["count"] if a["count"] else 0.0,
            "p95": a["p95"],
        }  # fmt: skip
        for k, a in acc.items()
    }
    return _upsert(Rollup1h, groups)


def default_window(span: timedelta, now: datetime | None = None) -> tuple[datetime, datetime]:
    """From `span` ago to now: each run re-rolls that window, so late rows are counted."""
    now = now or timezone.now()
    return now - span, now
