from django.conf import settings
from django.contrib import messages
from django.contrib.auth.decorators import login_required
from django.core.exceptions import PermissionDenied
from django.http import HttpResponse
from django.shortcuts import get_object_or_404, redirect, render
from django.views.decorators.http import require_POST

from curriculum.models import Challenge

from .services import (
    HintOrder,
    RateLimited,
    is_unlocked,
    lab_panel,
    reveal_hint,
    submit_flag,
)


def _unlocked_challenge(request, slug) -> Challenge:
    ch = get_object_or_404(Challenge.objects.select_related("tier"), slug=slug)
    if not is_unlocked(request.user, ch):
        raise PermissionDenied("This challenge is locked.")
    return ch


def _wants_partial(request) -> bool:
    """The lab page submits with fetch and swaps the panel, so the terminal stays open."""
    return request.headers.get("X-Partial") == "1"


def _panel(request, ch, template, status=200, **extra) -> HttpResponse:
    ctx = {"challenge": ch, **lab_panel(request.user, ch), **extra}
    return render(request, template, ctx, status=status)


@login_required
@require_POST
def flag_submit(request, slug):
    ch = _unlocked_challenge(request, slug)
    try:
        res = submit_flag(request.user, ch, request.POST.get("flag", ""), settings.DEPLOY_SECRET)
    except RateLimited:
        msg = "Too many attempts: 10 per 10 minutes. Wait a few minutes and try again."
        if _wants_partial(request):
            return _panel(request, ch, "progress/partials/flag.html", status=429, error=msg)
        messages.error(request, msg)
        return redirect("challenge", slug=ch.slug)
    if res.already_solved:
        msg = "You have already solved this challenge."
    elif res.correct:
        msg = "Correct! Challenge solved."
        if res.next_challenge:
            msg += f" Next up: {res.next_challenge.title}."
    else:
        msg = "That is not the flag."
    if _wants_partial(request):
        return _panel(request, ch, "progress/partials/flag.html", result=res, message=msg)
    (messages.success if res.correct else messages.error)(request, msg)
    return redirect("challenge", slug=ch.slug)


@login_required
@require_POST
def hint_reveal(request, slug):
    ch = _unlocked_challenge(request, slug)
    try:
        n = int(request.POST.get("n", "0"))
        reveal_hint(request.user, ch, n)
    except (HintOrder, ValueError):
        if _wants_partial(request):
            return _panel(request, ch, "progress/partials/hints.html", status=409)
        messages.error(request, "Hints are revealed one at a time, in order.")
        return redirect("challenge", slug=ch.slug)
    if _wants_partial(request):
        return _panel(request, ch, "progress/partials/hints.html")
    return redirect("challenge", slug=ch.slug)


@login_required
def dashboard(request):
    return render(request, "progress/dashboard.html", {})
