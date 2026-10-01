"""Tasks 7.4 and 7.5: rollups on hand-computed fixtures; retention windows."""

import uuid
from datetime import UTC, datetime, timedelta
from io import StringIO

import pytest
from django.core.management import call_command

from analytics.models import Rollup1h, Rollup1m
from analytics.retention import apply
from analytics.rollup import percentile, rollup_hours, rollup_minutes
from labs.models import Event, Sample, Session

pytestmark = pytest.mark.django_db
T = datetime(2026, 10, 1, 12, 0, tzinfo=UTC)
S1, S2 = uuid.uuid4(), uuid.uuid4()


def at(seconds: float) -> datetime:
    return T + timedelta(seconds=seconds)


@pytest.fixture
def data():
    for sid, slug in ((S1, "tier1-01-off-by-one"), (S2, "tier1-02-null-deref")):
        Session.objects.create(
            id=sid, user_id=1, challenge_slug=slug, state="running", created_at=T
        )
    cpu, rss = "host.cpu_pct", "session.rss_mb"
    rows = [
        (5, None, cpu, 10), (15, None, cpu, 20), (25, None, cpu, 60),
        (5, S1, rss, 20), (15, S1, rss, 22), (5, S2, rss, 30),
        (70, None, cpu, 40),  # the next minute
    ]  # fmt: skip
    Sample.objects.bulk_create(
        [Sample(ts=at(t), session_id=s, metric=m, value=v) for t, s, m, v in rows]
    )
    for latency in (1000, 2000):
        Event.objects.create(ts=at(30), type="lab_started", challenge_slug="tier1-01-off-by-one",
                             data={"start_latency_ms": latency})  # fmt: skip
    Event.objects.create(
        ts=at(40), type="lab_ended", data={"reason": "queue_timeout", "duration_s": 0}
    )
    Event.objects.create(ts=at(45), type="lab_ended", challenge_slug="tier1-01-off-by-one",
                         data={"reason": "user_stop", "duration_s": 300})  # fmt: skip


def row(model, bucket, metric, dims="{}"):
    r = model.objects.get(bucket_ts=bucket, metric=metric, dims_key=dims)
    return (r.count, r.sum, r.min, r.max, round(r.p50, 3), round(r.p95, 3))


def test_percentile_is_percentile_cont():
    assert percentile([10, 20, 60], 0.5) == 20
    assert percentile([10, 20, 60], 0.95) == pytest.approx(56)  # rank 1.9: 20 + 0.9 × 40
    assert percentile([5], 0.95) == 5
    assert percentile([], 0.5) == 0


def test_minute_buckets(data):
    rollup_minutes(T, T + timedelta(minutes=2))
    assert row(Rollup1m, T, "host.cpu_pct") == (3, 90, 10, 60, 20, 56)
    assert row(Rollup1m, T + timedelta(minutes=1), "host.cpu_pct") == (1, 40, 40, 40, 40, 40)
    # Every lab: 20, 22, 30. p95 at rank 1.9: 22 + 0.9 × 8 = 29.2.
    assert row(Rollup1m, T, "session.rss_mb") == (3, 72, 20, 30, 22, 29.2)
    assert row(Rollup1m, T, "session.rss_mb", '{"challenge":"tier1-01-off-by-one"}') == (
        2,
        42,
        20,
        22,
        21,
        21.9,
    )
    assert row(Rollup1m, T, "session.rss_mb", '{"challenge":"tier1-02-null-deref"}') == (
        1,
        30,
        30,
        30,
        30,
        30,
    )
    assert row(Rollup1m, T, "events", '{"type":"lab_started"}')[0] == 2
    assert row(Rollup1m, T, "events", '{"type":"lab_ended"}')[0] == 2
    assert row(Rollup1m, T, "lab.start_latency_ms") == (2, 3000, 1000, 2000, 1500, 1950)
    # A lab that never ran (duration 0) is not a session length.
    assert row(Rollup1m, T, "lab.duration_s") == (1, 300, 300, 300, 300, 300)


def test_running_twice_does_not_duplicate(data):
    rollup_minutes(T, T + timedelta(minutes=2))
    n = Rollup1m.objects.count()
    rollup_minutes(T, T + timedelta(minutes=2))
    assert Rollup1m.objects.count() == n
    # A late sample is picked up by the next run, in place.
    Sample.objects.create(ts=at(50), metric="host.cpu_pct", value=90)
    rollup_minutes(T, T + timedelta(minutes=2))
    assert Rollup1m.objects.count() == n
    assert row(Rollup1m, T, "host.cpu_pct")[:4] == (4, 180, 10, 90)


def test_hour_from_minutes(data):
    rollup_minutes(T, T + timedelta(minutes=2))
    rollup_hours(T, T + timedelta(hours=1))
    # count, sum, min, max exact; p50 weighted by count: (20 × 3 + 40 × 1) / 4 = 25; p95 the
    # largest minute p95.
    assert row(Rollup1h, T, "host.cpu_pct") == (4, 130, 10, 60, 25, 56)
    rollup_hours(T, T + timedelta(hours=1))
    assert Rollup1h.objects.filter(metric="host.cpu_pct").count() == 1


def test_command(data):
    out = StringIO()
    call_command("rollup", "--minute", "--hour", "--since", T.isoformat(),
                 "--until", (T + timedelta(hours=1)).isoformat(), stdout=out)  # fmt: skip
    assert "rollups_1m:" in out.getvalue() and "rollups_1h:" in out.getvalue()
    assert Rollup1m.objects.exists() and Rollup1h.objects.exists()


def test_retention():
    now = datetime(2026, 10, 1, tzinfo=UTC)
    for days in (8, 1):
        Sample.objects.create(ts=now - timedelta(days=days), metric="host.cpu_pct", value=1)
    for days in (91, 1):
        Event.objects.create(ts=now - timedelta(days=days), type="lab_requested")
        Rollup1m.objects.create(bucket_ts=now - timedelta(days=days), metric="m", count=1, sum=1,
                                min=1, max=1, p50=1, p95=1)  # fmt: skip
    Rollup1h.objects.create(bucket_ts=now - timedelta(days=400), metric="m", count=1, sum=1,
                            min=1, max=1, p50=1, p95=1)  # fmt: skip
    assert apply(now) == {"samples": 1, "events": 1, "rollups_1m": 1}
    assert Sample.objects.count() == 1 and Event.objects.count() == 1
    assert Rollup1m.objects.count() == 1
    assert Rollup1h.objects.count() == 1  # hourly rollups are kept forever
