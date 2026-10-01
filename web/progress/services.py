"""Unlock rules (spec "Flag submission rules"; plan 6, "Unlock rules").

Enabled challenges form one sequence: tier by tier, in order within a tier. The first is
unlocked for everyone; solving one unlocks the next, so solving a tier's last unlocks the next
tier's first. Progress rows are created lazily, and an existing `unlocked` row (signup, or an
admin) also unlocks.
"""

from dataclasses import dataclass
from datetime import timedelta

from django.utils import timezone

from curriculum.models import Challenge

from .models import FlagAttempt, Progress

# Flag rate limit (spec "Flag submission rules"): 10 attempts per challenge per 10 minutes.
ATTEMPT_LIMIT = 10
ATTEMPT_WINDOW = timedelta(minutes=10)

LOCKED, UNLOCKED, SOLVED = Progress.State.LOCKED, Progress.State.UNLOCKED, Progress.State.SOLVED


def sequence():
    """Enabled challenges in unlock order."""
    return list(
        Challenge.objects.filter(enabled=True)
        .select_related("tier")
        .order_by("tier__order", "order")
    )


@dataclass
class Item:
    challenge: Challenge
    state: str
    progress: Progress | None


def states(user) -> list[Item]:
    """Every enabled challenge with this user's state, in unlock order."""
    rows = {p.challenge_id: p for p in Progress.objects.filter(user=user)}
    out, prev_solved = [], True
    for c in sequence():
        p = rows.get(c.id)
        if p and p.state == SOLVED:
            st = SOLVED
        elif prev_solved or (p and p.state == UNLOCKED):
            st = UNLOCKED
        else:
            st = LOCKED
        out.append(Item(c, st, p))
        prev_solved = st == SOLVED
    return out


def state_of(user, challenge: Challenge) -> str:
    if not challenge.enabled:
        return LOCKED
    for it in states(user):
        if it.challenge.id == challenge.id:
            return it.state
    return LOCKED


def is_unlocked(user, challenge: Challenge) -> bool:
    return state_of(user, challenge) in (UNLOCKED, SOLVED)


def unlock_first(user) -> Progress | None:
    """At signup: record tier 1's first challenge as unlocked."""
    seq = sequence()
    if not seq:
        return None
    p, _ = Progress.objects.get_or_create(user=user, challenge=seq[0])
    return p


def next_after(challenge: Challenge) -> Challenge | None:
    seq = sequence()
    for i, c in enumerate(seq):
        if c.id == challenge.id:
            return seq[i + 1] if i + 1 < len(seq) else None
    return None


def attempts_in_window(user, challenge: Challenge) -> int:
    since = timezone.now() - ATTEMPT_WINDOW
    return FlagAttempt.objects.filter(user=user, challenge=challenge, ts__gte=since).count()


def lab_panel(user, challenge: Challenge) -> dict:
    """What the challenge and terminal pages show beside the lab: hints so far, the flag form's
    state, and past attempts."""
    p = Progress.objects.filter(user=user, challenge=challenge).first()
    shown = p.hints_used if p else 0
    hints = challenge.hints or []
    return {
        "solved": bool(p and p.state == SOLVED),
        "hints_shown": [{"n": i + 1, **h} for i, h in enumerate(hints[:shown])],
        "next_hint": {"n": shown + 1, **hints[shown]} if shown < len(hints) else None,
        "hints_total": len(hints),
        "attempts": FlagAttempt.objects.filter(user=user, challenge=challenge).order_by("-ts")[:10],
        "attempts_left": max(ATTEMPT_LIMIT - attempts_in_window(user, challenge), 0),
    }
