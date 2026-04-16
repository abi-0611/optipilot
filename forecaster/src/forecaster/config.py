"""
Configuration loading for the OptiPilot forecaster.

Loads from a YAML file, then applies overrides from environment variables
with the OPTIPILOT_FC_ prefix. Nested keys use underscore separators:
  OPTIPILOT_FC_SERVER_PORT=50052
  OPTIPILOT_FC_STORAGE_REGISTRY_DB=/data/registry.db
  OPTIPILOT_FC_TRAINING_MIN_DATA_POINTS=2000
"""

from __future__ import annotations

import os
from typing import Any

import yaml
from pydantic import BaseModel, Field, field_validator


class ServerConfig(BaseModel):
    host: str = "0.0.0.0"
    port: int = Field(default=50051, ge=1, le=65535)
    max_workers: int = Field(default=10, ge=1)


class StorageConfig(BaseModel):
    registry_db: str = "forecaster_registry.db"
    metrics_db: str = "forecaster_metrics.db"
    retention_days: int = Field(default=90, ge=1)


class TrainingConfig(BaseModel):
    min_data_points: int = Field(default=1440, ge=1)
    holdout_percent: int = Field(default=20, ge=1, le=99)
    full_retrain_interval_hours: int = Field(default=24, ge=1)
    incremental_retrain_interval_min: int = Field(default=10, ge=1)
    recalibration_interval_min: int = Field(default=5, ge=1)
    acceptance_mape_threshold: float = Field(default=0.30, ge=0.0, le=1.0)
    # Directory where .pkl model files are stored (relative to process CWD)
    models_dir: str = "models"
    # New model may be up to (1 + tolerance) times worse than current and still
    # be promoted — prevents churn when MAPE only worsens within noise.
    promotion_tolerance: float = Field(default=0.05, ge=0.0, le=1.0)


class InferenceConfig(BaseModel):
    forecast_horizons_min: list[int] = Field(default=[5, 15])
    default_capacity_per_replica: int = Field(default=100, ge=1)
    default_headroom_factor: float = Field(default=0.20, ge=0.0, le=1.0)

    @field_validator("forecast_horizons_min")
    @classmethod
    def horizons_must_be_positive(cls, v: list[int]) -> list[int]:
        if not v:
            raise ValueError("forecast_horizons_min must not be empty")
        if any(h <= 0 for h in v):
            raise ValueError("all forecast horizons must be positive")
        return v


class DriftConfig(BaseModel):
    rolling_window_hours: int = Field(default=3, ge=1)
    conservative_mape_threshold: float = Field(default=0.20, ge=0.0, le=1.0)
    reactive_mape_threshold: float = Field(default=0.30, ge=0.0, le=1.0)


class LoggingConfig(BaseModel):
    level: str = "INFO"
    format: str = "json"  # "json" or "text"

    @field_validator("level")
    @classmethod
    def level_must_be_valid(cls, v: str) -> str:
        valid = {"DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"}
        if v.upper() not in valid:
            raise ValueError(f"logging level must be one of {valid}")
        return v.upper()

    @field_validator("format")
    @classmethod
    def format_must_be_valid(cls, v: str) -> str:
        if v not in ("json", "text"):
            raise ValueError('logging format must be "json" or "text"')
        return v


class Config(BaseModel):
    server: ServerConfig = Field(default_factory=ServerConfig)
    storage: StorageConfig = Field(default_factory=StorageConfig)
    training: TrainingConfig = Field(default_factory=TrainingConfig)
    inference: InferenceConfig = Field(default_factory=InferenceConfig)
    drift: DriftConfig = Field(default_factory=DriftConfig)
    logging: LoggingConfig = Field(default_factory=LoggingConfig)


def _apply_env_overrides(data: dict[str, Any]) -> dict[str, Any]:
    """
    Walk environment variables with prefix OPTIPILOT_FC_ and apply them
    as overrides into the config dict. Keys are lowercased and split on _.

    Examples:
      OPTIPILOT_FC_SERVER_PORT=50052        -> data["server"]["port"] = "50052"
      OPTIPILOT_FC_STORAGE_REGISTRY_DB=... -> data["storage"]["registry_db"] = ...
    """
    prefix = "OPTIPILOT_FC_"
    section_keys = {
        "server": set(ServerConfig.model_fields),
        "storage": set(StorageConfig.model_fields),
        "training": set(TrainingConfig.model_fields),
        "inference": set(InferenceConfig.model_fields),
        "drift": set(DriftConfig.model_fields),
        "logging": set(LoggingConfig.model_fields),
    }

    for env_key, env_val in os.environ.items():
        if not env_key.startswith(prefix):
            continue
        remainder = env_key[len(prefix):].lower()  # e.g. "server_port"

        # Match against known section + field combinations
        matched = False
        for section, fields in section_keys.items():
            section_prefix = section + "_"
            if remainder.startswith(section_prefix):
                field_name = remainder[len(section_prefix):]
                if field_name in fields:
                    if section not in data:
                        data[section] = {}
                    data[section][field_name] = env_val
                    matched = True
                    break
        if not matched:
            # Top-level key (unlikely given the schema, but handle gracefully)
            if remainder in Config.model_fields:
                data[remainder] = env_val


    return data


def load_config(path: str = "forecaster.yaml") -> Config:
    """Load config from YAML file, then apply OPTIPILOT_FC_* env var overrides."""
    data: dict[str, Any] = {}

    if os.path.exists(path):
        with open(path) as f:
            loaded = yaml.safe_load(f)
            if isinstance(loaded, dict):
                data = loaded

    data = _apply_env_overrides(data)
    return Config.model_validate(data)
