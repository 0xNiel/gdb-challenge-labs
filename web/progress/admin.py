from django.contrib import admin

from .models import FlagAttempt, Progress


class ReadOnly(admin.ModelAdmin):
    def has_add_permission(self, request):
        return False

    def has_change_permission(self, request, obj=None):
        return False

    def has_delete_permission(self, request, obj=None):
        return False


@admin.register(Progress)
class ProgressAdmin(ReadOnly):
    list_display = ("user", "challenge", "state", "solved_at", "hints_used", "attempts")
    list_filter = ("state", "challenge")
    search_fields = ("user__email",)


@admin.register(FlagAttempt)
class FlagAttemptAdmin(ReadOnly):
    list_display = ("user", "challenge", "ts", "correct")
    list_filter = ("correct", "challenge")
    search_fields = ("user__email",)
