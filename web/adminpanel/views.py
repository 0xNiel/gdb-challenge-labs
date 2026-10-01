"""The admin live view (spec "adminpanel"; Phase 7, task 7.7): labd's live sessions with a kill
button each, the capacity gauge, drain and resume (ADR 0018), challenge toggles and the
pending-pull banner. Staff only."""

from datetime import datetime

from django.contrib import messages
from django.contrib.auth import get_user_model
from django.shortcuts import get_object_or_404, redirect, render
from django.utils import timezone
from django.views.decorators.cache import never_cache
from django.views.decorators.http import require_POST

from analytics.views import staff_only
from curriculum.models import Challenge
from labs.labd_client import LabdError, get_client


def _age_s(iso: str | None) -> int | None:
    if not iso:
        return None
    try:
        return int((timezone.now() - datetime.fromisoformat(iso)).total_seconds())
    except ValueError:
        return None


@staff_only
@never_cache
def live(request):
    client = get_client()
    try:
        stats, sessions = client.stats(), client.list()
    except LabdError as e:
        return render(
            request, "adminpanel/live.html", {"error": str(e), "challenges": _challenges()}
        )
    emails = dict(
        get_user_model()
        .objects.filter(id__in={s.get("user_id") for s in sessions})
        .values_list("id", "email")
    )
    rows = sorted(
        (
            {
                **s,
                "email": emails.get(s.get("user_id"), f"user {s.get('user_id')}"),
                "age_s": _age_s(s.get("started_at") or s.get("created_at")),
            }  # fmt: skip
            for s in sessions
        ),
        key=lambda r: (r.get("state") != "running", -(r["age_s"] or 0)),
    )
    cap = stats.get("max_sessions") or 0
    return render(
        request,
        "adminpanel/live.html",
        {
            "stats": stats,
            "sessions": rows,
            "pct": round(100 * stats.get("active", 0) / cap) if cap else 100,
            "challenges": _challenges(),
        },
    )


def _challenges():
    return Challenge.objects.select_related("tier").order_by("tier__order", "order")


@staff_only
@require_POST
def kill(request, session_id):
    try:
        get_client().stop(str(session_id), "admin_kill")
        messages.success(request, f"Session {str(session_id)[:8]} killed.")
    except LabdError as e:
        messages.error(request, f"Kill failed: {e}")
    return redirect("admin_live")


@staff_only
@require_POST
def drain(request):
    on = request.POST.get("drain") == "1"
    try:
        get_client().drain(on)
        messages.success(
            request, "Draining: no new lab starts." if on else "Resumed: labs start again."
        )
    except LabdError as e:
        messages.error(request, f"Drain failed: {e}")
    return redirect("admin_live")


@staff_only
@require_POST
def toggle(request, slug):
    ch = get_object_or_404(Challenge, slug=slug)
    ch.enabled = not ch.enabled
    ch.save(update_fields=["enabled"])
    messages.success(request, f"{slug} {'enabled' if ch.enabled else 'disabled'}.")
    return redirect("admin_live")
