"""Local development: SQLite by default, Postgres when DATABASE_URL is set."""

from .base import *  # noqa: F403
from .base import BASE_DIR, database_from_url, env

DEBUG = True
SECRET_KEY = env("DJANGO_SECRET_KEY", "dev-only-insecure-key")
ALLOWED_HOSTS = ["localhost", "127.0.0.1", "[::1]"]

DATABASES = {
    "default": database_from_url(env("DATABASE_URL", f"sqlite:///{BASE_DIR / 'db.sqlite3'}"))
}

DEPLOY_SECRET = env("DEPLOY_SECRET", "dev-deploy-secret")
LABD_INTERNAL_SECRET = env("LABD_INTERNAL_SECRET", "dev-internal-secret")
WS_TOKEN_KEY = env("WS_TOKEN_KEY", "dev-ws-token-key")
EMAIL_BACKEND = "django.core.mail.backends.console.EmailBackend"
