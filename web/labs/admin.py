from django.contrib import admin

from progress.admin import ReadOnly

from .models import Event, Session


@admin.register(Session)
class SessionAdmin(ReadOnly):
    """labd's rows (ADR 0003): read-only here. Live control comes in Phase 7 (/admin/live)."""

    list_display = (
        "id",
        "user_id",
        "challenge_slug",
        "state",
        "created_at",
        "end_reason",
        "commands",
    )
    list_filter = ("state", "end_reason", "challenge_slug")
    search_fields = ("id", "challenge_slug")


@admin.register(Event)
class EventAdmin(ReadOnly):
    list_display = ("ts", "type", "user_id", "challenge_slug", "session_id")
    list_filter = ("type",)
    search_fields = ("challenge_slug",)
