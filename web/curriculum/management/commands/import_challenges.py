from pathlib import Path

from django.conf import settings
from django.core.management.base import BaseCommand, CommandError

from curriculum.importer import ImportError_, import_challenges


class Command(BaseCommand):
    help = "Upsert tiers, lessons and challenges from challenges.json; disable absent ones."

    def add_arguments(self, parser):
        parser.add_argument("json", type=Path, help="path to challenges.json")
        parser.add_argument(
            "--repo",
            type=Path,
            default=Path(settings.CHALLENGES_REPO),
            help="repo checkout that lesson_path and solution_path are relative to",
        )

    def handle(self, *args, **opts):
        try:
            res = import_challenges(opts["json"], opts["repo"])
        except ImportError_ as e:
            raise CommandError(str(e)) from e
        for state in ("created", "updated", "disabled"):
            for slug in getattr(res, state):
                self.stdout.write(f"  {state:9} {slug}")
        self.stdout.write(self.style.SUCCESS(res.summary()))
