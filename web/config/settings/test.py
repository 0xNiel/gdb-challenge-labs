"""Test settings: in-memory SQLite and fixed fake secrets. Postgres-specific tests opt in
by setting DATABASE_URL (used by the integration and e2e runs in the VM)."""

from .base import *  # noqa: F403
from .base import database_from_url, env

DEBUG = False
SECRET_KEY = "test-only-key"
ALLOWED_HOSTS = ["testserver", "localhost"]

DATABASES = {"default": database_from_url(env("DATABASE_URL", "sqlite:///:memory:"))}

DEPLOY_SECRET = "test-secret-do-not-use"  # matches challenges/schema/flag_vectors.json
LABD_INTERNAL_SECRET = "test-internal-secret"
WS_TOKEN_KEY = "test-ws-token-key"
PASSWORD_HASHERS = ["django.contrib.auth.hashers.MD5PasswordHasher"]  # fast tests only
EMAIL_BACKEND = "django.core.mail.backends.locmem.EmailBackend"
