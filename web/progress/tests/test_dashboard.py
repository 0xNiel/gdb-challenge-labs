"""Task 6.10: the dashboard's totals from fixture data."""

import uuid
from datetime import date, timedelta

import pytest
from django.contrib.auth import get_user_model
from django.utils import timezone

from labs.models import Session
from progress.dashboard import streak
from progress.models import Progress

pytestmark = pytest.mark.django_db


def test_streak():
    today = date(2026, 10, 1)
    d = lambda n: today - timedelta(days=n)  # noqa: E731
    assert streak(set(), today) == 0
    assert streak({d(0), d(1), d(2), d(4)}, today) == 3
    assert streak({d(1), d(2)}, today) == 2  # nothing yet today: yesterday's streak holds
    assert streak({d(2)}, today) == 0


def test_dashboard_totals(client, imported):
    ada = get_user_model().objects.create_user("ada", "ada@example.com", "pw")
    other = get_user_model().objects.create_user("bob", "bob@example.com", "pw")
    now = timezone.now()
    Progress.objects.create(
        user=ada,
        challenge=imported[0],
        state="solved",
        solved_at=now - timedelta(days=1),
        hints_used=2,
    )
    Progress.objects.create(
        user=ada, challenge=imported[1], state="solved", solved_at=now, hints_used=1
    )
    Progress.objects.create(user=ada, challenge=imported[2], state="unlocked", hints_used=1)
    Progress.objects.create(
        user=other, challenge=imported[0], state="solved", solved_at=now, hints_used=3
    )

    def sess(user, start_min, length_min):
        start = now - timedelta(minutes=start_min)
        Session.objects.create(
            id=uuid.uuid4(),
            user_id=user.id,
            challenge_slug="x",
            state="ended",
            created_at=start,
            started_at=start,
            ended_at=start + timedelta(minutes=length_min),
        )

    sess(ada, 120, 20)
    sess(ada, 60, 25)
    sess(other, 60, 50)
    Session.objects.create(  # queued, never started: no time
        id=uuid.uuid4(), user_id=ada.id, challenge_slug="x", state="abandoned", created_at=now
    )

    client.force_login(ada)
    resp = client.get("/dashboard")
    assert resp.status_code == 200
    ctx = resp.context
    assert (ctx["solved"], ctx["total"]) == (2, 5)
    assert ctx["time_spent_s"] == 45 * 60
    assert ctx["hints_used"] == 4
    assert ctx["streak_days"] == 2
    assert [(t["solved"], t["total"], t["pct"]) for t in ctx["tiers"]] == [(2, 5, 40)]
    html = resp.content.decode()
    assert '<span class="num" id="stat-time">45</span>' in html
    assert '<span class="num" id="stat-streak">2</span>' in html
    assert 'aria-valuenow="40"' in html
