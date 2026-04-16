"""
Model persistence — save / load LightGBM models via joblib.

File layout:
  {models_dir}/{service_name}/{version}_{quantile}.pkl
  e.g. models/api-gateway/v3_p50.pkl

joblib is preferred over raw pickle because it handles large numpy arrays
efficiently and is the de-facto standard for sklearn-style model artefacts.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

import joblib
import lightgbm as lgb


def model_path(models_dir: Path, service_name: str, version: str, quantile: str) -> Path:
    """Return the canonical .pkl path for a given (service, version, quantile)."""
    return Path(models_dir) / service_name / f"{version}_{quantile}.pkl"


def save_model(model: Any, path: Path) -> None:
    """Persist a model to disk, creating parent directories as needed."""
    path.parent.mkdir(parents=True, exist_ok=True)
    joblib.dump(model, path)


def load_model(path: Path) -> lgb.Booster:
    """Load a model previously saved with `save_model`."""
    return joblib.load(path)
