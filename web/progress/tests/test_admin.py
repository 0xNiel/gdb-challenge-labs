"""Task 6.11: Django admin loads for staff; Progress and labd's rows are read-only."""

import pytest
from django.contrib.auth import get_user_model

from curriculum.models import Challenge
from progress.models import Progress

pytestmark = pytest.mark.django_db


@pytest.fixture
def staff(client, imported):
    u = get_user_model().objects.create_superuser("root", "root@example.com", "pw")
    Progress.objects.create(user=u, challenge=imported[0])
    client.force_login(u)
    return u


@pytest.mark.parametrize(
    "url",
    [
        "/admin/",
        "/admin/curriculum/tier/",
        "/admin/curriculum/lesson/",
        "/admin/curriculum/challenge/",
        "/admin/progress/progress/",
        "/admin/progress/flagattempt/",
        "/admin/labs/session/",
        "/admin/labs/event/",
    ],
)
def test_admin_pages_load_for_staff(client, staff, url):
    assert client.get(url).status_code == 200


def test_non_staff_is_sent_to_login(client, imported):
    u = get_user_model().objects.create_user("ada", "ada@example.com", "pw")
    client.force_login(u)
    assert client.get("/admin/").status_code == 302


def test_enable_toggle_in_challenge_list(client, staff, imported):
    html = client.get("/admin/curriculum/challenge/").content.decode()
    assert 'name="form-0-enabled"' in html
    ch = Challenge.objects.get(slug="tier1-03-uninitialized")
    resp = client.get(f"/admin/curriculum/challenge/{ch.id}/change/")
    assert resp.status_code == 200


def test_progress_is_read_only(client, staff, imported):
    p = Progress.objects.get(user=staff)
    resp = client.get(f"/admin/progress/progress/{p.id}/change/")
    assert resp.status_code == 200
    assert b'name="state"' not in resp.content  # shown, not editable
    assert client.get("/admin/progress/progress/add/").status_code == 403
    assert (
        client.post(f"/admin/progress/progress/{p.id}/delete/", {"post": "yes"}).status_code == 403
    )
