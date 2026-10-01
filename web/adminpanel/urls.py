from django.urls import path

from . import views

urlpatterns = [
    path("admin/live", views.live, name="admin_live"),
    path("admin/live/kill/<uuid:session_id>", views.kill, name="admin_kill"),
    path("admin/live/drain", views.drain, name="admin_drain"),
    path("admin/live/challenge/<slug:slug>/toggle", views.toggle, name="admin_toggle"),
]
