import pytest

from config.settings.base import database_from_url


def test_healthz_ok(client):
    resp = client.get("/healthz")
    assert resp.status_code == 200
    assert resp.content == b"ok"


def test_healthz_rejects_post(client):
    assert client.post("/healthz").status_code == 405


@pytest.mark.parametrize(
    ("url", "expected"),
    [
        (
            "postgres://web:p%40ss@127.0.0.1:5432/labs",
            {
                "ENGINE": "django.db.backends.postgresql",
                "NAME": "labs",
                "USER": "web",
                "PASSWORD": "p@ss",
                "HOST": "127.0.0.1",
                "PORT": "5432",
            },
        ),
        ("sqlite:///:memory:", {"ENGINE": "django.db.backends.sqlite3", "NAME": ":memory:"}),
    ],
)
def test_database_from_url(url, expected):
    assert database_from_url(url) == expected


def test_database_from_url_rejects_unknown_scheme():
    with pytest.raises(ValueError):
        database_from_url("mysql://x@y/z")
