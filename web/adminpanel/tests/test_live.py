"""Task 7.7: the admin live view with a fake labd: list, kill, drain, toggles, staff only."""

import pytest
from django.contrib.auth import get_user_model
from django.test import Client

from curriculum.models import Challenge
from labs.models import Session
from progress.models import Progress

pytestmark = pytest.mark.django_db
SLUG = "tier1-01-off-by-one"


@pytest.fixture
def learner_lab(imported, fake_labd):
    u = get_user_model().objects.create_user("ada", "ada@example.com", "pw")
    Progress.objects.create(user=u, challenge=imported[0])
    learner = Client()  # its own browser: the staff client stays logged in
    learner.force_login(u)
    learner.post(f"/lab/{SLUG}/start")
    return Session.objects.get(user_id=u.id)


@pytest.fixture
def staff(client, db):
    u = get_user_model().objects.create_user("root", "root@example.com", "pw", is_staff=True)
    client.force_login(u)
    return u


def test_list_renders_sessions_and_gauge(client, staff, learner_lab, fake_labd):
    resp = client.get("/admin/live")
    assert resp.status_code == 200
    html = resp.content.decode()
    assert "ada@example.com" in html and SLUG in html
    assert str(learner_lab.id)[:8] in html
    assert "1 / 100" in html  # the gauge: active of max_sessions
    assert f'action="/admin/live/kill/{learner_lab.id}"' in html


def test_kill_calls_labd_with_admin_kill(client, staff, learner_lab, fake_labd):
    resp = client.post(f"/admin/live/kill/{learner_lab.id}")
    assert resp.status_code == 302 and resp["Location"] == "/admin/live"
    assert fake_labd.calls[-1] == ("stop", str(learner_lab.id), "admin_kill")
    learner_lab.refresh_from_db()
    assert (learner_lab.state, learner_lab.end_reason) == ("ended", "admin_kill")


def test_drain_and_resume(client, staff, fake_labd):
    client.post("/admin/live/drain", {"drain": "1"})
    assert fake_labd.calls[-1] == ("drain", True)
    html = client.get("/admin/live").content.decode()
    assert "Draining:" in html and "Resume" in html and "0 / 0" in html
    client.post("/admin/live/drain", {"drain": "0"})
    assert fake_labd.calls[-1] == ("drain", False)
    assert "Draining:" not in client.get("/admin/live").content.decode()


def test_pending_pull_banner(client, staff, fake_labd):
    fake_labd.pending_pull = 2
    assert "2 challenges pending pull" in client.get("/admin/live").content.decode()


def test_toggle_challenge(client, staff, imported, fake_labd):
    client.post(f"/admin/live/challenge/{SLUG}/toggle")
    assert not Challenge.objects.get(slug=SLUG).enabled
    client.post(f"/admin/live/challenge/{SLUG}/toggle")
    assert Challenge.objects.get(slug=SLUG).enabled


def test_labd_down_is_shown(client, staff, fake_labd, monkeypatch):
    from labs.labd_client import LabdError

    def boom(self):
        raise LabdError("unavailable", "connection refused")

    monkeypatch.setattr(fake_labd, "stats", boom)
    resp = client.get("/admin/live")
    assert resp.status_code == 200 and b"labd is not reachable" in resp.content


@pytest.mark.parametrize(
    ("method", "url"),
    [("get", "/admin/live"), ("post", "/admin/live/drain"),
     ("post", f"/admin/live/challenge/{SLUG}/toggle"),
     ("post", "/admin/live/kill/00000000-0000-0000-0000-000000000001")],
)  # fmt: skip
def test_non_staff_403(client, imported, fake_labd, method, url):
    client.force_login(get_user_model().objects.create_user("bob", "bob@example.com", "pw"))
    assert getattr(client, method)(url).status_code == 403
    assert fake_labd.calls == []


def test_403_page_explains_staff_only(client, imported, fake_labd):
    client.force_login(get_user_model().objects.create_user("bob", "bob@example.com", "pw"))
    for url in ("/admin/live", "/admin/analytics/capacity"):
        resp = client.get(url)
        html = resp.content.decode()
        assert resp.status_code == 403
        assert "This page is for staff accounts" in html and "bob@example.com" in html
        assert "make_staff bob@example.com" in html
    # Other refusals keep their own reason.
    resp = client.get("/lab/tier1-02-null-deref")
    assert resp.status_code == 403 and b"This challenge is locked." in resp.content


def test_make_staff(db, capsys):
    from django.core.management import CommandError, call_command

    get_user_model().objects.create_user("bob", "bob@example.com", "pw")
    call_command("make_staff", "BOB@example.com")
    assert get_user_model().objects.get(username="bob").is_staff
    call_command("make_staff", "bob@example.com", "--revoke")
    assert not get_user_model().objects.get(username="bob").is_staff
    with pytest.raises(CommandError):
        call_command("make_staff", "nobody@example.com")
