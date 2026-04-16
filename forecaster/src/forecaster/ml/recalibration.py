"""
Bias recalibration for quantile predictions.

Computes median residuals (actual - predicted) over a recent time window and
stores them as bias offsets in model_status. Inference applies these offsets.
"""

from __future__ import annotations

import logging
from bisect import bisect_left
from dataclasses import dataclass
from datetime import datetime, timedelta

import numpy as np

from forecaster.models import ServiceMetric, StoredPrediction
from forecaster.storage.metrics import MetricsStore
from forecaster.storage.registry import ModelRegistry

_MATCH_TOLERANCE_MIN = 3


@dataclass
class MatchedResidual:
    prediction: StoredPrediction
    actual: ServiceMetric


def match_predictions_with_actuals(
    predictions: list[StoredPrediction],
    actuals: list[ServiceMetric],
    *,
    tolerance_minutes: int = _MATCH_TOLERANCE_MIN,
) -> list[MatchedResidual]:
    """Match each prediction to the nearest actual metric at horizon time."""
    if not predictions or not actuals:
        return []

    tolerance = timedelta(minutes=tolerance_minutes)
    actual_times = [m.timestamp for m in actuals]
    matched: list[MatchedResidual] = []

    for prediction in predictions:
        target = prediction.predicted_at + timedelta(minutes=prediction.horizon_min)
        idx = bisect_left(actual_times, target)

        candidates: list[ServiceMetric] = []
        if idx < len(actuals):
            candidates.append(actuals[idx])
        if idx > 0:
            candidates.append(actuals[idx - 1])
        if not candidates:
            continue

        best = min(candidates, key=lambda m: abs(m.timestamp - target))
        if abs(best.timestamp - target) <= tolerance:
            matched.append(MatchedResidual(prediction=prediction, actual=best))

    return matched


class BiasRecalibrator:
    def __init__(
        self,
        metrics_store: MetricsStore,
        registry: ModelRegistry,
        logger: logging.Logger,
    ) -> None:
        self._metrics = metrics_store
        self._registry = registry
        self._log = logger

    async def recalibrate_service(
        self, service_name: str, *, window_hours: int = 6
    ) -> dict[str, float | int | str]:
        minutes = window_hours * 60
        predictions = await self._metrics.get_recent_predictions(
            service_name, minutes=minutes, include_stub=False
        )
        actuals = await self._metrics.get_recent(service_name, minutes=minutes + 15)
        matched = match_predictions_with_actuals(predictions, actuals)
        if not matched:
            return {"outcome": "skipped", "matched_points": 0}

        residuals_p50 = np.asarray(
            [m.actual.rps - m.prediction.predicted_rps_p50 for m in matched],
            dtype=np.float64,
        )
        residuals_p90 = np.asarray(
            [m.actual.rps - m.prediction.predicted_rps_p90 for m in matched],
            dtype=np.float64,
        )

        bias_p50 = float(np.median(residuals_p50))
        bias_p90 = float(np.median(residuals_p90))
        await self._registry.update_bias_offsets(
            service_name=service_name,
            bias_offset_p50=bias_p50,
            bias_offset_p90=bias_p90,
            recalibrated_at=datetime.utcnow(),
        )

        self._log.info(
            "bias recalibrated",
            extra={
                "service": service_name,
                "matched_points": len(matched),
                "bias_offset_p50": round(bias_p50, 6),
                "bias_offset_p90": round(bias_p90, 6),
            },
        )
        return {
            "outcome": "updated",
            "matched_points": len(matched),
            "bias_offset_p50": bias_p50,
            "bias_offset_p90": bias_p90,
        }
