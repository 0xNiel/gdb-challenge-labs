from django.contrib.auth.decorators import login_required
from django.core.exceptions import PermissionDenied
from django.shortcuts import get_object_or_404, render

from curriculum.models import Challenge
from progress.services import state_of


@login_required
def challenge(request, slug):
    ch = get_object_or_404(Challenge.objects.select_related("tier", "lesson"), slug=slug)
    state = state_of(request.user, ch)
    if state == "locked":
        raise PermissionDenied("This challenge is locked.")
    return render(request, "labs/challenge.html", {"challenge": ch, "state": state})
