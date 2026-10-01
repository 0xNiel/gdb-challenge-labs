"""What the four dashboards show (spec "Dashboards"; Phase 7, task 7.6).

Usage and Learning read raw `events` and `sessions` (kept 90 days; exact medians); Capacity
and the charts over time read the rollups, which outlive the raw samples (7 days).
"""

import statistics
from collections import Counter, defaultdict
from datetime import datetime, timedelta

from django.db.models import Avg, Count, Q
from django.utils import timezone

from curriculum.models import Challenge
from labs.labd_client import LabdError, get_client
from labs.models import Event, Session
from progress.models import Progress

from .models import Rollup1h, Rollup1m
from .svg import bar_chart, line_chart

DAYS = 30
ABANDON_REASONS = ("idle_timeout", "hard_ttl", "ws_closed", "user_stop")


def _day(t: datetime) -> datetime:
    return t.replace(hour=0, minute=0, second=0, microsecond=0)


def _nearest(sorted_xs: list[float], p: float) -> float:
    """Nearest-rank percentile of a sorted, non-empty list."""
    return sorted_xs[min(len(sorted_xs) - 1, round(p * (len(sorted_xs) - 1)))]


def _median_min(seconds: list[float]) -> float | None:
    return round(statistics.median(seconds) / 60, 1) if seconds else None


def _series(model, metric: str, field: str, since: datetime, dims: str = "{}"):
    rows = model.objects.filter(metric=metric, dims_key=dims, bucket_ts__gte=since).order_by(
        "bucket_ts"
    )
    return [(r.bucket_ts, getattr(r, field)) for r in rows]


def labd_stats() -> dict | None:
    try:
        return get_client().stats()
    except LabdError:
        return None


def live() -> dict:
    st = labd_stats()
    since = timezone.now() - timedelta(hours=2)
    gauge = None
    if st:
        cap = st.get("max_sessions") or 0
        gauge = {"active": st.get("active", 0), "queued": st.get("queued", 0), "max": cap,
                 "pct": round(100 * st.get("active", 0) / cap) if cap else 0}  # fmt: skip
    return {
        "stats": st,
        "gauge": gauge,
        "chart": line_chart(
            [
                ("active", _series(Rollup1m, "labd.active", "max", since)),
                ("queued", _series(Rollup1m, "labd.queued", "max", since)),
            ],
            unit="labs",
        ),
    }


def usage() -> dict:
    since = _day(timezone.now()) - timedelta(days=DAYS - 1)
    starts = Event.objects.filter(type="lab_started", ts__gte=since).values_list(
        "ts", "user_id", "data"
    )
    labs, users, latency = Counter(), defaultdict(set), defaultdict(list)
    for ts, uid, data in starts:
        d = _day(ts)
        labs[d] += 1
        users[d].add(uid)
        if isinstance((data or {}).get("start_latency_ms"), int | float):
            latency[d].append(data["start_latency_ms"])
    lengths = defaultdict(list)
    for s in Session.objects.filter(ended_at__gte=since, started_at__isnull=False):
        lengths[_day(s.ended_at)].append(s.duration_s / 60)
    days = sorted(set(labs) | set(lengths))
    rows = []
    for d in days:
        lat = sorted(latency[d])
        rows.append({
            "day": d, "labs": labs[d], "users": len(users[d]),
            "median_min": round(statistics.median(lengths[d]), 1) if lengths[d] else None,
            "start_p50": round(statistics.median(lat)) if lat else None,
            "start_p95": round(_nearest(lat, 0.95)) if lat else None,
        })  # fmt: skip
    return {
        "rows": rows,
        "labs_chart": line_chart(
            [
                ("labs started", [(r["day"], r["labs"]) for r in rows]),
                ("users", [(r["day"], r["users"]) for r in rows]),
            ]
        ),  # fmt: skip
        "latency_chart": line_chart(
            [
                ("p50", [(r["day"], r["start_p50"]) for r in rows if r["start_p50"] is not None]),
                ("p95", [(r["day"], r["start_p95"]) for r in rows if r["start_p95"] is not None]),
            ],
            unit="ms",
        ),  # fmt: skip
    }


def learning() -> dict:
    since = timezone.now() - timedelta(days=DAYS)
    started = dict(
        Event.objects.filter(type="lab_started", ts__gte=since)
        .values("challenge_slug").annotate(n=Count("user_id", distinct=True))
        .values_list("challenge_slug", "n")
    )  # fmt: skip
    solve_times = defaultdict(list)
    for slug, data in Event.objects.filter(type="challenge_solved", ts__gte=since).values_list(
        "challenge_slug", "data"
    ):
        if isinstance((data or {}).get("time_to_solve_s"), int | float):
            solve_times[slug].append(data["time_to_solve_s"])
    prog = {
        r["challenge__slug"]: r
        for r in Progress.objects.values("challenge__slug").annotate(
            solved=Count("id", filter=Q(state="solved")), hints=Avg("hints_used")
        )
    }
    rows, tiers = [], defaultdict(lambda: {"started": 0, "solved": 0})
    for c in Challenge.objects.select_related("tier").order_by("tier__order", "order"):
        p = prog.get(c.slug, {})
        r = {
            "challenge": c, "started": started.get(c.slug, 0), "solved": p.get("solved", 0),
            "hints": round(p["hints"], 1) if p.get("hints") is not None else None,
            "median_solve_min": _median_min(solve_times[c.slug]),
        }  # fmt: skip
        rows.append(r)
        tiers[c.tier]["started"] += r["started"]
        tiers[c.tier]["solved"] += r["solved"]
    return {
        "rows": rows,
        "tiers": [{"tier": t, **v} for t, v in tiers.items()],
        "funnel": bar_chart([(f"{r['challenge'].slug} solved", r["solved"]) for r in rows]),
        "abandon": abandon_commands(since),
    }


def abandon_commands(since: datetime, top: int = 10) -> list[tuple[str, int]]:
    """The commands typed in the last 5 minutes of labs that ended without a solve: the verbs
    learners were stuck on (spec "Learning" dashboard)."""
    solved = {
        (u, s): ts
        for u, s, ts in Event.objects.filter(type="challenge_solved").values_list(
            "user_id", "challenge_slug", "ts"
        )
    }  # fmt: skip
    counts = Counter()
    for e in Event.objects.filter(
        type="lab_ended", ts__gte=since, data__reason__in=ABANDON_REASONS
    ):
        t = solved.get((e.user_id, e.challenge_slug))
        if t is not None and t <= e.ts:
            continue  # solved before this session ended: not abandoned
        lines = Event.objects.filter(
            type="command_entered",
            session_id=e.session_id,
            ts__gte=e.ts - timedelta(minutes=5),
            ts__lte=e.ts,
        ).values_list("data", flat=True)
        for d in lines:
            verb = str((d or {}).get("line", "")).split(" ", 1)[0]
            if verb:
                counts[verb] += 1
    return counts.most_common(top)


def capacity() -> dict:
    now = timezone.now()
    day_ago, month_ago = now - timedelta(hours=24), now - timedelta(days=DAYS)
    peaks = defaultdict(float)
    for t, v in _series(Rollup1h, "labd.active", "max", month_ago):
        peaks[_day(t)] = max(peaks[_day(t)], v)
    return {
        "stats": labd_stats(),
        "rss_chart": line_chart(
            [
                ("p50", _series(Rollup1m, "session.rss_mb", "p50", day_ago)),
                ("p95", _series(Rollup1m, "session.rss_mb", "p95", day_ago)),
            ],
            unit="MiB per lab",
        ),  # fmt: skip
        "rss_30d_chart": line_chart(
            [("p95", _series(Rollup1h, "session.rss_mb", "p95", month_ago))], unit="MiB per lab"
        ),
        "peak_chart": bar_chart(
            [(d.strftime("%b %d"), v) for d, v in sorted(peaks.items())], unit=" labs"
        ),
        "host_chart": line_chart(
            [("memory used", _series(Rollup1m, "host.mem_used_mb", "max", day_ago))], unit="MB"
        ),
        "cpu_chart": line_chart(
            [("host CPU p95", _series(Rollup1m, "host.cpu_pct", "p95", day_ago))], unit="%"
        ),
        "peak_today": max((v for d, v in peaks.items() if d == _day(now)), default=0),
    }
