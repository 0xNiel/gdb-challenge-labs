from django.urls import path

from . import views

urlpatterns = [
    path("dashboard", views.dashboard, name="dashboard"),
    path("lab/<slug:slug>/flag", views.flag_submit, name="flag_submit"),
    path("lab/<slug:slug>/hint", views.hint_reveal, name="hint_reveal"),
]
