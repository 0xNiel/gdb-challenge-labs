"""Task 7.3: each of web's five events is written by its action (spec "Event schema")."""

import pytest
from django.conf import settings
from django.contrib.auth import get_user_model

from labs.models import Event
from progress.flag import derive

pytestmark = pytest.mark.django_db
SLUG = "tier1-01-off-by-one"
PW = "correct-horse-battery-9"


def test_web_writes_its_five_events(client, imported, fake_labd):
    client.post("/signup", {"email": "ada@example.com", "password1": PW, "password2": PW})
    user = get_user_model().objects.get(email="ada@example.com")
    client.post("/accounts/logout/")
    client.post("/login", {"login": "ada@example.com", "password": PW})
    client.post(f"/lab/{SLUG}/hint", {"n": 1})
    client.post(f"/lab/{SLUG}/flag", {"flag": "LAB{NOPE}"})
    client.post(f"/lab/{SLUG}/flag", {"flag": derive(settings.DEPLOY_SECRET, SLUG)})

    evs = {}
    for e in Event.objects.filter(user_id=user.id).order_by("id"):
        evs.setdefault(e.type, []).append(e)
    want = {
        "user_signed_up": [],
        "user_logged_in": [],
        "hint_viewed": ["index", "cost"],
        "flag_submitted": ["correct", "attempt_no"],
        "challenge_solved": ["time_to_solve_s", "hints_used", "sessions_used"],
    }
    for typ, keys in want.items():
        assert typ in evs, f"no {typ} event"
        for k in keys:
            assert k in evs[typ][-1].data, f"{typ} lacks {k}: {evs[typ][-1].data}"
    assert [e.data["correct"] for e in evs["flag_submitted"]] == [False, True]
    assert evs["challenge_solved"][0].challenge_slug == SLUG
    assert evs["challenge_solved"][0].data["hints_used"] == 1
