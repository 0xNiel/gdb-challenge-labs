"""Task 6.8: flag submission, rate limit, unlocks, events."""

import uuid
from datetime import timedelta

import pytest
from django.conf import settings
from django.contrib.auth import get_user_model
from django.utils import timezone

from curriculum.models import Challenge, Tier
from labs.models import Event, Session, record_event
from progress.flag import derive
from progress.models import FlagAttempt, Progress
from progress.services import is_unlocked

pytestmark = pytest.mark.django_db
SLUG = "tier1-01-off-by-one"


@pytest.fixture
def ada(imported):
    u = get_user_model().objects.create_user("ada", "ada@example.com", "pw")
    Progress.objects.create(user=u, challenge=imported[0])
    return u


def submit(client, value, slug=SLUG, partial=False):
    headers = {"X-Partial": "1"} if partial else {}
    return client.post(f"/lab/{slug}/flag", {"flag": value}, headers=headers)


def test_correct_flag_solves_and_unlocks_next(client, ada, imported):
    t0 = timezone.now() - timedelta(minutes=7)
    Session.objects.create(
        id=uuid.uuid4(), user_id=ada.id, challenge_slug=SLUG, state="ended", created_at=t0
    )
    started = record_event("lab_started", user_id=ada.id, challenge_slug=SLUG)
    Event.objects.filter(id=started.id).update(ts=t0)
    client.force_login(ada)
    resp = submit(client, f"  {derive(settings.DEPLOY_SECRET, SLUG)}\n")
    assert resp.status_code == 302
    p = Progress.objects.get(user=ada, challenge=imported[0])
    assert p.state == "solved" and p.solved_at and p.attempts == 1
    assert is_unlocked(ada, imported[1])
    assert Progress.objects.get(user=ada, challenge=imported[1]).state == "unlocked"
    sub = Event.objects.get(type="flag_submitted")
    assert sub.data == {"correct": True, "attempt_no": 1}
    solved = Event.objects.get(type="challenge_solved")
    assert solved.challenge_slug == SLUG
    assert 7 * 60 - 5 <= solved.data["time_to_solve_s"] <= 7 * 60 + 5
    assert solved.data["hints_used"] == 0 and solved.data["sessions_used"] == 1


def test_wrong_flag_records_attempt_only(client, ada, imported):
    client.force_login(ada)
    resp = submit(client, "LAB{NOPE}", partial=True)
    assert resp.status_code == 200
    assert b"That is not the flag" in resp.content
    assert FlagAttempt.objects.filter(user=ada, correct=False).count() == 1
    p = Progress.objects.get(user=ada, challenge=imported[0])
    assert p.state == "unlocked" and p.attempts == 1
    assert not is_unlocked(ada, imported[1])
    assert Event.objects.get(type="flag_submitted").data == {"correct": False, "attempt_no": 1}
    assert not Event.objects.filter(type="challenge_solved").exists()


def test_eleventh_attempt_in_ten_minutes_is_429(client, ada):
    client.force_login(ada)
    for _ in range(10):
        assert submit(client, "LAB{NOPE}", partial=True).status_code == 200
    resp = submit(client, derive(settings.DEPLOY_SECRET, SLUG), partial=True)
    assert resp.status_code == 429
    assert FlagAttempt.objects.count() == 10  # the 11th is not recorded or checked
    # Attempts older than the window no longer count.
    FlagAttempt.objects.update(ts=timezone.now() - timedelta(minutes=11))
    assert submit(client, derive(settings.DEPLOY_SECRET, SLUG), partial=True).status_code == 200


def test_locked_challenge_flag_is_403(client, ada):
    client.force_login(ada)
    slug = "tier1-02-null-deref"
    assert submit(client, derive(settings.DEPLOY_SECRET, slug), slug=slug).status_code == 403


def test_resubmitting_after_solve_changes_nothing(client, ada, imported):
    client.force_login(ada)
    good = derive(settings.DEPLOY_SECRET, SLUG)
    submit(client, good)
    resp = submit(client, good, partial=True)
    assert b"already solved" in resp.content
    assert FlagAttempt.objects.count() == 1
    assert Event.objects.filter(type="challenge_solved").count() == 1


def test_last_in_tier_unlocks_next_tiers_first(client, ada, imported):
    t2 = Tier.objects.create(slug="tier2-optimized-c", number=2, title="Optimized", order=2)
    nxt = Challenge.objects.create(
        tier=t2,
        slug="tier2-01-inlined",
        title="Inlined",
        difficulty=2,
        order=1,
        estimated_minutes=20,
    )
    for c in imported[:-1]:
        Progress.objects.update_or_create(user=ada, challenge=c, defaults={"state": "solved"})
    boss = imported[-1]
    assert is_unlocked(ada, boss) and not is_unlocked(ada, nxt)
    client.force_login(ada)
    submit(client, derive(settings.DEPLOY_SECRET, boss.slug), slug=boss.slug)
    assert is_unlocked(ada, nxt)
    assert Progress.objects.get(user=ada, challenge=nxt).state == "unlocked"
