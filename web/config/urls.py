from allauth.account import views as account
from django.contrib import admin
from django.urls import include, path

from config.views import healthz, landing

urlpatterns = [
    path("", landing, name="landing"),
    path("healthz", healthz, name="healthz"),
    # The spec's /login and /signup; allauth's other pages stay under /accounts/.
    path("login", account.login, name="login"),
    path("signup", account.signup, name="signup"),
    path("accounts/", include("allauth.urls")),
    path("", include("curriculum.urls")),
    path("lab/", include("labs.urls")),
    path("", include("progress.urls")),
    # Staff pages under /admin/, before Django admin's own catch-all (Phase 7).
    path("", include("analytics.urls")),
    path("", include("adminpanel.urls")),
    path("admin/", admin.site.urls),
]
