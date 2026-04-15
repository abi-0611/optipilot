from collections import deque
from contextlib import asynccontextmanager
from datetime import datetime, timezone
import math
import os
from pathlib import Path
from typing import Any

import joblib
import pandas as pd
from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware

from autoscaling import compute_replicas, get_confidence_mode
from models.predict import PredictRequest, PredictResponse

# API requires at least this many recent minute-level points.
MIN_RECENT_POINTS = 8

# Confidence logic uses the last 3 prediction errors.
MAX_RECENT_APE_POINTS = 3


def _parse_minutes_suffix(feature_name: str, prefix: str) -> int:
    """Extract minute window/lag from a feature name like `rps_lag_10m`."""
    suffix = feature_name[len(prefix) :]
    if not suffix.endswith("m") or not suffix[:-1].isdigit():
        raise ValueError(f"Unsupported feature format: {feature_name}")
    value = int(suffix[:-1])
    if value <= 0:
        raise ValueError(f"Feature window must be positive: {feature_name}")
    return value


def _extract_feature_names(model: Any) -> list[str]:
    """Read feature names from either LightGBM Booster or sklearn wrapper."""
    if hasattr(model, "feature_name") and callable(model.feature_name):
        feature_names = list(model.feature_name())
    elif hasattr(model, "feature_name_"):
        feature_names = list(model.feature_name_)
    else:
        raise ValueError("Model does not expose feature names.")

    if not feature_names:
        raise ValueError("Model has no feature names.")
    return feature_names


def _build_feature_row(
    recent_rps: list[float],
    feature_names: list[str],
    now_utc: datetime,
) -> pd.DataFrame:
    """Create one inference row using recent RPS history and current calendar context."""
    series = pd.Series(recent_rps, dtype=float)
    oldest_value = float(series.iloc[0])

    def lag_value(minutes: int) -> float:
        # If history is shorter than requested lag, pad with oldest known value.
        if len(series) >= minutes:
            return float(series.iloc[-minutes])
        return oldest_value

    def rolling_value(window: int, mode: str) -> float:
        window = min(window, len(series))
        values = series.iloc[-window:]
        if mode == "mean":
            return float(values.mean())
        return float(values.max())

    feature_values: dict[str, float | int] = {}
    for feature_name in feature_names:
        if feature_name.startswith("rps_lag_"):
            lag = _parse_minutes_suffix(feature_name, "rps_lag_")
            feature_values[feature_name] = lag_value(lag)
        elif feature_name.startswith("rps_roll_mean_"):
            window = _parse_minutes_suffix(feature_name, "rps_roll_mean_")
            feature_values[feature_name] = rolling_value(window, mode="mean")
        elif feature_name.startswith("rps_roll_max_"):
            window = _parse_minutes_suffix(feature_name, "rps_roll_max_")
            feature_values[feature_name] = rolling_value(window, mode="max")
        elif feature_name == "hour_of_day":
            feature_values[feature_name] = now_utc.hour
        elif feature_name == "day_of_week":
            feature_values[feature_name] = now_utc.weekday()
        elif feature_name == "is_weekend":
            feature_values[feature_name] = 1 if now_utc.weekday() >= 5 else 0
        else:
            raise ValueError(
                f"Unsupported model feature '{feature_name}'. "
                "Update API feature builder to match training features."
            )

    return pd.DataFrame([feature_values], columns=feature_names)


def _predict_one(model: Any, features: pd.DataFrame) -> float:
    """Run one prediction and return a scalar float."""
    best_iteration = getattr(model, "best_iteration", None)
    if isinstance(best_iteration, int) and best_iteration > 0:
        values = model.predict(features, num_iteration=best_iteration)
    else:
        values = model.predict(features)
    return float(values[0])


def _load_models(models_dir: Path) -> dict[str, dict[str, Any]]:
    """Load all available service model pairs from disk once at startup."""
    model_store: dict[str, dict[str, Any]] = {}
    for p50_path in sorted(models_dir.glob("model_p50_*.pkl")):
        service_name = p50_path.stem.replace("model_p50_", "", 1)
        p90_path = models_dir / f"model_p90_{service_name}.pkl"
        if not p90_path.exists():
            continue

        model_p50 = joblib.load(p50_path)
        model_p90 = joblib.load(p90_path)
        feature_names_p50 = _extract_feature_names(model_p50)
        feature_names_p90 = _extract_feature_names(model_p90)
        if feature_names_p50 != feature_names_p90:
            raise RuntimeError(
                f"Feature mismatch between p50 and p90 models for service '{service_name}'."
            )

        model_store[service_name] = {
            "model_p50": model_p50,
            "model_p90": model_p90,
            "feature_names": feature_names_p50,
        }

    if not model_store:
        raise RuntimeError(
            f"No model pairs found in {models_dir}. Expected files like "
            "'model_p50_<service>.pkl' and 'model_p90_<service>.pkl'."
        )

    return model_store


def _get_recent_mape(app_obj: FastAPI, service_name: str, latest_actual_rps: float) -> float:
    """Compute mean absolute percentage error from last up-to-3 prediction errors."""
    recent_errors = app_obj.state.recent_ape_history.setdefault(
        service_name, deque(maxlen=MAX_RECENT_APE_POINTS)
    )
    previous_prediction = app_obj.state.last_p50_prediction.get(service_name)

    if previous_prediction is not None:
        ape = abs(latest_actual_rps - previous_prediction) / max(abs(latest_actual_rps), 1.0)
        recent_errors.append(float(ape))

    if not recent_errors:
        return 0.0
    return float(sum(recent_errors) / len(recent_errors))


@asynccontextmanager
async def lifespan(app_obj: FastAPI):
    """Load model artifacts once when the API process starts."""
    default_models_dir = Path(__file__).resolve().parent
    models_dir = Path(os.getenv("MODELS_DIR", default_models_dir))
    app_obj.state.model_store = _load_models(models_dir=models_dir)
    app_obj.state.recent_ape_history = {}
    app_obj.state.last_p50_prediction = {}
    yield


app = FastAPI(title="OptiPilot Forecaster API", lifespan=lifespan)

# Allow browser calls from a React frontend during local development.
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get("/health")
def health() -> dict[str, str]:
    """Simple readiness endpoint."""
    return {"status": "ok"}


@app.post("/predict", response_model=PredictResponse)
def predict(payload: PredictRequest) -> PredictResponse:
    """Predict p50/p90 RPS and return autoscaling guidance for one service."""
    recent_rps = payload.recent_rps
    if recent_rps is None:
        raise HTTPException(
            status_code=400,
            detail="Field 'recent_rps' is required and must contain numeric values.",
        )
    if len(recent_rps) < MIN_RECENT_POINTS:
        raise HTTPException(
            status_code=400,
            detail=f"Need at least {MIN_RECENT_POINTS} points in 'recent_rps'; got {len(recent_rps)}.",
        )
    if any(not math.isfinite(float(v)) for v in recent_rps):
        raise HTTPException(status_code=400, detail="'recent_rps' contains non-finite values.")
    if any(float(v) < 0 for v in recent_rps):
        raise HTTPException(status_code=400, detail="'recent_rps' cannot contain negative values.")

    service_name = payload.service_name
    model_info = app.state.model_store.get(service_name)
    if model_info is None:
        available = ", ".join(sorted(app.state.model_store.keys()))
        raise HTTPException(
            status_code=400,
            detail=f"Unsupported service_name '{service_name}'. Available services: {available}",
        )

    now_utc = datetime.now(timezone.utc)
    try:
        features = _build_feature_row(
            recent_rps=recent_rps,
            feature_names=model_info["feature_names"],
            now_utc=now_utc,
        )
    except ValueError as error:
        raise HTTPException(status_code=400, detail=str(error)) from error

    rps_p50 = _predict_one(model=model_info["model_p50"], features=features)
    rps_p90 = _predict_one(model=model_info["model_p90"], features=features)

    latest_actual_rps = float(recent_rps[-1])
    recent_mape = _get_recent_mape(
        app_obj=app,
        service_name=service_name,
        latest_actual_rps=latest_actual_rps,
    )
    scaling_mode, reason = get_confidence_mode(rps_p50=rps_p50, rps_p90=rps_p90, recent_mape=recent_mape)
    recommended_replicas = compute_replicas(rps_p90=rps_p90, service_name=service_name)

    # Save latest p50 for next call's rolling MAPE estimate.
    app.state.last_p50_prediction[service_name] = float(rps_p50)

    return PredictResponse(
        service_name=service_name,
        rps_p50=round(rps_p50, 2),
        rps_p90=round(rps_p90, 2),
        recommended_replicas=int(recommended_replicas),
        scaling_mode=scaling_mode,
        reason=reason,
    )
