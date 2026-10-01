from itertools import groupby

from django.contrib.auth.decorators import login_required
from django.core.exceptions import PermissionDenied
from django.shortcuts import get_object_or_404, render

from progress.services import is_unlocked, states

from .markdown import render as render_md
from .models import Lesson


@login_required
def learn(request):
    """Curriculum map: tiers, then each tier's lessons and challenges with their state."""
    items = states(request.user)
    tiers = [
        (tier, list(group)) for tier, group in groupby(items, key=lambda it: it.challenge.tier)
    ]
    summary = [
        {
            "tier": tier,
            "items": group,
            "solved": sum(it.state == "solved" for it in group),
        }
        for tier, group in tiers
    ]
    return render(request, "curriculum/learn.html", {"tiers": summary})


@login_required
def lesson(request, tier, slug):
    lesson = get_object_or_404(Lesson.objects.select_related("tier"), tier__slug=tier, slug=slug)
    challenge = getattr(lesson, "challenge", None)
    if challenge is None or not is_unlocked(request.user, challenge):
        raise PermissionDenied("This lesson unlocks with its challenge.")
    return render(
        request,
        "curriculum/lesson.html",
        {"lesson": lesson, "challenge": challenge, "body": render_md(lesson.body_md)},
    )
