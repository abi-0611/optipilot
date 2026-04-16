"""
InferenceEngine — lazy model cache + prediction logic.

Lifecycle:
  1. get_model() checks the registry for the currently promoted version.
  2. If the cache is cold or stale, load .pkl files from disk via asyncio.to_thread.
  3. predict() builds a feature row from recent metrics and calls both boosters.
  4. invalidate_cache() is called by TriggerRetrain after a new promotion so
     the next GetPrediction reloads from disk without a server restart.

The asyncio.Lock in get_model() ensures only one coroutine loads a given
service's model files concurrently; other requests queue behind the lock.
"""

from __future__ import annotations

import asyncio
import logging
import math
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import lightgbm as lgb

from forecaster.config import Config
from forecaster.ml.features import FEATURE_CONFIG_HASH, build_features
from forecaster.ml.storage import load_model, model_path
from forecaster.models import PredictionResult, ServiceMetric
from forecaster.storage.registry import ModelRegistry


@dataclass
class CachedModel:
    service_name: str
    version: str
    p50_booster: lgb.Booster
    p90_booster: lgb.Booster
    feature_config_hash: str
    loaded_at: datetime


class InferenceEngine:
    def __init__(
        self,
        registry: ModelRegistry,
        models_dir: Path,
        config: Config,
        logger: logging.Logger,
    ) -> None:
        self._registry = registry
        self._models_dir = Path(models_dir)
        self._config = config
        self._log = logger
        self._cache: dict[str, CachedModel] = {}
        # One lock per engine: serialises loads but not predictions (reads are lock-free).
        self._load_lock = asyncio.Lock()

    async def get_model(self, service_name: str) -> Optional[CachedModel]:
        """
        Return the in-memory CachedModel for service_name, loading from disk
        if the cache is empty or the promoted version changed.
        Returns None when no promoted model is registered.
        """
        async with self._load_lock:
            record = await self._registry.get_promoted_model(service_name)
            if record is None:
                return None

            cached = self._cache.get(service_name)
            if cached is not None and cached.version == record.version:
                return cached

            # Either first load or a new version was promoted — reload from disk.
            p50_path = model_path(self._models_dir, service_name, record.version, "p50")
            p90_path = model_path(self._models_dir, service_name, record.version, "p90")

            p50_booster = await asyncio.to_thread(load_model, p50_path)
            p90_booster = await asyncio.to_thread(load_model, p90_path)

            cached = CachedModel(
                service_name=service_name,
                version=record.version,
                p50_booster=p50_booster,
                p90_booster=p90_booster,
                feature_config_hash=record.feature_config_hash,
                loaded_at=datetime.now(tz=timezone.utc),
            )
            self._cache[service_name] = cached
            self._log.info(
                "model loaded",
                extra={"service": service_name, "version": record.version},
            )
            return cached

    async def predict(
        self,
        service_name: str,
        recent_metrics: list[ServiceMetric],
    ) -> Optional[PredictionResult]:
        """
        Produce a PredictionResult for service_name.
        Returns None if no promoted model exists, data is insufficient, or
        the on-disk model was trained with a different feature set.
        """
        model = await self.get_model(service_name)
        if model is None:
            return None

        # Reject models trained with a different feature schema.
        if model.feature_config_hash != FEATURE_CONFIG_HASH:
            self._log.warning(
                "feature hash mismatch — skipping inference",
                extra={
                    "service": service_name,
                    "model_hash": model.feature_config_hash,
                    "current_hash": FEATURE_CONFIG_HASH,
                },
            )
            return None

        status = await self._registry.get_status(service_name)
        bias_p50 = status.bias_offset_p50 if status else 0.0
        bias_p90 = status.bias_offset_p90 if status else 0.0
        degraded_override = status.degraded_to if status else None

        # Feature engineering runs in a thread pool (CPU-bound).
        X, _, _ = await asyncio.to_thread(build_features, recent_metrics, 5)
        if len(X) == 0:
            return None

        # Predict on the single most-recent row.
        last_row = X.iloc[-1:].values

        p50_arr = await asyncio.to_thread(
            model.p50_booster.predict, last_row,
            num_iteration=model.p50_booster.best_iteration
        )
        p90_arr = await asyncio.to_thread(
            model.p90_booster.predict, last_row,
            num_iteration=model.p90_booster.best_iteration
        )

        p50 = max(0.0, float(p50_arr[0]) + float(bias_p50))
        p90 = max(0.0, float(p90_arr[0]) + float(bias_p90))

        # Enforce monotonicity — quantile models can occasionally violate this.
        p90 = max(p90, p50)

        # Replica recommendation: p90 demand with headroom, capped to natural int.
        capacity = self._config.inference.default_capacity_per_replica
        headroom = self._config.inference.default_headroom_factor
        recommended = max(2, math.ceil((p90 / capacity) * (1.0 + headroom)))

        # Scaling mode from prediction-interval width (proxy for model confidence).
        # A narrow interval means both quantiles agree → predictive mode is safe.
        interval_width = (p90 - p50) / max(p50, 1.0)
        if interval_width < 0.40:
            mode = "PREDICTIVE"
            confidence = 0.85
            reason = f"high confidence (interval_width={interval_width:.2f})"
        elif interval_width < 0.60:
            mode = "CONSERVATIVE"
            confidence = 0.60
            reason = f"moderate confidence (interval_width={interval_width:.2f})"
        else:
            mode = "REACTIVE"
            confidence = 0.30
            reason = (
                f"low confidence (interval_width={interval_width:.2f}), "
                "deferring to reactive"
            )

        if degraded_override in {"CONSERVATIVE", "REACTIVE"}:
            mode = degraded_override
            confidence = min(confidence, 0.5 if degraded_override == "CONSERVATIVE" else 0.25)
            reason = f"{reason}; overridden by drift detector ({degraded_override})"

        return PredictionResult(
            service_name=service_name,
            rps_p50=p50,
            rps_p90=p90,
            recommended_replicas=recommended,
            scaling_mode=mode,
            confidence_score=confidence,
            reason=reason,
            model_version=model.version,
        )

    def invalidate_cache(self, service_name: str) -> None:
        """Drop the cached model for a service. Next predict() will reload from disk."""
        self._cache.pop(service_name, None)
