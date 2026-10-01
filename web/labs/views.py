"""Challenge page, start/stop, the terminal page and its token (spec "Start-a-lab flow").

web decides entitlement (logged in, unlocked, one active session per user) and asks labd for
the session. labd writes the `sessions` row before it answers, so web reads state from there.
"""

from django.conf import settings
from django.contrib import messages
from django.contrib.auth.decorators import login_required
from django.core.exceptions import PermissionDenied, ValidationError
from django.http import Http404, JsonResponse
from django.shortcuts import get_object_or_404, redirect, render
from django.views.decorators.cache import never_cache
from django.views.decorators.http import require_GET, require_POST

from curriculum.markdown import render as render_md
from curriculum.models import Challenge
from progress.services import lab_panel, state_of

from .labd_client import LabdError, get_client
from .models import LIVE_STATES, Session
from .services import active_session

LABD_ERRORS = {
    "queue_full": "Every lab slot is busy and the queue is full. Try again in a minute.",
    "unknown_challenge": "This lab is not available right now.",
    "shutting_down": "The lab service is restarting. Try again in a minute.",
    "unavailable": "The lab service is not reachable. Try again in a minute.",
}


def _challenge_for(request, slug) -> tuple[Challenge, str]:
    ch = get_object_or_404(Challenge.objects.select_related("tier", "lesson"), slug=slug)
    state = state_of(request.user, ch)
    if state == "locked":
        raise PermissionDenied("This challenge is locked.")
    return ch, state


@login_required
@require_GET
def challenge(request, slug):
    ch, state = _challenge_for(request, slug)
    active = active_session(request.user)
    return render(
        request,
        "labs/challenge.html",
        {
            "challenge": ch,
            "state": state,
            "readme": render_md(ch.readme_md),
            "active": active,
            "active_here": active is not None and active.challenge_slug == ch.slug,
            **lab_panel(request.user, ch),
        },
    )


@login_required
@require_POST
def start(request, slug):
    ch, _ = _challenge_for(request, slug)
    if not ch.enabled:
        raise PermissionDenied("This challenge is disabled.")
    active = active_session(request.user)
    if active is not None and active.challenge_slug != ch.slug:
        messages.error(
            request,
            f"You already have a lab running for {active.challenge_slug}. Stop it first.",
        )
        return redirect("challenge", slug=active.challenge_slug)
    try:
        get_client().start(request.user.id, ch.slug)
    except LabdError as e:
        messages.error(request, LABD_ERRORS.get(e.kind, f"The lab could not start ({e.kind})."))
        return redirect("challenge", slug=ch.slug)
    return redirect("lab_session", slug=ch.slug)


@login_required
@require_POST
def stop(request, slug):
    ch, _ = _challenge_for(request, slug)
    active = active_session(request.user)
    if active is not None and active.challenge_slug == ch.slug:
        try:
            get_client().stop(str(active.id), "user_stop")
        except LabdError as e:
            if e.kind != "not_found":
                messages.error(request, LABD_ERRORS.get(e.kind, "The lab could not be stopped."))
                return redirect("lab_session", slug=ch.slug)
        messages.info(request, "Lab stopped.")
    return redirect("challenge", slug=ch.slug)


@login_required
@require_GET
@never_cache
def session_page(request, slug):
    """The terminal page; only while this user's session for this challenge is live."""
    ch, state = _challenge_for(request, slug)
    s = active_session(request.user)
    if s is None or s.challenge_slug != ch.slug or s.state not in LIVE_STATES:
        messages.info(request, "Start the lab to open its terminal.")
        return redirect("challenge", slug=ch.slug)
    lesson = render_md(ch.lesson.body_md) if ch.lesson else ""
    return render(
        request,
        "labs/session.html",
        {
            "challenge": ch,
            "state": state,
            "session": s,
            "lesson": lesson,
            "source_lines": ch.source.splitlines(),
            "ws_base": settings.LABD_WS_BASE,
            **lab_panel(request.user, ch),
        },
    )


@login_required
@require_GET
@never_cache
def session_token(request, slug):
    """A fresh single-use WebSocket token (ADR 0015). Re-checks the Django session and that the
    session belongs to this user, so a cookie is needed to (re)open a terminal."""
    ch, _ = _challenge_for(request, slug)
    sid = request.GET.get("session", "")
    try:
        s = Session.objects.get(id=sid)
    except (Session.DoesNotExist, ValidationError, ValueError) as e:
        raise Http404("no such session") from e
    if s.user_id != request.user.id or s.challenge_slug != ch.slug:
        raise PermissionDenied("not your session")
    if s.state not in LIVE_STATES:
        return JsonResponse(
            {"error": "ended", "state": s.state, "reason": s.end_reason}, status=410
        )
    token = get_client().token(str(s.id), request.user.id)
    return JsonResponse({"session_id": str(s.id), "token": token, "state": s.state})
