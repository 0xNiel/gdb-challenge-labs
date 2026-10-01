"""ADR 0016: a correct flag stops the user's live lab for that challenge (reason solved)."""

import pytest
from django.conf import settings
from django.contrib.auth import get_user_model

from labs.models import Session
from progress.flag import derive
from progress.models import Progress

pytestmark = pytest.mark.django_db
SLUG = "tier1-01-off-by-one"


@pytest.fixture
def ada(client, imported, fake_labd):
    u = get_user_model().objects.create_user("ada", "ada@example.com", "pw")
    Progress.objects.create(user=u, challenge=imported[0])
    client.force_login(u)
    return u


def submit(client, value):
    return client.post(f"/lab/{SLUG}/flag", {"flag": value}, headers={"X-Partial": "1"})


def good():
    return derive(settings.DEPLOY_SECRET, SLUG)


def test_correct_flag_stops_the_lab(client, ada, fake_labd):
    client.post(f"/lab/{SLUG}/start")
    s = Session.objects.get(user_id=ada.id)
    resp = submit(client, good())
    assert b"Your lab has been stopped." in resp.content
    assert fake_labd.calls[-1] == ("stop", str(s.id), "solved")
    s.refresh_from_db()
    assert (s.state, s.end_reason) == ("ended", "solved")


def test_wrong_flag_leaves_it_running(client, ada, fake_labd):
    client.post(f"/lab/{SLUG}/start")
    submit(client, "LAB{NOPE}")
    assert [c[0] for c in fake_labd.calls] == ["start"]
    assert Session.objects.get(user_id=ada.id).state == "running"


def test_resubmission_after_solve_stops_nothing(client, ada, fake_labd):
    client.post(f"/lab/{SLUG}/start")
    submit(client, good())
    client.post(f"/lab/{SLUG}/start")  # look around again after solving
    submit(client, good())
    assert [c[0] for c in fake_labd.calls] == ["start", "stop", "start"]
    assert Session.objects.filter(user_id=ada.id, state="running").count() == 1


def test_solve_without_a_lab(client, ada, fake_labd):
    resp = submit(client, good())
    assert b"Correct!" in resp.content and b"stopped" not in resp.content
    assert fake_labd.calls == []


def test_solve_counts_when_stop_fails(client, ada, imported, fake_labd):
    client.post(f"/lab/{SLUG}/start")
    fake_labd.stop_fails = True
    resp = submit(client, good())
    assert resp.status_code == 200
    assert b"Correct!" in resp.content and b"stopped" not in resp.content
    assert Progress.objects.get(user=ada, challenge=imported[0]).state == "solved"
