"""Per-user progress and flag attempts (spec "Data model"). web is the only writer."""

from django.conf import settings
from django.db import models
from django.utils import timezone


class Progress(models.Model):
    class State(models.TextChoices):
        LOCKED = "locked"
        UNLOCKED = "unlocked"
        SOLVED = "solved"

    user = models.ForeignKey(settings.AUTH_USER_MODEL, on_delete=models.CASCADE)
    challenge = models.ForeignKey("curriculum.Challenge", on_delete=models.CASCADE)
    state = models.CharField(max_length=10, choices=State.choices, default=State.UNLOCKED)
    solved_at = models.DateTimeField(null=True, blank=True)
    hints_used = models.PositiveSmallIntegerField(default=0)  # hints revealed, in order
    attempts = models.PositiveIntegerField(default=0)

    class Meta:
        db_table = "progress"
        verbose_name_plural = "progress"
        constraints = [
            models.UniqueConstraint(fields=["user", "challenge"], name="progress_user_challenge")
        ]

    def __str__(self) -> str:
        return f"{self.user} {self.challenge} {self.state}"


class FlagAttempt(models.Model):
    user = models.ForeignKey(settings.AUTH_USER_MODEL, on_delete=models.CASCADE)
    challenge = models.ForeignKey("curriculum.Challenge", on_delete=models.CASCADE)
    ts = models.DateTimeField(default=timezone.now)
    correct = models.BooleanField()

    class Meta:
        db_table = "flag_attempts"
        indexes = [models.Index(fields=["user", "challenge", "ts"], name="flag_attempts_window")]

    def __str__(self) -> str:
        return f"{self.user} {self.challenge} {self.ts:%Y-%m-%d %H:%M:%S} {self.correct}"
