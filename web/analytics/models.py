"""Rollups of samples and events (spec "Rollup"; Phase 7, task 7.4).

web is their only writer (manage.py rollup), so Django owns them; ADR 0003 is amended to say
so. `dims` is the grouping ({} for the whole host or every lab, {"challenge": slug} per
challenge, {"type": t} for event counts); `dims_key` is its canonical text, so the unique key
works the same on Postgres and SQLite.
"""

from django.db import models


class Rollup(models.Model):
    bucket_ts = models.DateTimeField()
    metric = models.CharField(max_length=100)
    dims = models.JSONField(default=dict)
    dims_key = models.CharField(max_length=300, default="{}")
    count = models.BigIntegerField()
    sum = models.FloatField()
    min = models.FloatField()
    max = models.FloatField()
    p50 = models.FloatField()
    p95 = models.FloatField()

    class Meta:
        abstract = True

    def __str__(self) -> str:
        return f"{self.bucket_ts:%Y-%m-%d %H:%M} {self.metric} {self.dims_key}"


class Rollup1m(Rollup):
    class Meta:
        db_table = "rollups_1m"
        constraints = [
            models.UniqueConstraint(
                fields=["bucket_ts", "metric", "dims_key"], name="rollups_1m_key"
            )
        ]
        indexes = [models.Index(fields=["metric", "bucket_ts"], name="rollups_1m_metric_ts")]


class Rollup1h(Rollup):
    class Meta:
        db_table = "rollups_1h"
        constraints = [
            models.UniqueConstraint(
                fields=["bucket_ts", "metric", "dims_key"], name="rollups_1h_key"
            )
        ]
        indexes = [models.Index(fields=["metric", "bucket_ts"], name="rollups_1h_metric_ts")]
