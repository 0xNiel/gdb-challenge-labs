"""WebSocket tokens for the terminal gateway (S12, ADR 0012, ADR 0015).

Mirrors labd/internal/term/token.go:

    payload = session_id "|" user_id "|" exp_unix "|" base64url(8 random bytes)
    token   = base64url(payload) "." base64url(HMAC-SHA256(WS_TOKEN_KEY, payload))

labd verifies it: signature, 60 s expiry, the session id, single use.
"""

import base64
import hashlib
import hmac
import os
import time

TTL_S = 60


def _b64(b: bytes) -> str:
    return base64.urlsafe_b64encode(b).rstrip(b"=").decode()


def mint(
    key: str, session_id: str, user_id: int, exp: int | None = None, nonce: bytes | None = None
) -> str:
    """A token for (session_id, user_id) valid until exp (default: now + 60 s)."""
    if exp is None:
        exp = int(time.time()) + TTL_S
    if nonce is None:
        nonce = os.urandom(8)
    payload = f"{session_id}|{user_id}|{exp}|{_b64(nonce)}".encode()
    sig = hmac.new(key.encode(), payload, hashlib.sha256).digest()
    return f"{_b64(payload)}.{_b64(sig)}"
