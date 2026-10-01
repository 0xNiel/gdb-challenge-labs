"""Task 6.3: import_challenges creates, updates idempotently, disables, never deletes."""

import copy
from io import StringIO

import pytest
from django.core.management import CommandError, call_command

from conftest import REPO
from curriculum.importer import import_challenges
from curriculum.models import Challenge, Lesson, Tier

pytestmark = pytest.mark.django_db


def test_creates_five_challenges_and_lessons(challenges_doc, write_doc):
    res = import_challenges(write_doc(challenges_doc), REPO)
    assert len(res.created) == 5
    assert Challenge.objects.count() == 5
    assert Lesson.objects.count() == 5
    tier = Tier.objects.get()
    assert (tier.slug, tier.number, tier.title) == ("tier1-c-fundamentals", 1, "gdb fundamentals")
    c = Challenge.objects.get(slug="tier1-01-off-by-one")
    assert c.lesson.title == "Meet gdb: run, break, step, look"
    assert not c.lesson.body_md.startswith("# ")  # the heading became the title
    assert "sum_scores" in c.source
    assert c.solution_md
    assert len(c.hints) == 3
    assert Challenge.objects.get(slug="tier1-05-stack-overwrite").boss


def test_reimport_changes_nothing(challenges_doc, write_doc):
    path = write_doc(challenges_doc)
    import_challenges(path, REPO)
    res = import_challenges(path, REPO)
    assert len(res.unchanged) == 5
    assert res.created == res.updated == res.disabled == []


def test_absent_challenge_is_disabled_not_deleted(challenges_doc, write_doc):
    import_challenges(write_doc(challenges_doc), REPO)
    smaller = copy.deepcopy(challenges_doc)
    smaller["challenges"] = [c for c in smaller["challenges"] if c["order"] != 4]
    res = import_challenges(write_doc(smaller), REPO)
    assert res.disabled == ["tier1-04-unterminated"]
    assert Challenge.objects.count() == 5
    assert not Challenge.objects.get(slug="tier1-04-unterminated").enabled


def test_changed_digest_updates(challenges_doc, write_doc):
    import_challenges(write_doc(challenges_doc), REPO)
    new = "ghcr.io/test/lab-tier1-02-null-deref@sha256:" + "f" * 64
    challenges_doc["challenges"][1]["image"] = new
    res = import_challenges(write_doc(challenges_doc), REPO)
    assert res.updated == ["tier1-02-null-deref"]
    assert Challenge.objects.get(slug="tier1-02-null-deref").image_digest == new


def test_command_reports_and_rejects_bad_json(challenges_doc, write_doc, tmp_path):
    out = StringIO()
    call_command(
        "import_challenges", str(write_doc(challenges_doc)), "--repo", str(REPO), stdout=out
    )
    assert "5 created" in out.getvalue()
    bad = tmp_path / "bad.json"
    bad.write_text('{"version": 2}')
    with pytest.raises(CommandError):
        call_command("import_challenges", str(bad), "--repo", str(REPO))


def test_committed_challenges_json_imports():
    """The real file, as committed: all five present (disabled until pushed, Q2)."""
    res = import_challenges(REPO / "challenges.json", REPO)
    assert len(res.created) == 5
