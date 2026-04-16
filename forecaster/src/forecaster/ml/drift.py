"""
Rolling drift detection based on prediction-vs-actual MAPE.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass

import numpy as np

from forecaster.config import Config
from forecaster.ml.recalibration import match_predictions_with_actuals
from forecaster.ml.trainer import InsufficientDataError, Trainer
from forecaster.storage.metrics import MetricsStore
from forecaster.storage.registry import ModelRegistry
from forecaster.ml.inference import InferenceEngine


@dataclass
class DriftResult:
    service_name: str
    rolling_mape_3h: float
    degraded_to: str | None
    emergency_retrain_triggered: bool
    outcome: str


class DriftDetector:
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

    async def detect_service(self, service_name: str) -> DriftResult:
        window_min = self._cfg.drift.rolling_window_hours * 60
        predictions = await self._metrics.get_recent_predictions(
            service_name, minutes=window_min, include_stub=False
        )
        actuals = await self._metrics.get_recent(service_name, minutes=window_min + 15)
        matched = match_predictions_with_actuals(predictions, actuals)
        if not matched:
            await self._registry.update_drift_state(
                service_name=service_name,
                rolling_mape_3h=0.0,
                degraded_to=None,
            )
            return DriftResult(
                service_name=service_name,
                rolling_mape_3h=0.0,
                degraded_to=None,
                emergency_retrain_triggered=False,
                outcome="skipped_no_pairs",
            )

        y_true = np.asarray([pair.actual.rps for pair in matched], dtype=np.float64)
        y_pred = np.asarray(
            [pair.prediction.predicted_rps_p50 for pair in matched], dtype=np.float64
        )
        denom = np.maximum(np.abs(y_true), 1.0)
        rolling_mape = float(np.mean(np.abs(y_true - y_pred) / denom))

        degraded_to: str | None = None
        emergency = False
        if rolling_mape > self._cfg.drift.reactive_mape_threshold:
            degraded_to = "REACTIVE"
            emergency = await self._trigger_emergency_retrain(service_name)
        elif rolling_mape > self._cfg.drift.conservative_mape_threshold:
            degraded_to = "CONSERVATIVE"

        await self._registry.update_drift_state(
            service_name=service_name,
            rolling_mape_3h=rolling_mape,
            degraded_to=degraded_to,
        )

        log_payload = {
            "service": service_name,
            "matched_points": len(matched),
            "rolling_mape_3h": round(rolling_mape, 6),
            "degraded_to": degraded_to,
            "emergency_retrain_triggered": emergency,
        }
        if degraded_to is not None:
            self._log.warning("drift threshold exceeded", extra=log_payload)
        else:
            self._log.info("drift evaluated", extra=log_payload)
        return DriftResult(
            service_name=service_name,
            rolling_mape_3h=rolling_mape,
            degraded_to=degraded_to,
            emergency_retrain_triggered=emergency,
            outcome="ok",
        )

    async def _trigger_emergency_retrain(self, service_name: str) -> bool:
        window_min = max(self._cfg.training.min_data_points * 2, 24 * 60)
        metrics = await self._metrics.get_recent(service_name, minutes=window_min)
        if len(metrics) < self._cfg.training.min_data_points:
            self._log.warning(
                "emergency retrain skipped: insufficient data",
                extra={
                    "service": service_name,
                    "have": len(metrics),
                    "need": self._cfg.training.min_data_points,
                },
            )
            return False

        try:
            result = await self._trainer.train_service(
                service_name,
                metrics,
                use_hyperparameter_search=False,
                training_window_label="emergency",
            )
            if result.promoted:
                self._inference.invalidate_cache(service_name)
            return True
        except InsufficientDataError:
            return False
        except Exception as exc:  # fail-soft per service
            self._log.exception(
                "emergency retrain failed",
                extra={"service": service_name},
                exc_info=exc,
            )
            return False
