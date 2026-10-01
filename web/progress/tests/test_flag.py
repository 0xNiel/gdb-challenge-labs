"""ADR 0005 vectors, shared with labd/internal/flag (S15)."""

import inspect
import json
from pathlib import Path

import pytest

from progress import flag

VECTORS = Path(__file__).resolve().parents[3] / "challenges" / "schema" / "flag_vectors.json"
CASES = json.loads(VECTORS.read_text())["vectors"]


def test_ten_vectors():
    assert len(CASES) == 10


@pytest.mark.parametrize("v", CASES, ids=[c["slug"] for c in CASES])
def test_derive_matches_vector(v):
    assert flag.derive(v["secret"], v["slug"]) == v["flag"]


@pytest.mark.parametrize("v", CASES, ids=[c["slug"] for c in CASES])
def test_check(v):
    good = v["flag"]
    assert flag.check(v["secret"], v["slug"], good)
    assert flag.check(v["secret"], v["slug"], f"  {good}\n")  # whitespace is trimmed
    assert not flag.check(v["secret"], v["slug"], good.lower())  # case-sensitive
    assert not flag.check(v["secret"], v["slug"], good[:-2] + "}")
    assert not flag.check(v["secret"] + "x", v["slug"], good)
    assert not flag.check(v["secret"], v["slug"], "")


def test_check_is_constant_time():
    """S15: the comparison is hmac.compare_digest, not ==."""
    src = inspect.getsource(flag.check)
    assert "hmac.compare_digest(" in src
    assert "==" not in src
