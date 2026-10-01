"""Tiers, lessons and challenges, imported from challenges.json (spec "Data model").

web owns these tables. `import_challenges` is their only writer apart from the admin's
enable toggle.
"""

from django.db import models


class Tier(models.Model):
    slug = models.SlugField(unique=True)  # the directory: tier1-c-fundamentals
    number = models.PositiveSmallIntegerField(unique=True)  # 1, 2, 3 ...
    title = models.CharField(max_length=200)
    order = models.PositiveSmallIntegerField()

    class Meta:
        db_table = "tiers"
        ordering = ["order"]

    def __str__(self) -> str:
        return f"Tier {self.number}: {self.title}"


class Lesson(models.Model):
    tier = models.ForeignKey(Tier, on_delete=models.CASCADE, related_name="lessons")
    slug = models.SlugField(unique=True)  # the challenge's slug (plan 6, "Import")
    title = models.CharField(max_length=200)
    body_md = models.TextField()
    order = models.PositiveSmallIntegerField()

    class Meta:
        db_table = "lessons"
        ordering = ["tier__order", "order"]

    def __str__(self) -> str:
        return self.title


class Challenge(models.Model):
    tier = models.ForeignKey(Tier, on_delete=models.CASCADE, related_name="challenges")
    slug = models.SlugField(unique=True)
    title = models.CharField(max_length=200)
    difficulty = models.PositiveSmallIntegerField()
    order = models.PositiveSmallIntegerField()
    image_digest = models.CharField(max_length=300, blank=True)
    limits = models.JSONField(default=dict)
    hints = models.JSONField(default=list)  # [{"cost": 0, "text": "..."}], in reveal order
    tags = models.JSONField(default=list)
    estimated_minutes = models.PositiveSmallIntegerField()
    boss = models.BooleanField(default=False)
    enabled = models.BooleanField(default=True)
    lesson = models.OneToOneField(
        Lesson, null=True, blank=True, on_delete=models.SET_NULL, related_name="challenge"
    )
    solution_md = models.TextField(blank=True)
    source = models.TextField(blank=True)  # src/main.c, shown read-only beside the terminal
    readme_md = models.TextField(blank=True)  # README.md, the lab's description (also in the image)

    class Meta:
        db_table = "challenges"
        ordering = ["tier__order", "order"]

    def __str__(self) -> str:
        return self.slug
