from django.core.management.base import BaseCommand

from analytics.retention import KEEP, apply


class Command(BaseCommand):
    help = "Delete samples older than 7 days, events and rollups_1m older than 90 days."

    def handle(self, *args, **opts):
        for table, n in apply().items():
            self.stdout.write(f"{table}: {n} rows older than {KEEP[table].days} days deleted")
