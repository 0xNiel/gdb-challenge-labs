from datetime import datetime, timedelta

from django.core.management.base import BaseCommand, CommandError

from analytics.rollup import default_window, rollup_hours, rollup_minutes


class Command(BaseCommand):
    help = "Roll samples and events into rollups_1m (--minute), those into rollups_1h (--hour)."

    def add_arguments(self, parser):
        parser.add_argument("--minute", action="store_true", help="minute buckets from raw rows")
        parser.add_argument("--hour", action="store_true", help="hour buckets from minute rows")
        parser.add_argument(
            "--since", help="ISO time; default: 10 minutes ago (--minute), 3 hours ago (--hour)"
        )
        parser.add_argument("--until", help="ISO time; default: now")

    def handle(self, *args, **opts):
        if not (opts["minute"] or opts["hour"]):
            raise CommandError("give --minute, --hour or both")

        def window(span):
            start, end = default_window(span)
            if opts["since"]:
                start = datetime.fromisoformat(opts["since"])
            if opts["until"]:
                end = datetime.fromisoformat(opts["until"])
            return start, end

        if opts["minute"]:
            n = rollup_minutes(*window(timedelta(minutes=10)))
            self.stdout.write(f"rollups_1m: {n} rows written")
        if opts["hour"]:
            n = rollup_hours(*window(timedelta(hours=3)))
            self.stdout.write(f"rollups_1h: {n} rows written")
