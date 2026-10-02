"""Task 7.6: the four dashboards render from fixture rollups for staff, 403 for others; the SVG
helper writes valid XML."""

import uuid
import xml.etree.ElementTree as ET
from datetime import timedelta

import pytest
from django.contrib.auth import get_user_model
from django.utils import timezone

from analytics.dashboards import abandon_commands
from analytics.models import Rollup1h, Rollup1m
from analytics.svg import bar_chart, line_chart
from labs.models import Event, Session

PAGES = ["live", "usage", "learning", "capacity"]


def test_svg_is_valid_xml():
    now = timezone.now()
    charts = [
        line_chart(
            [
                ("a <b>", [(now - timedelta(minutes=i), i * 1.5) for i in range(10, 0, -1)]),
                ("c & d", [(now, 3)]),
            ],
            unit="MiB",
        ),  # fmt: skip
        line_chart([]),
        line_chart([("flat", [(now, 0)])]),
        bar_chart([("tier1-01 <solved>", 3), ("x", 0)], unit=" labs"),
        bar_chart([]),
    ]
    for svg in charts:
        root = ET.fromstring(str(svg))  # noqa: S314  our own output
        assert root.tag == "{http://www.w3.org/2000/svg}svg"
    assert "a &lt;b&gt;" in str(charts[0])  # labels are escaped


@pytest.fixture
def staff(client, db):
    u = get_user_model().objects.create_user("root", "root@example.com", "pw", is_staff=True)
    client.force_login(u)
    return u


SLUG = "tier1-01-off-by-one"


def rollup(model, t, metric, v, **kw):
    vals = {"count": 1, "sum": v, "min": v, "max": v, "p50": v, "p95": v, **kw}
    model.objects.create(bucket_ts=t, metric=metric, **vals)


def event(t, typ, sid=None, **data):
    Event.objects.create(ts=t, type=typ, user_id=7, session_id=sid, challenge_slug=SLUG, data=data)


@pytest.fixture
def fixture_data(imported):
    now = timezone.now()
    for i in range(5):
        t = (now - timedelta(minutes=i)).replace(second=0, microsecond=0)
        for metric, v in (("labd.active", 10 + i), ("labd.queued", i), ("session.rss_mb", 25 + i),
                          ("host.mem_used_mb", 5000), ("host.cpu_pct", 30)):  # fmt: skip
            rollup(Rollup1m, t, metric, v)
    hour = now.replace(minute=0, second=0, microsecond=0)
    rollup(Rollup1h, hour, "labd.active", 10, count=60, sum=600, min=5, max=42, p95=40)
    sid = uuid.uuid4()
    began, ended = now - timedelta(minutes=30), now - timedelta(minutes=10)
    Session.objects.create(id=sid, user_id=7, challenge_slug=SLUG, state="ended", created_at=began,
                           started_at=began, ended_at=ended, end_reason="idle_timeout")  # fmt: skip
    event(began, "lab_started", sid, start_latency_ms=1500)
    for i, line in enumerate(["break main", "run", "print total", "run"]):
        event(now - timedelta(minutes=13, seconds=-i), "command_entered", sid, seq=i + 1, line=line)
    event(ended, "lab_ended", sid, reason="idle_timeout", duration_s=1200, commands=4)
    return began, ended


@pytest.mark.parametrize("page", PAGES)
def test_staff_sees_each_dashboard(client, staff, fixture_data, fake_labd, page):
    resp = client.get(f"/admin/analytics/{page}")
    assert resp.status_code == 200
    html = resp.content.decode()
    assert '<meta http-equiv="refresh" content="30">' in html
    assert "<svg" in html


def test_dashboard_numbers(client, staff, fixture_data, fake_labd):
    # Starts count on the day they began, lengths on the day they ended: in the first half hour
    # of a UTC day the fixture's session spans two rows.
    began, ended = fixture_data
    rows = {r["day"]: r for r in client.get("/admin/analytics/usage").context["rows"]}
    start_day = rows[began.replace(hour=0, minute=0, second=0, microsecond=0)]
    end_day = rows[ended.replace(hour=0, minute=0, second=0, microsecond=0)]
    assert (start_day["labs"], start_day["users"], start_day["start_p50"]) == (1, 1, 1500)
    assert end_day["median_min"] == 20.0
    learning = client.get("/admin/analytics/learning").context
    lab1 = next(r for r in learning["rows"] if r["challenge"].slug == "tier1-01-off-by-one")
    assert lab1["started"] == 1 and lab1["solved"] == 0
    assert dict(learning["abandon"]) == {"run": 2, "break": 1, "print": 1}
    cap = client.get("/admin/analytics/capacity").context
    assert cap["peak_today"] == 42


def test_abandon_skips_solved_sessions(db, fixture_data):
    since = timezone.now() - timedelta(days=1)
    assert abandon_commands(since)
    event(timezone.now() - timedelta(minutes=12), "challenge_solved")
    assert abandon_commands(since) == []


@pytest.mark.parametrize("page", PAGES)
def test_non_staff_403_and_anonymous_to_login(client, db, page):
    assert client.get(f"/admin/analytics/{page}")["Location"].startswith("/login")
    client.force_login(get_user_model().objects.create_user("ada", "ada@example.com", "pw"))
    assert client.get(f"/admin/analytics/{page}").status_code == 403
