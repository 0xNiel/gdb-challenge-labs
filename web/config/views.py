from django.http import HttpRequest, HttpResponse
from django.views.decorators.http import require_GET


@require_GET
def healthz(request: HttpRequest) -> HttpResponse:
    """Liveness for Caddy and systemd. Does not touch the database."""
    return HttpResponse("ok", content_type="text/plain")
