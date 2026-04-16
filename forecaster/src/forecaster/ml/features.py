"""
Feature engineering for RPS forecasting.

Pipeline (pure functions, no I/O):
  list[ServiceMetric]
    -> resample to 1-minute regular frequency (mean of values in each bin)
    -> forward-fill short gaps (<=5 minutes), drop rows with larger gaps
    -> compute lag / rolling / temporal / resource features
    -> create target = rps shifted by -horizon_min
    -> drop rows with NaN from lagging or shifting
    -> return (X, y, timestamps)

The feature set is small and deliberately chosen: it is the minimum that
captures daily + weekly seasonality (temporal), recent dynamics (lags),
smoothed trend (rollings), and cross-signal context (resource).
"""

from __future__ import annotations

import hashlib
from typing import Iterable

import numpy as np
import pandas as pd

from forecaster.models import ServiceMetric


FEATURE_VERSION = "v1"

# Lag offsets (minutes ago). Chosen to give the model short-term + medium-term
# context without exploding feature count.
_LAG_MINUTES = [1, 2, 3, 5, 10, 15, 30]

# Rolling window sizes (minutes) — we compute mean+max for each.
_ROLLING_WINDOWS = [5, 15, 30]

FEATURE_NAMES: list[str] = (
    [f"rps_lag_{n}" for n in _LAG_MINUTES]
    + [f"rps_mean_{w}m" for w in _ROLLING_WINDOWS]
    + [f"rps_max_{w}m" for w in _ROLLING_WINDOWS]
    + ["hour_of_day", "day_of_week", "is_weekend"]
    + ["cpu_usage_percent", "memory_usage_mb", "error_rate"]
)


def _compute_feature_config_hash() -> str:
    """Stable short hash of (feature names, version). Registered with each model
    so we can detect feature-set drift when loading older models later."""
    payload = "|".join(sorted(FEATURE_NAMES)) + "|" + FEATURE_VERSION
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()[:16]


FEATURE_CONFIG_HASH: str = _compute_feature_config_hash()


# ---------------------------------------------------------------------------
# Core pipeline
# ---------------------------------------------------------------------------

def _metrics_to_frame(metrics: Iterable[ServiceMetric]) -> pd.DataFrame:
    """Convert dataclass list -> DataFrame with UTC DatetimeIndex."""
    rows = [
        {
            "timestamp": m.timestamp,
            "rps": m.rps,
            "avg_latency_ms": m.avg_latency_ms,
            "p99_latency_ms": m.p99_latency_ms,
            "active_connections": m.active_connections,
            "cpu_usage_percent": m.cpu_usage_percent,
            "memory_usage_mb": m.memory_usage_mb,
            "error_rate": m.error_rate,
        }
        for m in metrics
    ]
    df = pd.DataFrame(rows)
    if df.empty:
        return df
    df["timestamp"] = pd.to_datetime(df["timestamp"], utc=True)
    df = df.sort_values("timestamp").set_index("timestamp")
    # Collapse duplicate timestamps (rare but possible) via mean.
    df = df[~df.index.duplicated(keep="last")]
    return df


def _resample_to_minute(df: pd.DataFrame, gap_fill_limit: int = 5) -> pd.DataFrame:
    """
    Resample to a regular 1-minute frequency. Gaps up to `gap_fill_limit`
    minutes are forward-filled; longer gaps remain NaN and are dropped later.
    """
    if df.empty:
        return df
    # mean() across each 1-minute bin; if the bin has no samples -> NaN.
    resampled = df.resample("1min").mean()
    # Forward-fill short gaps only.
    filled = resampled.ffill(limit=gap_fill_limit)
    return filled


def build_features(
    metrics: list[ServiceMetric], horizon_min: int = 5
) -> tuple[pd.DataFrame, pd.Series, pd.DatetimeIndex]:
    """
    Build (X, y, timestamps) for supervised quantile training.

    horizon_min: how many minutes ahead to predict (target = rps at t+horizon).
    Returns X as float64 DataFrame in FEATURE_NAMES order, y as float64 Series
    named 'rps_future_{horizon}m', and the timestamp index aligned with both.
    Rows with any NaN (from lags, rolling, or target shift) are dropped.
    """
    df = _metrics_to_frame(metrics)
    if df.empty:
        return (
            pd.DataFrame(columns=FEATURE_NAMES),
            pd.Series(dtype=float, name=f"rps_future_{horizon_min}m"),
            pd.DatetimeIndex([]),
        )

    df = _resample_to_minute(df)

    # Drop rows where even rps is NaN (means a gap > gap_fill_limit) — any
    # feature computed over these would be meaningless.
    df = df.dropna(subset=["rps"])

    rps = df["rps"]

    features = pd.DataFrame(index=df.index)

    # Lag features
    for n in _LAG_MINUTES:
        features[f"rps_lag_{n}"] = rps.shift(n)

    # Rolling stats (window-based on the already 1-minute regular index)
    for w in _ROLLING_WINDOWS:
        features[f"rps_mean_{w}m"] = rps.rolling(window=w, min_periods=w).mean()
        features[f"rps_max_{w}m"] = rps.rolling(window=w, min_periods=w).max()

    # Temporal features — simple integers. LightGBM handles them natively.
    features["hour_of_day"] = df.index.hour.astype("int64")
    features["day_of_week"] = df.index.dayofweek.astype("int64")
    features["is_weekend"] = (df.index.dayofweek >= 5).astype("int64")

    # Resource features — current values at time t
    features["cpu_usage_percent"] = df["cpu_usage_percent"]
    features["memory_usage_mb"] = df["memory_usage_mb"]
    features["error_rate"] = df["error_rate"]

    # Target
    target_name = f"rps_future_{horizon_min}m"
    target = rps.shift(-horizon_min).rename(target_name)

    # Combine + drop rows with NaN (start rows from lagging, tail rows from shift)
    combined = features.join(target, how="inner").dropna()

    X = combined[FEATURE_NAMES].astype("float64").copy()
    y = combined[target_name].astype("float64").copy()
    ts = combined.index

    return X, y, ts


# ---------------------------------------------------------------------------
# Metrics
# ---------------------------------------------------------------------------

def compute_mape(y_true: np.ndarray, y_pred: np.ndarray, eps: float = 1.0) -> float:
    """
    Mean Absolute Percentage Error as a fraction (0.10 = 10%).

    Guards against division-by-zero by flooring |y_true| at `eps`. Default eps=1.0
    is reasonable for RPS where 1 rps is a natural noise floor.
    """
    y_true = np.asarray(y_true, dtype=np.float64)
    y_pred = np.asarray(y_pred, dtype=np.float64)
    if y_true.size == 0:
        return 0.0
    denom = np.maximum(np.abs(y_true), eps)
    return float(np.mean(np.abs(y_true - y_pred) / denom))
