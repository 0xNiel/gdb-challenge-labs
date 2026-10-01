from django.urls import path

from . import views

urlpatterns = [
    path("<slug:slug>", views.challenge, name="challenge"),
    path("<slug:slug>/start", views.start, name="lab_start"),
    path("<slug:slug>/stop", views.stop, name="lab_stop"),
    path("<slug:slug>/session", views.session_page, name="lab_session"),
    path("<slug:slug>/session/token", views.session_token, name="lab_token"),
]
