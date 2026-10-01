"""Task 6.6: start, stop and token with a fake labd."""

import base64
import uuid

import pytest
from django.contrib.auth import get_user_model
from django.utils import timezone

from labs.models import Session
from progress.models import Progress

pytestmark = pytest.mark.django_db
SLUG = "tier1-01-off-by-one"


@pytest.fixture
def ada(imported):
    u = get_user_model().objects.create_user("ada", "ada@example.com", "pw")
    Progress.objects.create(user=u, challenge=imported[0])
    return u


@pytest.fixture
def bob(imported):
    u = get_user_model().objects.create_user("bob", "bob@example.com", "pw")
    Progress.objects.create(user=u, challenge=imported[0])
    return u


def start(client, slug=SLUG):
    return client.post(f"/lab/{slug}/start")


def test_challenge_page(client, ada, fake_labd):
    client.force_login(ada)
    resp = client.get(f"/lab/{SLUG}")
    assert resp.status_code == 200
    assert b"Start the lab" in resp.content
    assert b"total of 5 scores" in resp.content or b"scores" in resp.content  # README


def test_start_when_unlocked_redirects_to_session(client, ada, fake_labd):
    client.force_login(ada)
    resp = start(client)
    assert resp.status_code == 302 and resp["Location"] == f"/lab/{SLUG}/session"
    assert fake_labd.calls == [("start", ada.id, SLUG)]
    s = Session.objects.get(user_id=ada.id)
    resp = client.get(f"/lab/{SLUG}/session")
    assert resp.status_code == 200
    assert str(s.id).encode() in resp.content


def test_start_when_locked_is_403(client, ada, fake_labd):
    client.force_login(ada)
    assert start(client, "tier1-02-null-deref").status_code == 403
    assert fake_labd.calls == []


def test_start_needs_post_and_login(client, ada, fake_labd):
    assert start(client).status_code == 302  # to /login
    client.force_login(ada)
    assert client.get(f"/lab/{SLUG}/start").status_code == 405
    assert fake_labd.calls == []


def test_start_with_existing_session_returns_same(client, ada, fake_labd):
    client.force_login(ada)
    start(client)
    start(client)
    assert Session.objects.filter(user_id=ada.id).count() == 1


def test_start_while_another_challenge_runs_is_refused(client, ada, imported, fake_labd):
    Progress.objects.filter(user=ada).update(state="solved")
    client.force_login(ada)
    start(client)
    resp = start(client, "tier1-02-null-deref")
    assert resp.status_code == 302 and resp["Location"] == f"/lab/{SLUG}"
    assert [c[0] for c in fake_labd.calls] == ["start"]  # labd was not asked again


def test_queue_full_is_reported(client, ada, fake_labd):
    fake_labd.fail_with = "queue_full"
    client.force_login(ada)
    resp = start(client)
    assert resp.status_code == 302 and resp["Location"] == f"/lab/{SLUG}"
    msgs = [str(m) for m in resp.wsgi_request._messages]
    assert any("queue is full" in m for m in msgs)


def test_stop_calls_labd_with_user_stop(client, ada, fake_labd):
    client.force_login(ada)
    start(client)
    sid = str(Session.objects.get(user_id=ada.id).id)
    resp = client.post(f"/lab/{SLUG}/stop")
    assert resp.status_code == 302 and resp["Location"] == f"/lab/{SLUG}"
    assert fake_labd.calls[-1] == ("stop", sid, "user_stop")
    assert Session.objects.get(id=sid).state == "ended"
    # the terminal page is gone once the session ended
    assert client.get(f"/lab/{SLUG}/session").status_code == 302


def test_token_for_own_session(client, ada, fake_labd, settings):
    client.force_login(ada)
    start(client)
    sid = str(Session.objects.get(user_id=ada.id).id)
    resp = client.get(f"/lab/{SLUG}/session/token", {"session": sid})
    assert resp.status_code == 200
    assert resp["Cache-Control"].startswith("max-age=0")
    body = resp.json()
    payload = base64.urlsafe_b64decode(body["token"].split(".")[0] + "==").decode()
    assert payload.startswith(f"{sid}|{ada.id}|")


def test_token_refuses_another_users_session(client, ada, bob, fake_labd):
    client.force_login(bob)
    start(client)
    bob_sid = str(Session.objects.get(user_id=bob.id).id)
    client.force_login(ada)
    assert client.get(f"/lab/{SLUG}/session/token", {"session": bob_sid}).status_code == 403
    assert client.get(f"/lab/{SLUG}/session/token", {"session": "nope"}).status_code == 404
    assert (
        client.get(f"/lab/{SLUG}/session/token", {"session": str(uuid.uuid4())}).status_code == 404
    )


def test_token_for_ended_session_is_410(client, ada, fake_labd):
    now = timezone.now()
    s = Session.objects.create(
        id=uuid.uuid4(), user_id=ada.id, challenge_slug=SLUG, state="ended",
        created_at=now, end_reason="idle_timeout",
    )  # fmt: skip
    client.force_login(ada)
    resp = client.get(f"/lab/{SLUG}/session/token", {"session": str(s.id)})
    assert resp.status_code == 410
    assert resp.json()["reason"] == "idle_timeout"
