"""Signup unlocks tier 1's first challenge; signup and login are product events."""

from allauth.account.signals import user_logged_in, user_signed_up
from django.dispatch import receiver

from labs.models import record_event
from progress.services import unlock_first


@receiver(user_signed_up)
def on_signup(request, user, **kwargs):
    unlock_first(user)
    record_event("user_signed_up", user_id=user.id)


@receiver(user_logged_in)
def on_login(request, user, **kwargs):
    record_event("user_logged_in", user_id=user.id)
