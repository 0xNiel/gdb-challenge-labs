"""Production. Every secret is required; missing ones fail at import time.

Completed in Phase 8 (Caddy, TLS, static files, email provider)."""

from .base import *  # noqa: F403
from .base import database_from_url, env

DEBUG = False
SECRET_KEY = env("DJANGO_SECRET_KEY")
SITE_HOST = env("SITE_HOST")
ALLOWED_HOSTS = [SITE_HOST]
CSRF_TRUSTED_ORIGINS = [f"https://{SITE_HOST}"]

DATABASES = {"default": database_from_url(env("DATABASE_URL"))}

DEPLOY_SECRET = env("DEPLOY_SECRET")
LABD_INTERNAL_SECRET = env("LABD_INTERNAL_SECRET")
WS_TOKEN_KEY = env("WS_TOKEN_KEY")

ACCOUNT_EMAIL_VERIFICATION = "mandatory"
DEFAULT_FROM_EMAIL = env("DEFAULT_FROM_EMAIL", f"noreply@{SITE_HOST}")

SECURE_PROXY_SSL_HEADER = ("HTTP_X_FORWARDED_PROTO", "https")
SESSION_COOKIE_SECURE = True
CSRF_COOKIE_SECURE = True
SECURE_HSTS_SECONDS = 60 * 60 * 24 * 30
SECURE_CONTENT_TYPE_NOSNIFF = True
