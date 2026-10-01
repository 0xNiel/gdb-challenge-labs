from django.urls import path

from . import views

urlpatterns = [
    path("learn", views.learn, name="learn"),
    path("learn/<slug:tier>/<slug:slug>", views.lesson, name="lesson"),
]
