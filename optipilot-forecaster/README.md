# optipilot-forecaster

Generate synthetic 1-minute RPS time-series data for:
- `api-gateway`
- `order-service`
- `payment-service`

The generator creates:
- 21 days of data per service
- realistic daily peaks (around 9am and 7pm)
- random noise
- one sudden spike event per service
- one combined plot for all services

## Run with uv

```bash
uv run python generate_data.py
```

Outputs are written to `generated_data/`:
- `api-gateway.csv`
- `order-service.csv`
- `payment-service.csv`
- `all-services-rps.png`

## Build model features

Use `build_features(df, service_name)` from `feature_engineering.py` to create:
- lag features (`t-1, t-2, t-3, t-5, t-10, t-15, t-30`)
- rolling mean/max windows (`5, 15, 30`)
- time context (`hour_of_day`, `day_of_week`, `is_weekend`)
- target (`rps` at `t+5`)

## Train p50/p90 quantile models

```bash
uv run python train_quantile_models.py --service-name api-gateway --data-dir generated_data --output-dir .
```

This script:
- uses the newest 2 days as test data (time ordered, no shuffle)
- trains LightGBM quantile models for `alpha=0.5` and `alpha=0.9` with early stopping
- prints p50 MAE and MAPE on test data
- saves `model_p50_<service>.pkl`, `model_p90_<service>.pkl`, and a test prediction plot

## Compute replica recommendations

Use `compute_replicas(rps_p90, service_name)` from `autoscaling.py` to:
- convert p90 RPS into replica demand using per-pod capacity and target utilization
- add 20% headroom
- clamp to min/max replicas (`2` to `20`)
- enforce scale-up (2 min) and scale-down (10 min) cooldown behavior

Use `get_confidence_mode(rps_p50, rps_p90, recent_mape)` from `autoscaling.py` to
switch between `PREDICTIVE`, `CONSERVATIVE`, and `REACTIVE` scaling modes based on
forecast interval width and recent model error.

## Demo replay: predictive vs reactive

```bash
uv run python demo_simulation.py --service-name api-gateway --data-dir generated_data --models-dir . --output-dir .
```

This demo replays the last 3 hours minute-by-minute, compares reactive and predictive
replica decisions, and saves:
- `demo_replay_<service>.csv`
- `predictive_vs_reactive_demo_<service>.png`

## FastAPI inference service

Start API:

```bash
uv run uvicorn app:app --host 0.0.0.0 --port 8000 --reload
```

Endpoints:
- `GET /health` -> `{"status": "ok"}`
- `POST /predict`

Example request:

```json
{
  "service_name": "api-gateway",
  "recent_rps": [120, 130, 125, 140, 135, 128, 122, 119]
}
```
