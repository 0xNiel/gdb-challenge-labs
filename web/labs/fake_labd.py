"""An in-process labd for tests (settings.LABD_CLIENT in config.settings.test).

It writes `sessions` rows the way labd does, so the views read them as they would in
production, and records every call for assertions.
"""

import uuid

from django.utils import timezone

from .labd_client import LabdClient, LabdError
from .models import ACTIVE_STATES, Session, record_event


class FakeLabd(LabdClient):
    calls: list[tuple] = []  # shared across instances; reset by the `fake_labd` fixture
    fail_with: str | None = None  # set to make the next start raise LabdError(fail_with)
    start_state = "running"

    def __init__(self, *args, **kwargs):
        super().__init__(base_url="http://fake-labd", secret="fake")  # noqa: S106  test only

    def start(self, user_id, slug):
        FakeLabd.calls.append(("start", user_id, slug))
        if FakeLabd.fail_with:
            kind, FakeLabd.fail_with = FakeLabd.fail_with, None
            raise LabdError(kind)
        s = Session.objects.filter(user_id=user_id, state__in=ACTIVE_STATES).first()
        if s is None:  # labd: one session per user; a second start returns it
            now = timezone.now()
            s = Session.objects.create(
                id=uuid.uuid4(),
                user_id=user_id,
                challenge_slug=slug,
                state=FakeLabd.start_state,
                created_at=now,
                started_at=now if FakeLabd.start_state == "running" else None,
            )
            record_event("lab_requested", user_id=user_id, session_id=s.id, challenge_slug=slug)
            if s.state == "running":
                record_event("lab_started", user_id=user_id, session_id=s.id, challenge_slug=slug)
        pos = 1 if s.state == "queued" else 0
        return {"session_id": str(s.id), "ws_token": "", "state": s.state, "queue_position": pos}

    def stop(self, session_id, reason="user_stop"):
        FakeLabd.calls.append(("stop", str(session_id), reason))
        n = Session.objects.filter(id=session_id, state__in=ACTIVE_STATES).update(
            state="ended", end_reason=reason, ended_at=timezone.now()
        )
        if not n:
            raise LabdError("not_found", status=404)
        return {"session_id": str(session_id), "state": "ending"}

    def list(self):
        return [
            {"session_id": str(s.id), "user_id": s.user_id, "state": s.state}
            for s in Session.objects.filter(state__in=ACTIVE_STATES)
        ]

    def stats(self):
        return {"active": Session.objects.filter(state__in=ACTIVE_STATES).count()}
