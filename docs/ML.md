# OptiPilot ML Pipeline

This document explains the forecasting pipeline in the forecaster service.

## 1. Why LightGBM + quantile regression

OptiPilot predicts **future request rate (RPS)** under noisy, non-linear, and seasonal traffic. LightGBM is used because it:

- handles non-linear feature interactions without extensive manual transforms,
- trains fast enough for frequent incremental retrains,
- performs well on tabular operational telemetry,
- supports **quantile objectives**, allowing direct p50/p90 estimates.

Using p50 + p90 forecasts gives both a central estimate and uncertainty band used for confidence-aware mode selection.

## 2. Feature engineering rationale

Pipeline (per service):

1. Convert raw metrics to a timestamp-indexed frame.
2. Resample to 1-minute bins.
3. Forward-fill short gaps (up to 5 minutes), discard long-gap regions.
4. Build features and target (`rps_future_5m`).
5. Drop rows with NaN from lag/rolling/target shifts.

Feature groups:

- **Lag features**: `rps_lag_{1,2,3,5,10,15,30}`
- **Rolling stats**: mean/max over 5m, 15m, 30m windows
- **Temporal**: hour-of-day, day-of-week, weekend flag
- **Resource context**: cpu usage, memory, error rate

This set intentionally keeps dimensionality modest while capturing short-term dynamics, trend smoothing, and time seasonality.

## 3. Training lifecycle

## Data gates

- minimum raw points: `training.min_data_points` (default `1440`)
- minimum engineered rows: at least `50` rows after feature generation

## Train/eval flow

1. Time-ordered train/validation split (`holdout_percent`, default 20%).
2. Train two quantile models:
   - p50 (`alpha=0.5`)
   - p90 (`alpha=0.9`)
3. Evaluate MAPE on validation set.
4. Persist models as versioned artifacts (`vN`) and register metadata.

## Incremental vs full retrain

- **Incremental retrain:** recent 2h window, no hyperparameter search.
- **Full retrain:** up to 90d window, lightweight parameter grid search.

Scheduler jobs run per registered service:

- recalibration (`training.recalibration_interval_min`)
- incremental retrain (`training.incremental_retrain_interval_min`)
- full retrain (`training.full_retrain_interval_hours`)
- drift detection (every 5 minutes)

## 4. Promotion criteria

A newly trained model is promoted if:

1. there is no promoted model yet (bootstrap), or
2. new `mape_p50` is below `acceptance_mape_threshold`, **and**
3. new `mape_p50 <= current_mape * (1 + promotion_tolerance)`.

Default config:

- `acceptance_mape_threshold = 0.30`
- `promotion_tolerance = 0.05`

This policy avoids churn from noisy marginal improvements while blocking clearly poor models.

## 5. Inference and recommendation logic

Inference engine:

1. loads current promoted model from registry (lazy cache),
2. validates feature schema hash,
3. computes p50/p90 for the latest row,
4. applies bias recalibration offsets,
5. enforces quantile monotonicity (`p90 >= p50`),
6. computes replica recommendation:

`recommended = ceil((p90 / capacity_per_replica) * (1 + headroom_factor))`

Mode/confidence from interval width:

- narrow `(p90-p50)/max(p50,1)` -> `PREDICTIVE`, high confidence
- medium -> `CONSERVATIVE`
- wide -> `REACTIVE`, low confidence

## 6. Drift detection math

Rolling window (default 3h):

- pair each prediction with near-time actuals,
- compute rolling MAPE:

`mean(abs(y_true - y_pred) / max(abs(y_true), 1))`

Threshold behavior:

- if `rolling_mape > conservative_mape_threshold` (default 0.20) -> degrade to `CONSERVATIVE`
- if `rolling_mape > reactive_mape_threshold` (default 0.30) -> degrade to `REACTIVE` and trigger emergency retrain attempt

Drift state is written to registry status and used by inference as override.

## 7. Manual retrain conditions

Manual retrain (`TriggerRetrain`) succeeds only when:

- enough metrics exist for the service (`min_data_points`),
- feature matrix has sufficient usable rows,
- training completes without runtime/model I/O errors.

Promotion of that retrained model still obeys the same promotion criteria above.

## 8. Current limitations

- Forecast quality depends on data maturity; sparse services may stay in fallback behavior.
- Model family is single-algorithm (LightGBM); ensemble/online alternatives are not yet included.
- Multi-cluster/shared-global models are out of current v1 scope.

