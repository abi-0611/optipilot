"""
ModelRegistry — SQLite-backed store for trained model records and per-service status.

Tables:
  models       — one row per trained model version
  model_status — one row per service (upserted), tracks live serving state
"""

from __future__ import annotations

from datetime import datetime
from typing import Optional

import aiosqlite

from forecaster.models import ModelRecord, ModelStatus

_CREATE_MODELS = """
CREATE TABLE IF NOT EXISTS models (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    service_name          TEXT NOT NULL,
    version               TEXT NOT NULL,
    created_at            TIMESTAMP NOT NULL,
    trained_on_points     INTEGER,
    training_window_start TIMESTAMP,
    training_window_end   TIMESTAMP,
    validation_mape       REAL,
    is_promoted           BOOLEAN DEFAULT 0,
    file_path             TEXT NOT NULL,
    feature_config_hash   TEXT,
    UNIQUE(service_name, version)
);
"""

_CREATE_MODELS_IDX = """
CREATE INDEX IF NOT EXISTS idx_models_service_promoted
    ON models(service_name, is_promoted);
"""

_CREATE_STATUS = """
CREATE TABLE IF NOT EXISTS model_status (
    service_name         TEXT PRIMARY KEY,
    model_version        TEXT,
    current_mape         REAL,
    scaling_mode         TEXT,
    last_trained_at      TIMESTAMP,
    last_recalibrated_at TIMESTAMP,
    training_data_points INTEGER,
    updated_at           TIMESTAMP
);
"""


def _row_to_record(row: aiosqlite.Row) -> ModelRecord:
    return ModelRecord(
        service_name=row["service_name"],
        version=row["version"],
        created_at=datetime.fromisoformat(row["created_at"]),
        trained_on_points=row["trained_on_points"] or 0,
        training_window_start=datetime.fromisoformat(row["training_window_start"])
            if row["training_window_start"] else datetime.utcnow(),
        training_window_end=datetime.fromisoformat(row["training_window_end"])
            if row["training_window_end"] else datetime.utcnow(),
        validation_mape=row["validation_mape"] or 0.0,
        is_promoted=bool(row["is_promoted"]),
        file_path=row["file_path"],
        feature_config_hash=row["feature_config_hash"] or "",
    )


def _row_to_status(row: aiosqlite.Row) -> ModelStatus:
    return ModelStatus(
        service_name=row["service_name"],
        model_version=row["model_version"] or "",
        current_mape=row["current_mape"] or 0.0,
        scaling_mode=row["scaling_mode"] or "REACTIVE",
        last_trained_at=datetime.fromisoformat(row["last_trained_at"])
            if row["last_trained_at"] else datetime.utcnow(),
        last_recalibrated_at=datetime.fromisoformat(row["last_recalibrated_at"])
            if row["last_recalibrated_at"] else datetime.utcnow(),
        training_data_points=row["training_data_points"] or 0,
    )


class ModelRegistry:
    def __init__(self, db_path: str) -> None:
        self._db_path = db_path
        self._db: Optional[aiosqlite.Connection] = None

    async def initialize(self) -> None:
        self._db = await aiosqlite.connect(self._db_path)
        self._db.row_factory = aiosqlite.Row
        await self._db.execute("PRAGMA journal_mode=WAL")
        await self._db.execute(_CREATE_MODELS)
        await self._db.execute(_CREATE_MODELS_IDX)
        await self._db.execute(_CREATE_STATUS)
        await self._db.commit()

    async def register_model(self, record: ModelRecord) -> None:
        assert self._db is not None
        await self._db.execute(
            """
            INSERT INTO models
              (service_name, version, created_at, trained_on_points,
               training_window_start, training_window_end, validation_mape,
               is_promoted, file_path, feature_config_hash)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                record.service_name,
                record.version,
                record.created_at.isoformat(),
                record.trained_on_points,
                record.training_window_start.isoformat(),
                record.training_window_end.isoformat(),
                record.validation_mape,
                int(record.is_promoted),
                record.file_path,
                record.feature_config_hash,
            ),
        )
        await self._db.commit()

    async def promote_model(self, service_name: str, version: str) -> None:
        """Atomically promote one model version; demotes all others for this service."""
        assert self._db is not None
        async with self._db.execute("BEGIN"):
            await self._db.execute(
                "UPDATE models SET is_promoted = 0 WHERE service_name = ?",
                (service_name,),
            )
            await self._db.execute(
                "UPDATE models SET is_promoted = 1 WHERE service_name = ? AND version = ?",
                (service_name, version),
            )
        await self._db.commit()

    async def get_promoted_model(self, service_name: str) -> Optional[ModelRecord]:
        assert self._db is not None
        async with self._db.execute(
            "SELECT * FROM models WHERE service_name = ? AND is_promoted = 1 LIMIT 1",
            (service_name,),
        ) as cursor:
            row = await cursor.fetchone()
            return _row_to_record(row) if row else None

    async def list_models(self, service_name: str) -> list[ModelRecord]:
        assert self._db is not None
        async with self._db.execute(
            "SELECT * FROM models WHERE service_name = ? ORDER BY created_at DESC",
            (service_name,),
        ) as cursor:
            rows = await cursor.fetchall()
            return [_row_to_record(r) for r in rows]

    async def upsert_status(self, status: ModelStatus) -> None:
        assert self._db is not None
        await self._db.execute(
            """
            INSERT INTO model_status
              (service_name, model_version, current_mape, scaling_mode,
               last_trained_at, last_recalibrated_at, training_data_points, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(service_name) DO UPDATE SET
              model_version        = excluded.model_version,
              current_mape         = excluded.current_mape,
              scaling_mode         = excluded.scaling_mode,
              last_trained_at      = excluded.last_trained_at,
              last_recalibrated_at = excluded.last_recalibrated_at,
              training_data_points = excluded.training_data_points,
              updated_at           = excluded.updated_at
            """,
            (
                status.service_name,
                status.model_version,
                status.current_mape,
                status.scaling_mode,
                status.last_trained_at.isoformat(),
                status.last_recalibrated_at.isoformat(),
                status.training_data_points,
                datetime.utcnow().isoformat(),
            ),
        )
        await self._db.commit()

    async def get_status(self, service_name: str) -> Optional[ModelStatus]:
        assert self._db is not None
        async with self._db.execute(
            "SELECT * FROM model_status WHERE service_name = ?",
            (service_name,),
        ) as cursor:
            row = await cursor.fetchone()
            return _row_to_status(row) if row else None

    async def get_all_statuses(self) -> list[ModelStatus]:
        assert self._db is not None
        async with self._db.execute(
            "SELECT * FROM model_status ORDER BY service_name"
        ) as cursor:
            rows = await cursor.fetchall()
            return [_row_to_status(r) for r in rows]

    async def close(self) -> None:
        if self._db is not None:
            await self._db.close()
            self._db = None
