"""Load challenges.json into tiers, lessons and challenges (plan 6, "Import").

Upserts by slug, disables challenges that are absent from the file, never deletes anything.
Running it twice changes nothing the second time.
"""

import json
from dataclasses import dataclass, field
from pathlib import Path, PurePosixPath

from django.db import transaction

from .models import Challenge, Lesson, Tier

# Tier themes from the spec's "Curriculum ladder". A tier not listed is titled from its
# directory name (tier4-stripped-binaries → "Stripped binaries").
TIER_TITLES = {
    1: "gdb fundamentals",
    2: "Optimized and partially stripped C",
    3: "Concurrency and signals",
    4: "Fully stripped binaries and core dumps",
    5: "C++ and object-oriented",
    6: "ARM64",
    7: "Go",
    8: "Rust",
}


class ImportError_(Exception):
    """challenges.json or a file it points at is not what the import expects."""


@dataclass
class Result:
    created: list[str] = field(default_factory=list)
    updated: list[str] = field(default_factory=list)
    unchanged: list[str] = field(default_factory=list)
    disabled: list[str] = field(default_factory=list)

    def summary(self) -> str:
        return (
            f"{len(self.created)} created, {len(self.updated)} updated, "
            f"{len(self.unchanged)} unchanged, {len(self.disabled)} disabled"
        )


def _read(repo: Path, rel: str, what: str) -> str:
    path = repo / rel
    if not path.is_file():
        raise ImportError_(f"{what} {rel} not found under {repo}")
    return path.read_text()


def _split_title(md: str, fallback: str) -> tuple[str, str]:
    """A lesson's first line '# Title' becomes its title; the rest is the body."""
    first, _, rest = md.partition("\n")
    if first.startswith("# "):
        return first[2:].strip(), rest.lstrip("\n")
    return fallback, md


def _tier_for(entry: dict) -> tuple[str, str]:
    """Slug and title of the tier, from the lesson path: challenges/<tier dir>/<NN-slug>/."""
    parts = PurePosixPath(entry["lesson_path"]).parts
    if len(parts) < 4 or parts[0] != "challenges":
        raise ImportError_(f"{entry['slug']}: unexpected lesson_path {entry['lesson_path']}")
    slug = parts[1]
    number = entry["tier"]
    name = slug.split("-", 1)[1].replace("-", " ").capitalize() if "-" in slug else slug
    return slug, TIER_TITLES.get(number, name)


def _upsert(model, lookup: dict, values: dict):
    """Create or update; returns (obj, 'created' | 'updated' | 'unchanged')."""
    obj = model.objects.filter(**lookup).first()
    if obj is None:
        return model.objects.create(**lookup, **values), "created"
    changed = [k for k, v in values.items() if getattr(obj, k) != v]
    if not changed:
        return obj, "unchanged"
    for k in changed:
        setattr(obj, k, values[k])
    obj.save(update_fields=changed)
    return obj, "updated"


@transaction.atomic
def import_challenges(json_path: Path, repo: Path) -> Result:
    try:
        doc = json.loads(Path(json_path).read_text())
    except (OSError, ValueError) as e:
        raise ImportError_(f"cannot read {json_path}: {e}") from e
    if doc.get("version") != 1 or not isinstance(doc.get("challenges"), list):
        raise ImportError_(f"{json_path}: expected version 1 with a challenges list")

    res = Result()
    seen: set[str] = set()
    for entry in doc["challenges"]:
        slug = entry["slug"]
        seen.add(slug)
        tier_slug, tier_title = _tier_for(entry)
        tier, _ = _upsert(
            Tier,
            {"number": entry["tier"]},
            {"slug": tier_slug, "title": tier_title, "order": entry["tier"]},
        )
        title, body = _split_title(_read(repo, entry["lesson_path"], "lesson"), entry["title"])
        lesson, lstate = _upsert(
            Lesson,
            {"slug": slug},
            {"tier": tier, "title": title, "body_md": body, "order": entry["order"]},
        )
        src = PurePosixPath(entry["lesson_path"]).parent / "src" / "main.c"
        source = (repo / src).read_text() if (repo / src).is_file() else ""
        _, cstate = _upsert(
            Challenge,
            {"slug": slug},
            {
                "tier": tier,
                "title": entry["title"],
                "difficulty": entry["difficulty"],
                "order": entry["order"],
                "image_digest": entry.get("image", ""),
                "limits": entry.get("limits", {}),
                "hints": entry.get("hints", []),
                "tags": entry.get("tags", []),
                "estimated_minutes": entry["estimated_minutes"],
                "boss": entry.get("boss", False),
                "enabled": entry.get("enabled", True),
                "lesson": lesson,
                "solution_md": _read(repo, entry["solution_path"], "solution"),
                "source": source,
            },
        )
        state = cstate if cstate != "unchanged" else lstate
        getattr(res, state).append(slug)

    for ch in Challenge.objects.exclude(slug__in=seen).filter(enabled=True):
        ch.enabled = False
        ch.save(update_fields=["enabled"])
        res.disabled.append(ch.slug)
    return res
