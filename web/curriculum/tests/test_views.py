"""Task 6.5: /learn and lessons."""

import pytest
from django.contrib.auth import get_user_model

from curriculum.models import Lesson
from progress.models import Progress

pytestmark = pytest.mark.django_db


@pytest.fixture
def user(imported):
    u = get_user_model().objects.create_user("ada", "ada@example.com", "pw")
    Progress.objects.create(user=u, challenge=imported[0])
    return u


@pytest.mark.parametrize(
    "url", ["/learn", "/learn/tier1-c-fundamentals/tier1-01-off-by-one", "/dashboard"]
)
def test_anonymous_redirected_to_login(client, imported, url):
    resp = client.get(url)
    assert resp.status_code == 302
    assert resp["Location"].startswith("/login?next=")


def test_learn_shows_tiers_with_lock_states(client, user, imported):
    client.force_login(user)
    resp = client.get("/learn")
    assert resp.status_code == 200
    html = resp.content.decode()
    assert "Tier 1: gdb fundamentals" in html
    states = [it.state for t in resp.context["tiers"] for it in t["items"]]
    assert states == ["unlocked", "locked", "locked", "locked", "locked"]
    Progress.objects.filter(user=user, challenge=imported[0]).update(state="solved")
    states = [it.state for t in client.get("/learn").context["tiers"] for it in t["items"]]
    assert states == ["solved", "unlocked", "locked", "locked", "locked"]


def test_lesson_renders_markdown(client, user):
    client.force_login(user)
    resp = client.get("/learn/tier1-c-fundamentals/tier1-01-off-by-one")
    assert resp.status_code == 200
    html = resp.content.decode()
    assert "<h1>Meet gdb: run, break, step, look</h1>" in html
    assert "<h2>" in html and "<pre><code" in html


def test_lesson_strips_script(client, user):
    Lesson.objects.filter(slug="tier1-01-off-by-one").update(
        body_md="Hi\n\n<script>alert('x')</script>\n"
    )
    client.force_login(user)
    html = client.get("/learn/tier1-c-fundamentals/tier1-01-off-by-one").content.decode()
    assert "<script>alert" not in html
    assert "&lt;script&gt;" in html


def test_locked_lesson_forbidden(client, user):
    client.force_login(user)
    assert client.get("/learn/tier1-c-fundamentals/tier1-02-null-deref").status_code == 403
    assert client.get("/learn/tier1-c-fundamentals/nope").status_code == 404
