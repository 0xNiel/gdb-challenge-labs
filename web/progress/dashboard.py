"""The numbers on /dashboard (spec "Pages": progress, streaks, time spent, tier completion)."""

from datetime import date, timedelta
from itertools import groupby

from django.db.models import Sum
from django.utils import timezone

from labs.models import Session

from .models import Progress
from .services import SOLVED, states


def streak(solve_days: set[date], today: date) -> int:
    """Consecutive days with a solve, ending today, or yesterday if nothing is solved yet today."""
    day = today if today in solve_days else today - timedelta(days=1)
    n = 0
    while day in solve_days:
        n += 1
        day -= timedelta(days=1)
    return n


def summary(user) -> dict:
    items = states(user)
    tiers = []
    for tier, group in groupby(items, key=lambda it: it.challenge.tier):
        group = list(group)
        solved = sum(it.state == SOLVED for it in group)
        tiers.append(
            {"tier": tier, "solved": solved, "total": len(group),
             "pct": round(100 * solved / len(group)) if group else 0}
        )  # fmt: skip
    rows = Progress.objects.filter(user=user)
    solve_days = {
        timezone.localdate(p.solved_at) for p in rows.filter(state=SOLVED, solved_at__isnull=False)
    }
    seconds = sum(
        s.duration_s for s in Session.objects.filter(user_id=user.id, started_at__isnull=False)
    )
    return {
        "tiers": tiers,
        "solved": sum(t["solved"] for t in tiers),
        "total": sum(t["total"] for t in tiers),
        "time_spent_s": seconds,
        "hints_used": rows.aggregate(n=Sum("hints_used"))["n"] or 0,
        "streak_days": streak(solve_days, timezone.localdate()),
        "solve_days": len(solve_days),
        "recent": rows.filter(state=SOLVED).select_related("challenge").order_by("-solved_at")[:5],
    }
