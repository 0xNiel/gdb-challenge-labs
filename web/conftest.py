"""Shared pytest fixtures.

labd owns `sessions` and `events` (ADR 0003), so Django's test database lacks them. They
are created here from the unmanaged models, which labs/tests/test_models.py keeps equal to
labd's migration.
"""

import json
from pathlib import Path

import pytest
from django.apps import apps
from django.db import connection


@pytest.fixture(scope="session")
def django_db_setup(django_db_setup, django_db_blocker):
    with django_db_blocker.unblock():
        existing = set(connection.introspection.table_names())
        with connection.schema_editor() as editor:
            for model in apps.get_models():
                if model._meta.managed or model._meta.db_table in existing:
                    continue
                if model._meta.db_table == "samples":
                    # labd's samples table has no primary key: `ts` is one only to Django.
                    sql, params = editor.table_sql(model)
                    editor.execute(sql.replace(" PRIMARY KEY", ""), params)
                else:
                    editor.create_model(model)


REPO = Path(__file__).resolve().parent.parent


@pytest.fixture
def challenges_doc():
    """A copy of the repo's challenges.json with every challenge enabled and a fake digest
    (the committed file lists them disabled until images are pushed, QUESTIONS Q2)."""
    doc = json.loads((REPO / "challenges.json").read_text())
    for c in doc["challenges"]:
        c["enabled"] = True
        c["image"] = f"ghcr.io/test/lab-{c['slug']}@sha256:" + "0" * 64
    return doc


@pytest.fixture
def write_doc(tmp_path):
    def write(doc) -> Path:
        p = tmp_path / "challenges.json"
        p.write_text(json.dumps(doc))
        return p

    return write


@pytest.fixture
def imported(db, challenges_doc, write_doc):
    """The five tier-1 challenges, enabled, as import_challenges leaves them."""
    from curriculum.importer import import_challenges

    import_challenges(write_doc(challenges_doc), REPO)
    from curriculum.models import Challenge

    return list(Challenge.objects.order_by("tier__order", "order"))


@pytest.fixture
def fake_labd():
    from labs.fake_labd import FakeLabd

    FakeLabd.calls = []
    FakeLabd.fail_with = None
    FakeLabd.start_state = "running"
    FakeLabd.stop_fails = False
    return FakeLabd
