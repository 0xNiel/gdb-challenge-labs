"""End-to-end fixtures: a real browser against the stack from scripts/web-stack.sh.

These tests run only with E2E=1 (./run.sh test --e2e brings the stack up and down). They use
Playwright's sync API directly and read labd's rows with psycopg, not the Django ORM.
"""

import os

import psycopg
import pytest

BASE_URL = os.environ.get("E2E_BASE_URL", "http://127.0.0.1:8000")
DB_URL = os.environ.get("WEB_DATABASE_URL", "postgres://web:web@127.0.0.1:5432/labs")


def pytest_collection_modifyitems(config, items):
    if os.environ.get("E2E") == "1":
        return
    skip = pytest.mark.skip(reason="end-to-end: set E2E=1 (./run.sh test --e2e)")
    for item in items:
        if "tests/e2e/" in str(item.fspath):
            item.add_marker(skip)


@pytest.fixture(scope="session")
def browser():
    from playwright.sync_api import sync_playwright

    with sync_playwright() as p:
        b = p.chromium.launch(headless=os.environ.get("E2E_HEADED") != "1")
        yield b
        b.close()


@pytest.fixture
def page(browser):
    ctx = browser.new_context(base_url=BASE_URL, viewport={"width": 1400, "height": 900})
    pg = ctx.new_page()
    pg.set_default_timeout(30_000)
    yield pg
    ctx.close()


@pytest.fixture(scope="session")
def db():
    with psycopg.connect(DB_URL, autocommit=True) as conn:
        yield conn
