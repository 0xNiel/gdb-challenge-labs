"""Flag derivation and checking (ADR 0005, S15). Mirrors labd/internal/flag/flag.go.

Flags are never stored: the expected flag is derived from DEPLOY_SECRET and the slug on every
check and compared in constant time.
"""

import base64
import hashlib
import hmac


def derive(secret: str, slug: str) -> str:
    mac = hmac.new(secret.encode(), slug.encode(), hashlib.sha256).digest()
    body = base64.b32encode(mac).decode().rstrip("=")[:24]
    return f"LAB{{{body}}}"


def check(secret: str, slug: str, submitted: str) -> bool:
    """True when `submitted`, stripped of surrounding whitespace, is the flag for `slug`.

    Case-sensitive. hmac.compare_digest keeps the time independent of where they differ.
    """
    return hmac.compare_digest(submitted.strip().encode(), derive(secret, slug).encode())
