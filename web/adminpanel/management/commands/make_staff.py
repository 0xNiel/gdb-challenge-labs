from django.contrib.auth import get_user_model
from django.core.management.base import BaseCommand, CommandError


class Command(BaseCommand):
    help = "Make an existing account staff, so it can open /admin/live and /admin/analytics."

    def add_arguments(self, parser):
        parser.add_argument("email")
        parser.add_argument("--superuser", action="store_true", help="also Django admin superuser")
        parser.add_argument("--revoke", action="store_true", help="take staff away instead")

    def handle(self, *args, **opts):
        users = get_user_model().objects.filter(email__iexact=opts["email"])
        if users.count() != 1:
            raise CommandError(
                f"{users.count()} accounts with email {opts['email']}; sign up first"
            )
        u = users.get()
        u.is_staff = not opts["revoke"]
        u.is_superuser = opts["superuser"] and not opts["revoke"]
        u.save(update_fields=["is_staff", "is_superuser"])
        state = (
            "no longer staff"
            if opts["revoke"]
            else "staff" + (" and superuser" if u.is_superuser else "")
        )
        self.stdout.write(f"{u.email} is {state}")
