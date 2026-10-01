"""labd's tables, mapped read-mostly (ADR 0003, ADR 0011).

labd creates and migrates `sessions`, `events` and `samples`; Django never does
(`managed = False`). web reads `sessions` and appends to `events`. The columns must match
labd/internal/store/migrations/; labs/tests/test_models.py compares them.
"""

import uuid

from django.db import models
from django.utils import timezone

# Session states that hold a slot or a queue place (labd orch.State).
LIVE_STATES = ("queued", "creating", "running")
ACTIVE_STATES = (*LIVE_STATES, "ending")


class Session(models.Model):
    id = models.UUIDField(primary_key=True, default=uuid.uuid4, editable=False)
    user_id = models.BigIntegerField()
    challenge_slug = models.TextField()
    image = models.TextField(default="")
    state = models.TextField()
    created_at = models.DateTimeField()
    started_at = models.DateTimeField(null=True)
    ended_at = models.DateTimeField(null=True)
    end_reason = models.TextField(default="")
    container_id = models.TextField(default="")
    extended = models.BooleanField(default=False)
    peak_rss_mb = models.FloatField(null=True)
    commands = models.IntegerField(default=0)

    class Meta:
        managed = False
        db_table = "sessions"

    def __str__(self) -> str:
        return f"{self.id} {self.challenge_slug} {self.state}"

    @property
    def duration_s(self) -> int:
        if not self.started_at:
            return 0
        end = self.ended_at or timezone.now()
        return max(int((end - self.started_at).total_seconds()), 0)


class Event(models.Model):
    id = models.BigAutoField(primary_key=True)
    ts = models.DateTimeField(default=timezone.now)
    type = models.TextField()
    user_id = models.BigIntegerField(null=True)
    session_id = models.UUIDField(null=True)
    challenge_slug = models.TextField(null=True)  # noqa: DJ001  labd: nullable text
    data = models.JSONField(default=dict)

    class Meta:
        managed = False
        db_table = "events"

    def __str__(self) -> str:
        return f"{self.ts:%Y-%m-%d %H:%M:%S} {self.type}"


def record_event(type_: str, *, user_id=None, session_id=None, challenge_slug=None, **data):
    """Append one product event (spec "Metrics & analytics → Event schema")."""
    return Event.objects.create(
        type=type_, user_id=user_id, session_id=session_id, challenge_slug=challenge_slug, data=data
    )
