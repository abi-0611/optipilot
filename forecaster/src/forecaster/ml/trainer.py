"""
LightGBM quantile trainer.

One `train_service(name, metrics)` call does the whole pipeline:

  check data count  ->  build features  ->  time-ordered split
  ->  train p50 + p90 quantile models  ->  evaluate MAPE on holdout
  ->  auto-increment version  ->  save both .pkl files
  ->  register in ModelRegistry  ->  decide promotion  ->  upsert ModelStatus

CPU-bound work (feature engineering, LightGBM training) runs inside
`asyncio.to_thread` so the gRPC event loop stays responsive during training.

Promotion policy:
  - No current promoted model -> promote the new one (bootstraps serving).
  - Else: promote only if new p50 MAPE is within (1 + tolerance) of the
    current MAPE AND below the absolute acceptance threshold. This
    prevents churning models on noise while still rejecting regressions.
"""

from __future__ import annotations

import asyncio
import logging
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import lightgbm as lgb
import numpy as np
import pandas as pd

from forecaster.config import Config
from forecaster.ml.features import (
    FEATURE_CONFIG_HASH,
    FEATURE_NAMES,
    build_features,
    compute_mape,
)
from forecaster.ml.storage import model_path, save_model
from forecaster.models import ModelRecord, ModelStatus, ServiceMetric
from forecaster.storage.registry import ModelRegistry


# LightGBM hyperparameters — modest, not heavily tuned.
_LGB_BASE_PARAMS: dict[str, Any] = {
    "objective": "quantile",
    "metric": "quantile",
    "num_leaves": 31,
    "learning_rate": 0.05,
    "min_data_in_leaf": 20,
    "feature_fraction": 0.9,
    "bagging_fraction": 0.8,
    "bagging_freq": 5,
    "verbose": -1,
}

_NUM_BOOST_ROUND = 200
_EARLY_STOPPING_ROUNDS = 20
_FORECAST_HORIZON_MIN = 5  # primary horizon — predict RPS 5 minutes ahead


@dataclass
class TrainingResult:
    service_name: str
    version: str
    p50_model_path: str
    p90_model_path: str
    validation_mape_p50: float
    validation_mape_p90: float
    training_window_start: datetime
    training_window_end: datetime
    trained_on_points: int
    feature_config_hash: str
    promoted: bool
    previous_mape: float  # MAPE of the model that was serving before (0.0 if none)


class InsufficientDataError(ValueError):
    """Raised when there isn't enough data to train a meaningful model."""


class Trainer:
    def __init__(
        self,
        config: Config,
        registry: ModelRegistry,
        models_dir: Path,
        logger: logging.Logger,
    ) -> None:
        self._config = config
        self._registry = registry
        self._models_dir = Path(models_dir)
        self._log = logger

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    async def train_service(
        self, service_name: str, metrics: list[ServiceMetric]
    ) -> TrainingResult:
        start_wall = time.monotonic()

        # Fast-fail: need enough raw points before we bother building features.
        min_points = self._config.training.min_data_points
        if len(metrics) < min_points:
            raise InsufficientDataError(
                f"have {len(metrics)} raw points, need at least {min_points}"
            )

        # CPU-bound section: feature engineering + LightGBM training.
        training_bundle = await asyncio.to_thread(self._train_sync, metrics)

        # ----- Async section: version, persist, register, promote -----
        version = await self._next_version(service_name)

        p50_path = model_path(self._models_dir, service_name, version, "p50")
        p90_path = model_path(self._models_dir, service_name, version, "p90")

        await asyncio.to_thread(save_model, training_bundle["booster_p50"], p50_path)
        await asyncio.to_thread(save_model, training_bundle["booster_p90"], p90_path)

        now = datetime.now(tz=timezone.utc)

        record = ModelRecord(
            service_name=service_name,
            version=version,
            created_at=now,
            trained_on_points=training_bundle["trained_on_points"],
            training_window_start=training_bundle["training_window_start"],
            training_window_end=training_bundle["training_window_end"],
            # ModelRecord stores a single MAPE — use p50 since it's the primary
            # point forecast. p90 MAPE is logged separately below.
            validation_mape=training_bundle["mape_p50"],
            is_promoted=False,
            # file_path points at p50; the sibling p90 file is alongside it.
            file_path=str(p50_path),
            feature_config_hash=FEATURE_CONFIG_HASH,
        )
        await self._registry.register_model(record)

        # Promotion decision
        current = await self._registry.get_promoted_model(service_name)
        previous_mape = current.validation_mape if current else 0.0
        promote = self._should_promote(
            new_mape=training_bundle["mape_p50"],
            current=current,
        )

        if promote:
            await self._registry.promote_model(service_name, version)

        # Update per-service status regardless of promotion outcome so ops can
        # see the latest training result.
        served_mape = training_bundle["mape_p50"] if promote else previous_mape
        served_version = version if promote else (current.version if current else version)
        status = ModelStatus(
            service_name=service_name,
            model_version=served_version,
            current_mape=served_mape,
            # Scaling mode stays REACTIVE here — the controller flips it
            # once the model is good enough. Trainer does not decide mode.
            scaling_mode="REACTIVE",
            last_trained_at=now,
            last_recalibrated_at=now,
            training_data_points=training_bundle["trained_on_points"],
        )
        await self._registry.upsert_status(status)

        duration = time.monotonic() - start_wall
        self._log.info(
            "training complete",
            extra={
                "service": service_name,
                "version": version,
                "trained_points": training_bundle["trained_on_points"],
                "mape_p50": round(training_bundle["mape_p50"], 4),
                "mape_p90": round(training_bundle["mape_p90"], 4),
                "promoted": promote,
                "previous_mape": round(previous_mape, 4),
                "duration_sec": round(duration, 2),
            },
        )

        return TrainingResult(
            service_name=service_name,
            version=version,
            p50_model_path=str(p50_path),
            p90_model_path=str(p90_path),
            validation_mape_p50=training_bundle["mape_p50"],
            validation_mape_p90=training_bundle["mape_p90"],
            training_window_start=training_bundle["training_window_start"],
            training_window_end=training_bundle["training_window_end"],
            trained_on_points=training_bundle["trained_on_points"],
            feature_config_hash=FEATURE_CONFIG_HASH,
            promoted=promote,
            previous_mape=previous_mape,
        )

    # ------------------------------------------------------------------
    # Internals
    # ------------------------------------------------------------------

    def _train_sync(self, metrics: list[ServiceMetric]) -> dict[str, Any]:
        """All CPU-bound work in one function (called via asyncio.to_thread)."""
        X, y, ts = build_features(metrics, horizon_min=_FORECAST_HORIZON_MIN)

        if len(X) < 50:
            # A feature matrix this small can't be split meaningfully.
            raise InsufficientDataError(
                f"after feature engineering only {len(X)} usable rows; "
                f"need at least 50 to split and train"
            )

        # Time-ordered split — last holdout_percent as validation.
        holdout_frac = self._config.training.holdout_percent / 100.0
        split_idx = int(len(X) * (1.0 - holdout_frac))
        # Guarantee at least 1 row each side.
        split_idx = max(1, min(split_idx, len(X) - 1))

        X_train, X_val = X.iloc[:split_idx], X.iloc[split_idx:]
        y_train, y_val = y.iloc[:split_idx], y.iloc[split_idx:]

        train_ds = lgb.Dataset(X_train, label=y_train, feature_name=FEATURE_NAMES)
        val_ds = lgb.Dataset(
            X_val, label=y_val, reference=train_ds, feature_name=FEATURE_NAMES
        )

        booster_p50 = _train_quantile_model(train_ds, val_ds, alpha=0.5)
        booster_p90 = _train_quantile_model(train_ds, val_ds, alpha=0.9)

        pred_p50 = booster_p50.predict(X_val, num_iteration=booster_p50.best_iteration)
        pred_p90 = booster_p90.predict(X_val, num_iteration=booster_p90.best_iteration)

        mape_p50 = compute_mape(y_val.to_numpy(), np.asarray(pred_p50))
        mape_p90 = compute_mape(y_val.to_numpy(), np.asarray(pred_p90))

        return {
            "booster_p50": booster_p50,
            "booster_p90": booster_p90,
            "mape_p50": mape_p50,
            "mape_p90": mape_p90,
            "trained_on_points": int(len(X_train)),
            "training_window_start": _to_utc(ts[0]),
            "training_window_end": _to_utc(ts[-1]),
        }

    async def _next_version(self, service_name: str) -> str:
        """Pick the next v{N} string by scanning existing registry entries."""
        existing = await self._registry.list_models(service_name)
        max_n = 0
        for m in existing:
            if m.version.startswith("v"):
                tail = m.version[1:]
                if tail.isdigit():
                    max_n = max(max_n, int(tail))
        return f"v{max_n + 1}"

    def _should_promote(
        self, new_mape: float, current: ModelRecord | None
    ) -> bool:
        threshold = self._config.training.acceptance_mape_threshold
        if current is None:
            # Bootstraps serving: any first model wins, even if imperfect.
            # Serving a weak model is better than returning only stub responses.
            return True
        if new_mape >= threshold:
            return False
        tolerance = self._config.training.promotion_tolerance
        return new_mape <= current.validation_mape * (1.0 + tolerance)


# ---------------------------------------------------------------------------
# Module helpers
# ---------------------------------------------------------------------------

def _train_quantile_model(
    train_ds: lgb.Dataset, val_ds: lgb.Dataset, alpha: float
) -> lgb.Booster:
    """Train one quantile model with early stopping on the validation set."""
    params = {**_LGB_BASE_PARAMS, "alpha": alpha}
    return lgb.train(
        params,
        train_ds,
        num_boost_round=_NUM_BOOST_ROUND,
        valid_sets=[val_ds],
        valid_names=["val"],
        callbacks=[
            lgb.early_stopping(_EARLY_STOPPING_ROUNDS, verbose=False),
            lgb.log_evaluation(period=0),
        ],
    )


def _to_utc(ts: pd.Timestamp) -> datetime:
    """Convert a pandas Timestamp (tz-aware or naive) to a UTC-aware datetime."""
    py = ts.to_pydatetime()
    if py.tzinfo is None:
        py = py.replace(tzinfo=timezone.utc)
    else:
        py = py.astimezone(timezone.utc)
    return py
