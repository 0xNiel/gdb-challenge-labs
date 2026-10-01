"""LabdClient against a stand-in HTTP server: bearer, bodies, and error mapping."""

import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest

from labs.labd_client import LabdClient, LabdError


class Handler(BaseHTTPRequestHandler):
    seen: list = []
    reply: tuple = (200, {})

    def _handle(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = json.loads(self.rfile.read(n)) if n else None
        Handler.seen.append((self.command, self.path, self.headers.get("Authorization"), body))
        code, obj = Handler.reply
        data = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    do_GET = do_POST = do_DELETE = _handle

    def log_message(self, *args):
        pass


@pytest.fixture
def server():
    srv = HTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    Handler.seen = []
    yield LabdClient(f"http://127.0.0.1:{srv.server_port}", "s3cret")
    srv.shutdown()


def test_start_sends_bearer_and_body(server):
    Handler.reply = (
        200,
        {"session_id": "abc", "ws_token": "", "state": "creating", "queue_position": 0},
    )
    assert server.start(7, "tier1-01-off-by-one")["session_id"] == "abc"
    method, path, auth, body = Handler.seen[0]
    assert (method, path, auth) == ("POST", "/internal/sessions", "Bearer s3cret")
    assert body == {"user_id": 7, "challenge_slug": "tier1-01-off-by-one"}


def test_stop_sends_reason(server):
    Handler.reply = (200, {"session_id": "abc", "state": "ending"})
    server.stop("abc", "user_stop")
    assert Handler.seen[0][:2] == ("DELETE", "/internal/sessions/abc")
    assert Handler.seen[0][3] == {"reason": "user_stop"}


@pytest.mark.parametrize(
    ("code", "obj", "kind"),
    [
        (503, {"error": "queue_full", "retry_after_s": 30}, "queue_full"),
        (404, {"error": "unknown_challenge"}, "unknown_challenge"),
        (401, {"error": "unauthorized"}, "unauthorized"),
    ],
)
def test_errors_map_to_kind(server, code, obj, kind):
    Handler.reply = (code, obj)
    with pytest.raises(LabdError) as e:
        server.start(1, "x")
    assert (e.value.kind, e.value.status) == (kind, code)


def test_unreachable_is_unavailable():
    with pytest.raises(LabdError) as e:
        LabdClient("http://127.0.0.1:9", "x").stats()
    assert e.value.kind == "unavailable"
