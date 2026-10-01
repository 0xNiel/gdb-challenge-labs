"""Session helpers shared by the lab and progress views."""

import logging

from .labd_client import LabdError, get_client
from .models import ACTIVE_STATES, LIVE_STATES, Session

log = logging.getLogger(__name__)


def active_session(user) -> Session | None:
    """The user's session that holds a slot or a queue place, if any (spec: one per user)."""
    return (
        Session.objects.filter(user_id=user.id, state__in=ACTIVE_STATES)
        .order_by("-created_at")
        .first()
    )


def stop_on_solve(user, slug: str) -> bool:
    """ADR 0016: a correct flag stops the user's live lab for that challenge (reason `solved`).

    Returns True if a lab was stopped. A failure is logged, never raised: the solve counts
    either way, and the lab then ends by its idle timeout.
    """
    s = active_session(user)
    if s is None or s.challenge_slug != slug or s.state not in LIVE_STATES:
        return False
    try:
        get_client().stop(str(s.id), "solved")
    except LabdError as e:
        log.warning("stop on solve failed: session_id=%s err=%s", s.id, e)
        return False
    return True
