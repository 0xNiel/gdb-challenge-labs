from functools import wraps

from django.contrib.auth.views import redirect_to_login
from django.core.exceptions import PermissionDenied
from django.shortcuts import render
from django.views.decorators.cache import never_cache

from . import dashboards

PAGES = [("live", "Live"), ("usage", "Usage"), ("learning", "Learning"), ("capacity", "Capacity")]


def staff_only(view):
    """Anonymous: to the login page. Logged in but not staff: 403."""

    @wraps(view)
    def wrapped(request, *args, **kwargs):
        if not request.user.is_authenticated:
            return redirect_to_login(request.get_full_path())
        if not request.user.is_staff:
            raise PermissionDenied("staff only")
        return view(request, *args, **kwargs)

    return wrapped


def _page(name):
    @staff_only
    @never_cache
    def view(request):
        ctx = getattr(dashboards, name)()
        return render(request, f"analytics/{name}.html", {"page": name, "pages": PAGES, **ctx})

    view.__name__ = name
    return view


live = _page("live")
usage = _page("usage")
learning = _page("learning")
capacity = _page("capacity")
