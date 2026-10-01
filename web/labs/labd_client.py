"""web's client for labd's internal API (spec "Django ↔ labd contract"; labd/internal/api).

Loopback HTTP with the shared bearer secret (S11). Standard library only. `get_client()`
returns the class named by settings.LABD_CLIENT, so tests inject labs.fake_labd.FakeLabd.
"""

import json
import urllib.error
import urllib.request

from django.conf import settings
from django.utils.module_loading import import_string

from . import tokens

START_TIMEOUT_S = 3  # spec: web never blocks on container creation longer than 3 s
TIMEOUT_S = 5


class LabdError(Exception):
    """labd refused or could not be reached. `kind` is labd's error string or 'unavailable'."""

    def __init__(self, kind: str, detail: str = "", status: int = 0):
        super().__init__(f"{kind}: {detail}" if detail else kind)
        self.kind, self.detail, self.status = kind, detail, status


class LabdClient:
    def __init__(self, base_url: str | None = None, secret: str | None = None):
        self.base = (base_url or settings.LABD_INTERNAL_URL).rstrip("/")
        self.secret = secret if secret is not None else settings.LABD_INTERNAL_SECRET

    def _call(self, method: str, path: str, body: dict | None = None, timeout=TIMEOUT_S) -> dict:
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(  # noqa: S310  base is settings.LABD_INTERNAL_URL
            self.base + path, data=data, method=method
        )
        req.add_header("Authorization", f"Bearer {self.secret}")
        if data is not None:
            req.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310
                return json.loads(resp.read() or b"{}")
        except urllib.error.HTTPError as e:
            try:
                err = json.loads(e.read() or b"{}")
            except ValueError:
                err = {}
            raise LabdError(err.get("error", "http_error"), err.get("detail", ""), e.code) from e
        except (urllib.error.URLError, TimeoutError, OSError) as e:
            raise LabdError("unavailable", str(e)) from e

    def start(self, user_id: int, slug: str) -> dict:
        """{session_id, state, queue_position}. Raises LabdError('queue_full'|...)."""
        return self._call(
            "POST",
            "/internal/sessions",
            {"user_id": user_id, "challenge_slug": slug},
            timeout=START_TIMEOUT_S,
        )

    def stop(self, session_id: str, reason: str = "user_stop") -> dict:
        return self._call("DELETE", f"/internal/sessions/{session_id}", {"reason": reason})

    def token(self, session_id: str, user_id: int) -> str:
        """web mints the browser's tokens itself (ADR 0015); no call to labd."""
        return tokens.mint(settings.WS_TOKEN_KEY, session_id, user_id)

    def list(self) -> list[dict]:
        return self._call("GET", "/internal/sessions").get("sessions", [])

    def stats(self) -> dict:
        return self._call("GET", "/internal/stats")

    def drain(self, on: bool) -> dict:
        """Drain (effective max_sessions 0) or resume (ADR 0018). Returns the stats."""
        return self._call("POST", "/internal/drain", {"drain": on})


def get_client():
    return import_string(settings.LABD_CLIENT)()
