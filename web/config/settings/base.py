"""Settings shared by every environment. Secrets come only from the environment.

See deploy/env.example for every variable. No third-party env library: a small helper
below parses DATABASE_URL (docs/CONVENTIONS.md).
"""

import os
from pathlib import Path
from urllib.parse import unquote, urlparse

BASE_DIR = Path(__file__).resolve().parent.parent.parent


def env(name: str, default: str | None = None) -> str:
    """Return an environment variable, or raise if it is unset and has no default."""
    value = os.environ.get(name, default)
    if value is None:
        raise RuntimeError(f"environment variable {name} is required")
    return value


def database_from_url(url: str) -> dict:
    """Translate postgres://user:pass@host:port/name or sqlite:///path into DATABASES['default']."""
    parsed = urlparse(url)
    if parsed.scheme == "sqlite":
        name = parsed.path[1:] if parsed.path.startswith("/") else parsed.path
        return {"ENGINE": "django.db.backends.sqlite3", "NAME": name or ":memory:"}
    if parsed.scheme in ("postgres", "postgresql"):
        return {
            "ENGINE": "django.db.backends.postgresql",
            "NAME": parsed.path.lstrip("/"),
            "USER": unquote(parsed.username or ""),
            "PASSWORD": unquote(parsed.password or ""),
            "HOST": parsed.hostname or "",
            "PORT": str(parsed.port or ""),
        }
    raise ValueError(f"unsupported DATABASE_URL scheme: {parsed.scheme!r}")


INSTALLED_APPS = [
    "django.contrib.admin",
    "django.contrib.auth",
    "django.contrib.contenttypes",
    "django.contrib.sessions",
    "django.contrib.messages",
    "django.contrib.staticfiles",
    "allauth",
    "allauth.account",
    "accounts",
    "curriculum",
    "labs",
    "progress",
    "analytics",
    "adminpanel",
]

MIDDLEWARE = [
    "django.middleware.security.SecurityMiddleware",
    "django.contrib.sessions.middleware.SessionMiddleware",
    "django.middleware.common.CommonMiddleware",
    "django.middleware.csrf.CsrfViewMiddleware",
    "django.contrib.auth.middleware.AuthenticationMiddleware",
    "django.contrib.messages.middleware.MessageMiddleware",
    "django.middleware.clickjacking.XFrameOptionsMiddleware",
    "allauth.account.middleware.AccountMiddleware",
]

ROOT_URLCONF = "config.urls"
WSGI_APPLICATION = "config.wsgi.application"

TEMPLATES = [
    {
        "BACKEND": "django.template.backends.django.DjangoTemplates",
        "DIRS": [BASE_DIR / "templates"],
        "APP_DIRS": True,
        "OPTIONS": {
            "context_processors": [
                "django.template.context_processors.request",
                "django.contrib.auth.context_processors.auth",
                "django.contrib.messages.context_processors.messages",
            ],
        },
    },
]

AUTH_PASSWORD_VALIDATORS = [
    {"NAME": "django.contrib.auth.password_validation.UserAttributeSimilarityValidator"},
    {"NAME": "django.contrib.auth.password_validation.MinimumLengthValidator"},
    {"NAME": "django.contrib.auth.password_validation.CommonPasswordValidator"},
    {"NAME": "django.contrib.auth.password_validation.NumericPasswordValidator"},
]

LANGUAGE_CODE = "en-us"
TIME_ZONE = "UTC"
USE_I18N = True
USE_TZ = True

STATIC_URL = "static/"
STATIC_ROOT = BASE_DIR / "staticfiles"
STATICFILES_DIRS = [BASE_DIR / "static"]

DEFAULT_AUTO_FIELD = "django.db.models.BigAutoField"

# Shared with labd and the challenge build (ADR 0005). Overridden per environment.
DEPLOY_SECRET = os.environ.get("DEPLOY_SECRET", "")
LABD_INTERNAL_URL = os.environ.get("LABD_INTERNAL_URL", "http://127.0.0.1:8081")
LABD_INTERNAL_SECRET = os.environ.get("LABD_INTERNAL_SECRET", "")
WS_TOKEN_KEY = os.environ.get("WS_TOKEN_KEY", "")
SITE_HOST = os.environ.get("SITE_HOST", "localhost")

# Where the browser opens the terminal WebSocket: ws(s)://host, without /ws/term. Empty means
# the page's own host, which is what production has (Caddy routes /ws/term to labd).
LABD_WS_BASE = os.environ.get("LABD_WS_BASE", "")
# The repo checkout that challenges.json's lesson_path and solution_path are relative to.
CHALLENGES_REPO = os.environ.get("CHALLENGES_REPO", str(BASE_DIR.parent))

# Accounts: django-allauth, email only, no social providers (QUESTIONS Q4).
AUTHENTICATION_BACKENDS = [
    "django.contrib.auth.backends.ModelBackend",
    "allauth.account.auth_backends.AuthenticationBackend",
]
ACCOUNT_LOGIN_METHODS = {"email"}
ACCOUNT_SIGNUP_FIELDS = ["email*", "password1*", "password2*"]
ACCOUNT_UNIQUE_EMAIL = True
ACCOUNT_EMAIL_VERIFICATION = "optional"  # prod: mandatory
ACCOUNT_LOGOUT_ON_GET = False
LOGIN_URL = "/login"
LOGIN_REDIRECT_URL = "/learn"
ACCOUNT_LOGOUT_REDIRECT_URL = "/"
ACCOUNT_SIGNUP_REDIRECT_URL = "/learn"

# The labd client class; tests use labs.fake_labd.FakeLabd.
LABD_CLIENT = "labs.labd_client.LabdClient"
