"""Task 6.7: the terminal page's template (the browser part is the e2e test and the human check)."""

import pytest
from django.contrib.auth import get_user_model

from labs.models import Session
from progress.models import Progress

pytestmark = pytest.mark.django_db
SLUG = "tier1-01-off-by-one"


@pytest.fixture
def page(client, imported, fake_labd):
    u = get_user_model().objects.create_user("ada", "ada@example.com", "pw")
    Progress.objects.create(user=u, challenge=imported[0])
    client.force_login(u)
    client.post(f"/lab/{SLUG}/start")
    resp = client.get(f"/lab/{SLUG}/session")
    assert resp.status_code == 200
    return resp, Session.objects.get(user_id=u.id)


def test_command_recording_notice_present(page):
    """S19: users are told that commands are recorded."""
    resp, _ = page
    assert b"Commands are recorded to improve hints." in resp.content


def test_page_wiring(page):
    resp, s = page
    html = resp.content.decode()
    assert f'data-session="{s.id}"' in html
    assert f'data-token-url="/lab/{SLUG}/session/token"' in html
    for asset in (
        "vendor/xterm-5.5.0/xterm-5.5.0.js",
        "vendor/xterm-5.5.0/addon-fit-0.10.0.js",
        "js/lab.js",
    ):
        assert asset in html
    assert "cdn" not in html.lower()  # everything is vendored (CONVENTIONS)
    for tab in ("lesson", "source", "hints", "flag"):
        assert f'data-tab="{tab}"' in html
    assert '<td class="ln">1</td>' in html  # source with server-side line numbers
    assert "sum_scores" in html
    assert 'action="/lab/tier1-01-off-by-one/stop"' in html
    assert resp["Cache-Control"].startswith("max-age=0")


def test_source_is_escaped(client, imported, fake_labd):
    imported[0].source = "<script>alert(1)</script>"
    imported[0].save()
    u = get_user_model().objects.create_user("bob", "bob@example.com", "pw")
    client.force_login(u)
    client.post(f"/lab/{SLUG}/start")
    html = client.get(f"/lab/{SLUG}/session").content.decode()
    assert "<script>alert(1)</script>" not in html
