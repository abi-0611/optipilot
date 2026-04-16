"""
Background scheduler for retraining, recalibration, and drift detection.
"""

from __future__ import annotations

import logging
import time
from collections.abc import Iterable

from apscheduler.schedulers.asyncio import AsyncIOScheduler

from forecaster.config import Config
from forecaster.ml.drift import DriftDetector
from forecaster.ml.inference import InferenceEngine
from forecaster.ml.recalibration import BiasRecalibrator
from forecaster.ml.trainer import InsufficientDataError, Trainer
from forecaster.storage.metrics import MetricsStore
from forecaster.storage.registry import ModelRegistry

_INCREMENTAL_WINDOW_MIN = 120
_FULL_WINDOW_DAYS = 90
_RECALIBRATION_WINDOW_HOURS = 6
_DRIFT_INTERVAL_MIN = 5


class ForecasterScheduler:
    def __init__(
        self,
        config: Config,
        metrics_store: MetricsStore,
        registry: ModelRegistry,
        trainer: Trainer,
        inference: InferenceEngine,
        logger: logging.Logger,
    ) -> None:
        self._cfg = config
        self._metrics = metrics_store
        self._registry = registry
        self._trainer = trainer
        self._inference = inference
        self._log = logger
        self._scheduler = AsyncIOScheduler(timezone="UTC")
        self._registered_services: set[str] = set()
        self._running = False
        self._recalibrator = BiasRecalibrator(metrics_store, registry, logger)
        self._drift = DriftDetector(
            config=config,
            metrics_store=metrics_store,
            registry=registry,
            trainer=trainer,
            inference=inference,
            logger=logger,
        )

    async def start(self) -> None:
        if self._running:
            return
        self._scheduler.start()
        self._running = True
        services = await self._metrics.list_services_with_data()
        self.register_services(services)
        self._log.info(
            "scheduler started",
            extra={"services_registered": len(self._registered_services)},
        )

    async def shutdown(self) -> None:
        if not self._running:
            return
        self._scheduler.shutdown(wait=False)
        self._running = False
        self._log.info("scheduler stopped")

    def register_services(self, service_names: Iterable[str]) -> int:
        count = 0
        for service_name in service_names:
            if self.register_service(service_name):
                count += 1
        return count

    def register_service(self, service_name: str) -> bool:
        service_name = service_name.strip()
        if not service_name or service_name in self._registered_services:
            return False

        self._scheduler.add_job(
            self._run_recalibration_job,
            trigger="interval",
            minutes=self._cfg.training.recalibration_interval_min,
            args=[service_name],
            id=f"recalibration::{service_name}",
            replace_existing=True,
            max_instances=1,
            coalesce=True,
        )
        self._scheduler.add_job(
            self._run_incremental_retrain_job,
            trigger="interval",
            minutes=self._cfg.training.incremental_retrain_interval_min,
            args=[service_name],
            id=f"incremental_retrain::{service_name}",
            replace_existing=True,
            max_instances=1,
            coalesce=True,
        )
        self._scheduler.add_job(
            self._run_full_retrain_job,
            trigger="interval",
            hours=self._cfg.training.full_retrain_interval_hours,
            args=[service_name],
            id=f"full_retrain::{service_name}",
            replace_existing=True,
            max_instances=1,
            coalesce=True,
        )
        self._scheduler.add_job(
            self._run_drift_job,
            trigger="interval",
            minutes=_DRIFT_INTERVAL_MIN,
            args=[service_name],
            id=f"drift::{service_name}",
            replace_existing=True,
            max_instances=1,
            coalesce=True,
        )

        self._registered_services.add(service_name)
        self._log.info("scheduler jobs registered", extra={"service": service_name})
        return True

    async def _run_recalibration_job(self, service_name: str) -> None:
        await self._run_job(
            service_name=service_name,
            job_type="recalibration",
            runner=lambda: self._recalibrator.recalibrate_service(
                service_name, window_hours=_RECALIBRATION_WINDOW_HOURS
            ),
        )

    async def _run_incremental_retrain_job(self, service_name: str) -> None:
        async def _runner() -> dict[str, object]:
            metrics = await self._metrics.get_recent(
                service_name, minutes=_INCREMENTAL_WINDOW_MIN
            )
            min_points = self._cfg.training.min_data_points
            if len(metrics) < min_points:
                return {
                    "outcome": "skipped",
                    "reason": "insufficient_data",
                    "have": len(metrics),
                    "need": min_points,
                }
            result = await self._trainer.train_service(
                service_name,
                metrics,
                use_hyperparameter_search=False,
                training_window_label="incremental_2h",
            )
            if result.promoted:
                self._inference.invalidate_cache(service_name)
            return {
                "outcome": "trained",
                "version": result.version,
                "promoted": result.promoted,
                "mape_p50": result.validation_mape_p50,
                "mape_p90": result.validation_mape_p90,
            }

        await self._run_job(
            service_name=service_name,
            job_type="incremental_retrain",
            runner=_runner,
        )

    async def _run_full_retrain_job(self, service_name: str) -> None:
        async def _runner() -> dict[str, object]:
            minutes = _FULL_WINDOW_DAYS * 24 * 60
            metrics = await self._metrics.get_recent(service_name, minutes=minutes)
            min_points = self._cfg.training.min_data_points
            if len(metrics) < min_points:
                return {
                    "outcome": "skipped",
                    "reason": "insufficient_data",
                    "have": len(metrics),
                    "need": min_points,
                }
            result = await self._trainer.train_service(
                service_name,
                metrics,
                use_hyperparameter_search=True,
                training_window_label="full_90d",
            )
            if result.promoted:
                self._inference.invalidate_cache(service_name)
            return {
                "outcome": "trained",
                "version": result.version,
                "promoted": result.promoted,
                "mape_p50": result.validation_mape_p50,
                "mape_p90": result.validation_mape_p90,
            }

        await self._run_job(
            service_name=service_name,
            job_type="full_retrain",
            runner=_runner,
        )

    async def _run_drift_job(self, service_name: str) -> None:
        await self._run_job(
            service_name=service_name,
            job_type="drift_detection",
            runner=lambda: self._drift.detect_service(service_name),
        )

    async def _run_job(
        self,
        *,
        service_name: str,
        job_type: str,
        runner,
    ) -> None:
        started = time.monotonic()
        try:
            payload = await runner()
            outcome = "ok"
            if isinstance(payload, dict):
                outcome = str(payload.get("outcome", "ok"))
            self._log.info(
                "scheduled job completed",
                extra={
                    "service": service_name,
                    "job_type": job_type,
                    "duration_sec": round(time.monotonic() - started, 3),
                    "outcome": outcome,
                    "details": payload,
                },
            )
        except InsufficientDataError as exc:
            self._log.warning(
                "scheduled job skipped",
                extra={
                    "service": service_name,
                    "job_type": job_type,
                    "duration_sec": round(time.monotonic() - started, 3),
                    "outcome": "insufficient_data",
                    "details": str(exc),
                },
            )
        except Exception as exc:
            # Fail-soft: one service/job failure must not stop scheduler.
            self._log.exception(
                "scheduled job failed",
                extra={
                    "service": service_name,
                    "job_type": job_type,
                    "duration_sec": round(time.monotonic() - started, 3),
                    "outcome": "error",
                },
                exc_info=exc,
            )
