from django.urls import path

from . import views

urlpatterns = [
    path("", views.learn, name="learn"),
    path("/<slug:tier>/<slug:slug>", views.lesson, name="lesson"),
]
