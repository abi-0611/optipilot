"""
Internal dataclasses for the forecaster.

These mirror proto message shapes but use Python-idiomatic types so the
rest of the codebase stays independent of protobuf. Converters live in
server/service.py where the proto boundary is crossed.

Note: field names match the proto where possible.
  - cpu_usage_percent (proto: cpu_usage_percent)
  - memory_usage_mb   (proto: memory_usage_mb)
  - p99_latency_ms    (proto has p99 only, no p95)
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from typing import Optional


@dataclass
class ServiceMetric:
    service_name: str
    rps: float
    avg_latency_ms: float
    p99_latency_ms: float
    active_connections: int
    cpu_usage_percent: float
    memory_usage_mb: float
    error_rate: float
    timestamp: datetime


@dataclass
class ModelRecord:
    service_name: str
    version: str                    # e.g. "v1", "v2"
    created_at: datetime
    trained_on_points: int
    training_window_start: datetime
    training_window_end: datetime
    validation_mape: float
    is_promoted: bool               # True = currently serving
    file_path: str                  # path to serialised model file
    feature_config_hash: str        # hash of feature config at train time


@dataclass
class PredictionResult:
    service_name: str
    rps_p50: float
    rps_p90: float
    recommended_replicas: int
    scaling_mode: str               # "PREDICTIVE" | "CONSERVATIVE" | "REACTIVE"
    confidence_score: float
    reason: str
    model_version: str


@dataclass
class StoredPrediction:
    service_name: str
    predicted_at: datetime
    horizon_min: int
    predicted_rps_p50: float
    predicted_rps_p90: float
    scaling_mode: str
    model_version: str
    confidence_score: float


@dataclass
class ModelStatus:
    service_name: str
    model_version: str
    current_mape: float
    scaling_mode: str
    last_trained_at: datetime
    last_recalibrated_at: datetime
    training_data_points: int
    bias_offset_p50: float = 0.0
    bias_offset_p90: float = 0.0
    degraded_to: Optional[str] = None
    rolling_mape_3h: float = 0.0
