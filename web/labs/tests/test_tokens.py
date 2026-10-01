"""Shared WebSocket token vectors (ADR 0015): Python mints exactly what Go mints."""

import base64
import json
import time
from pathlib import Path

import pytest

from labs import tokens

DOC = json.loads(
    (
        Path(__file__).resolve().parents[3] / "challenges" / "schema" / "ws_token_vectors.json"
    ).read_text()
)


@pytest.mark.parametrize("v", DOC["vectors"], ids=[v["session_id"] for v in DOC["vectors"]])
def test_vector(v):
    got = tokens.mint(
        DOC["key"], v["session_id"], v["user_id"], v["exp"], bytes.fromhex(v["nonce_hex"])
    )
    assert got == v["token"]


def test_default_expiry_and_random_nonce():
    a = tokens.mint("k", "s", 1)
    b = tokens.mint("k", "s", 1)
    assert a != b  # random nonce: two tokens in one second differ (ADR 0012)
    payload = base64.urlsafe_b64decode(a.split(".")[0] + "==").decode()
    sid, uid, exp, nonce = payload.split("|")
    assert (sid, uid) == ("s", "1")
    assert abs(int(exp) - (int(time.time()) + 60)) <= 1
    assert len(base64.urlsafe_b64decode(nonce + "=")) == 8
