from django.contrib.auth.decorators import login_required
from django.shortcuts import render

from progress.services import states


@login_required
def learn(request):
    return render(request, "curriculum/learn.html", {"items": states(request.user)})
