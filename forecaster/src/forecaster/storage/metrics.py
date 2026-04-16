"""
MetricsStore — SQLite-backed store for raw service metrics (training data).

Metrics are ingested via IngestMetrics RPC and queried for model training
and history inspection. Old data is purged by retention policy.
"""

from __future__ import annotations

from datetime import datetime, timedelta
from typing import Optional

import aiosqlite

from forecaster.models import ServiceMetric

_CREATE_METRICS = """
CREATE TABLE IF NOT EXISTS metrics (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    service_name       TEXT NOT NULL,
    rps                REAL,
    avg_latency_ms     REAL,
    p99_latency_ms     REAL,
    active_connections INTEGER,
    cpu_usage_percent  REAL,
    memory_usage_mb    REAL,
    error_rate         REAL,
    timestamp          TIMESTAMP NOT NULL
);
"""

_CREATE_METRICS_IDX = """
CREATE INDEX IF NOT EXISTS idx_metrics_service_time
    ON metrics(service_name, timestamp);
"""


def _row_to_metric(row: aiosqlite.Row) -> ServiceMetric:
    return ServiceMetric(
        service_name=row["service_name"],
        rps=row["rps"] or 0.0,
        avg_latency_ms=row["avg_latency_ms"] or 0.0,
        p99_latency_ms=row["p99_latency_ms"] or 0.0,
        active_connections=row["active_connections"] or 0,
        cpu_usage_percent=row["cpu_usage_percent"] or 0.0,
        memory_usage_mb=row["memory_usage_mb"] or 0.0,
        error_rate=row["error_rate"] or 0.0,
        timestamp=datetime.fromisoformat(row["timestamp"]),
    )


class MetricsStore:
    def __init__(self, db_path: str) -> None:
        self._db_path = db_path
        self._db: Optional[aiosqlite.Connection] = None

    async def initialize(self) -> None:
        self._db = await aiosqlite.connect(self._db_path)
        self._db.row_factory = aiosqlite.Row
        await self._db.execute("PRAGMA journal_mode=WAL")
        await self._db.execute(_CREATE_METRICS)
        await self._db.execute(_CREATE_METRICS_IDX)
        await self._db.commit()

    async def insert_batch(self, metrics: list[ServiceMetric]) -> int:
        """Insert a list of metrics in a single transaction. Returns count inserted."""
        assert self._db is not None
        if not metrics:
            return 0

        rows = [
            (
                m.service_name,
                m.rps,
                m.avg_latency_ms,
                m.p99_latency_ms,
                m.active_connections,
                m.cpu_usage_percent,
                m.memory_usage_mb,
                m.error_rate,
                m.timestamp.isoformat(),
            )
            for m in metrics
        ]

        await self._db.executemany(
            """
            INSERT INTO metrics
              (service_name, rps, avg_latency_ms, p99_latency_ms,
               active_connections, cpu_usage_percent, memory_usage_mb,
               error_rate, timestamp)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            rows,
        )
        await self._db.commit()
        return len(rows)

    async def get_recent(self, service_name: str, minutes: int) -> list[ServiceMetric]:
        """Return metrics for the given service from the last `minutes` minutes."""
        assert self._db is not None
        cutoff = (datetime.utcnow() - timedelta(minutes=minutes)).isoformat()
        async with self._db.execute(
            """
            SELECT * FROM metrics
            WHERE service_name = ? AND timestamp >= ?
            ORDER BY timestamp ASC
            """,
            (service_name, cutoff),
        ) as cursor:
            rows = await cursor.fetchall()
            return [_row_to_metric(r) for r in rows]

    async def get_range(
        self, service_name: str, start: datetime, end: datetime
    ) -> list[ServiceMetric]:
        """Return metrics for the given service within [start, end]."""
        assert self._db is not None
        async with self._db.execute(
            """
            SELECT * FROM metrics
            WHERE service_name = ? AND timestamp >= ? AND timestamp <= ?
            ORDER BY timestamp ASC
            """,
            (service_name, start.isoformat(), end.isoformat()),
        ) as cursor:
            rows = await cursor.fetchall()
            return [_row_to_metric(r) for r in rows]

    async def get_count(self, service_name: str) -> int:
        """Return total number of stored metrics for a service."""
        assert self._db is not None
        async with self._db.execute(
            "SELECT COUNT(*) FROM metrics WHERE service_name = ?",
            (service_name,),
        ) as cursor:
            row = await cursor.fetchone()
            return row[0] if row else 0

    async def delete_services(self, service_names: list[str]) -> int:
        """Delete all metrics rows for the given services. Returns rows deleted."""
        assert self._db is not None
        if not service_names:
            return 0
        placeholders = ",".join("?" for _ in service_names)
        async with self._db.execute(
            f"DELETE FROM metrics WHERE service_name IN ({placeholders})",
            tuple(service_names),
        ) as cursor:
            deleted = cursor.rowcount
        await self._db.commit()
        return deleted

    async def purge_older_than(self, days: int) -> int:
        """Delete metrics older than `days` days. Returns number of rows deleted."""
        assert self._db is not None
        cutoff = (datetime.utcnow() - timedelta(days=days)).isoformat()
        async with self._db.execute(
            "DELETE FROM metrics WHERE timestamp < ?", (cutoff,)
        ) as cursor:
            deleted = cursor.rowcount
        await self._db.commit()
        return deleted

    async def close(self) -> None:
        if self._db is not None:
            await self._db.close()
            self._db = None
