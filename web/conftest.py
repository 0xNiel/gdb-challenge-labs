"""Shared pytest fixtures.

labd owns `sessions` and `events` (ADR 0003), so Django's test database lacks them. They
are created here from the unmanaged models, which labs/tests/test_models.py keeps equal to
labd's migration.
"""

import pytest
from django.apps import apps
from django.db import connection


@pytest.fixture(scope="session")
def django_db_setup(django_db_setup, django_db_blocker):
    with django_db_blocker.unblock():
        existing = set(connection.introspection.table_names())
        with connection.schema_editor() as editor:
            for model in apps.get_models():
                if not model._meta.managed and model._meta.db_table not in existing:
                    editor.create_model(model)
