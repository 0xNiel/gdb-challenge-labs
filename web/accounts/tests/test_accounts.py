"""Task 6.4: signup unlocks tier1-01; login; logout; password reset email."""

import pytest
from django.contrib.auth import get_user_model
from django.core import mail

from labs.models import Event
from progress.models import Progress
from progress.services import is_unlocked

pytestmark = pytest.mark.django_db
PW = "correct-horse-battery-9"


def signup(client, email="ada@example.com"):
    return client.post("/signup", {"email": email, "password1": PW, "password2": PW})


def test_signup_creates_user_and_unlocks_first(client, imported):
    resp = signup(client)
    assert resp.status_code == 302 and resp["Location"] == "/learn"
    user = get_user_model().objects.get(email="ada@example.com")
    first, second = imported[0], imported[1]
    assert first.slug == "tier1-01-off-by-one"
    assert Progress.objects.get(user=user, challenge=first).state == "unlocked"
    assert is_unlocked(user, first)
    assert not is_unlocked(user, second)
    assert Event.objects.filter(type="user_signed_up", user_id=user.id).exists()


def test_signup_rejects_duplicate_email(client, imported):
    signup(client)
    client.logout()
    resp = signup(client)
    assert resp.status_code == 200  # the form again, with an error
    assert get_user_model().objects.count() == 1


def test_login_and_logout(client, imported):
    signup(client)
    client.post("/accounts/logout/")
    assert client.get("/learn").status_code == 302  # logged out: redirected to /login
    resp = client.post("/login", {"login": "ada@example.com", "password": PW})
    assert resp.status_code == 302 and resp["Location"] == "/learn"
    assert client.get("/learn").status_code == 200
    assert Event.objects.filter(type="user_logged_in").count() >= 1
    resp = client.post("/accounts/logout/")
    assert resp.status_code == 302
    assert client.get("/learn")["Location"].startswith("/login")


def test_wrong_password_refused(client, imported):
    signup(client)
    client.post("/accounts/logout/")
    resp = client.post("/login", {"login": "ada@example.com", "password": "nope"})
    assert resp.status_code == 200
    assert client.get("/learn").status_code == 302


def test_password_reset_sends_email(client, imported):
    signup(client)
    client.post("/accounts/logout/")
    mail.outbox.clear()
    resp = client.post("/accounts/password/reset/", {"email": "ada@example.com"})
    assert resp.status_code == 302
    assert len(mail.outbox) == 1
    assert "ada@example.com" in mail.outbox[0].to
    assert "/accounts/password/reset/key/" in mail.outbox[0].body


def test_pages_render(client):
    for url in ("/", "/login", "/signup", "/accounts/password/reset/"):
        assert client.get(url).status_code == 200, url
