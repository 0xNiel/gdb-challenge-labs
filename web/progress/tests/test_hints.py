"""Task 6.9: hints in order, recorded; the boss has none."""

import pytest
from django.contrib.auth import get_user_model

from labs.models import Event
from progress.models import Progress

pytestmark = pytest.mark.django_db
SLUG = "tier1-01-off-by-one"


@pytest.fixture
def ada(imported):
    u = get_user_model().objects.create_user("ada", "ada@example.com", "pw")
    Progress.objects.create(user=u, challenge=imported[0])
    return u


def reveal(client, n, slug=SLUG):
    return client.post(f"/lab/{slug}/hint", {"n": n}, headers={"X-Partial": "1"})


def test_reveal_in_order_records_event(client, ada, imported):
    client.force_login(ada)
    resp = reveal(client, 1)
    assert resp.status_code == 200
    assert b"<code>break sum_scores</code>" in resp.content  # rendered as markdown
    assert b"Show hint 2 of 3" in resp.content
    assert Progress.objects.get(user=ada, challenge=imported[0]).hints_used == 1
    ev = Event.objects.get(type="hint_viewed")
    assert (ev.challenge_slug, ev.data) == (SLUG, {"index": 1, "cost": 0})
    reveal(client, 2)
    reveal(client, 3)
    assert Progress.objects.get(user=ada, challenge=imported[0]).hints_used == 3
    assert list(Event.objects.filter(type="hint_viewed").values_list("data__index", flat=True)) == [
        1,
        2,
        3,
    ]
    html = client.get(f"/lab/{SLUG}").content.decode()
    assert html.index("Hint 1") < html.index("Hint 2") < html.index("Hint 3")
    assert "No more hints" in html


def test_out_of_order_refused(client, ada, imported):
    client.force_login(ada)
    assert reveal(client, 2).status_code == 409
    assert reveal(client, 4).status_code == 409
    assert Progress.objects.get(user=ada, challenge=imported[0]).hints_used == 0
    assert not Event.objects.filter(type="hint_viewed").exists()
    reveal(client, 1)
    assert reveal(client, 1).status_code == 200  # a double click is harmless
    assert Event.objects.filter(type="hint_viewed").count() == 1


def test_boss_has_no_hints(client, ada, imported):
    for c in imported[:-1]:
        Progress.objects.update_or_create(user=ada, challenge=c, defaults={"state": "solved"})
    client.force_login(ada)
    boss = imported[-1].slug
    html = client.get(f"/lab/{boss}").content.decode()
    assert "no hints" in html
    assert reveal(client, 1, slug=boss).status_code == 409


def test_locked_hint_is_403(client, ada):
    client.force_login(ada)
    assert reveal(client, 1, slug="tier1-02-null-deref").status_code == 403
