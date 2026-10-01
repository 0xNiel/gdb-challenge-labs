from django.urls import path

from . import views

urlpatterns = [
    path("admin/analytics/live", views.live, name="analytics_live"),
    path("admin/analytics/usage", views.usage, name="analytics_usage"),
    path("admin/analytics/learning", views.learning, name="analytics_learning"),
    path("admin/analytics/capacity", views.capacity, name="analytics_capacity"),
]
