from django.contrib import admin

from .models import Challenge, Lesson, Tier


@admin.register(Tier)
class TierAdmin(admin.ModelAdmin):
    list_display = ("number", "slug", "title", "order")


@admin.register(Lesson)
class LessonAdmin(admin.ModelAdmin):
    list_display = ("slug", "title", "tier", "order")
    list_filter = ("tier",)
    search_fields = ("slug", "title")


@admin.register(Challenge)
class ChallengeAdmin(admin.ModelAdmin):
    """Content comes from import_challenges; here an admin mostly turns challenges on and off."""

    list_display = (
        "slug",
        "title",
        "tier",
        "order",
        "difficulty",
        "boss",
        "enabled",
        "image_digest",
    )
    list_editable = ("enabled",)
    list_filter = ("tier", "enabled", "boss")
    search_fields = ("slug", "title")
    readonly_fields = (
        "image_digest",
        "limits",
        "hints",
        "tags",
        "solution_md",
        "source",
        "readme_md",
    )
