from argparse import ArgumentParser
from pathlib import Path
from typing import Any

import joblib
import matplotlib.pyplot as plt
import pandas as pd

from autoscaling import MIN_REPLICAS, POD_RPS_CAPACITY, _SCALER_STATE, compute_replicas
from feature_engineering import build_features

# Replay only the newest 3 hours at 1-minute granularity.
REPLAY_MINUTES = 3 * 60

# Reactive trigger: scale only when demand reaches 80% of current capacity.
REACTIVE_TRIGGER_UTILIZATION = 0.80


def load_service_data(data_path: Path, service_name: str) -> pd.DataFrame:
    """Load and validate one service time series."""
    if not data_path.exists():
        raise FileNotFoundError(f"Data CSV not found: {data_path}")

    df = pd.read_csv(data_path)
    required_columns = {"timestamp", "service_name", "rps"}
    missing_columns = required_columns - set(df.columns)
    if missing_columns:
        missing_str = ", ".join(sorted(missing_columns))
        raise ValueError(f"Missing required columns: {missing_str}")

    df["timestamp"] = pd.to_datetime(df["timestamp"], errors="raise")
    service_df = df.loc[df["service_name"] == service_name].sort_values("timestamp").reset_index(drop=True)
    if service_df.empty:
        raise ValueError(f"No data found for service_name='{service_name}' in {data_path}")
    return service_df


def predict_series(model: Any, X: pd.DataFrame) -> pd.Series:
    """Run model prediction with best iteration when available."""
    best_iteration = getattr(model, "best_iteration", None)
    if isinstance(best_iteration, int) and best_iteration > 0:
        predictions = model.predict(X, num_iteration=best_iteration)
    else:
        predictions = model.predict(X)
    return pd.Series(predictions, index=X.index)


def detect_spike_timestamp(service_df: pd.DataFrame, replay_df: pd.DataFrame) -> pd.Timestamp:
    """Pick a spike timestamp to mark on plots (prefer global spike if inside replay window)."""
    global_spike_time = service_df.loc[service_df["rps"].idxmax(), "timestamp"]
    replay_start = replay_df["timestamp"].iloc[0]
    replay_end = replay_df["timestamp"].iloc[-1]
    if replay_start <= global_spike_time <= replay_end:
        return global_spike_time

    # Fallback: mark local spike in the replay window so the indicator is visible.
    return replay_df.loc[replay_df["actual_rps"].idxmax(), "timestamp"]


def simulate_replay(
    service_df: pd.DataFrame,
    pred_p50: pd.Series,
    pred_p90: pd.Series,
    service_name: str,
) -> pd.DataFrame:
    """Replay minute-by-minute scaling behavior for reactive and predictive systems."""
    if len(pred_p50) < REPLAY_MINUTES or len(pred_p90) < REPLAY_MINUTES:
        raise ValueError("Not enough feature rows to replay the last 3 hours.")

    actual_rps_by_time = service_df.set_index("timestamp")["rps"]
    replay_index = pred_p90.index[-REPLAY_MINUTES:]

    # Keep scaling states separate so both systems can run side by side.
    reactive_key = f"{service_name}::reactive"
    predictive_key = f"{service_name}::predictive"
    _SCALER_STATE[reactive_key] = {"current_replicas": MIN_REPLICAS, "last_scale_at": None}
    _SCALER_STATE[predictive_key] = {"current_replicas": MIN_REPLICAS, "last_scale_at": None}

    records: list[dict[str, float | pd.Timestamp | int]] = []
    for timestamp in replay_index:
        actual_rps = float(actual_rps_by_time.loc[timestamp])

        # --- Reactive system ---
        reactive_current = int(_SCALER_STATE[reactive_key]["current_replicas"])
        reactive_capacity = reactive_current * POD_RPS_CAPACITY
        if actual_rps >= REACTIVE_TRIGGER_UTILIZATION * reactive_capacity:
            reactive_replicas = compute_replicas(actual_rps, reactive_key)
        else:
            reactive_replicas = reactive_current

        # --- Predictive system (5-minute-ahead forecast) ---
        predictive_replicas = compute_replicas(float(pred_p90.loc[timestamp]), predictive_key)

        reactive_capacity_used_pct = (actual_rps / max(reactive_replicas * POD_RPS_CAPACITY, 1.0)) * 100.0
        predictive_capacity_used_pct = (actual_rps / max(predictive_replicas * POD_RPS_CAPACITY, 1.0)) * 100.0

        records.append(
            {
                "timestamp": timestamp,
                "actual_rps": actual_rps,
                "predicted_rps_p50": float(pred_p50.loc[timestamp]),
                "predicted_rps_p90": float(pred_p90.loc[timestamp]),
                "reactive_replicas": int(reactive_replicas),
                "predictive_replicas": int(predictive_replicas),
                "reactive_capacity_used_pct": reactive_capacity_used_pct,
                "predictive_capacity_used_pct": predictive_capacity_used_pct,
            }
        )

    return pd.DataFrame(records)


def plot_simulation(
    replay_df: pd.DataFrame,
    spike_time: pd.Timestamp,
    output_path: Path,
) -> None:
    """Create the requested two-panel demo visualization."""
    fig, (ax1, ax2) = plt.subplots(nrows=2, ncols=1, figsize=(16, 10), sharex=True)

    # Panel 1: Actual traffic with reactive/predictive replica trajectories.
    ax1.plot(replay_df["timestamp"], replay_df["actual_rps"], color="black", linewidth=1.4, label="Actual RPS")
    ax1.set_ylabel("Actual RPS")
    ax1.grid(alpha=0.25)

    ax1_replicas = ax1.twinx()
    ax1_replicas.step(
        replay_df["timestamp"],
        replay_df["reactive_replicas"],
        where="post",
        color="tab:blue",
        linewidth=1.2,
        label="Reactive Replicas",
    )
    ax1_replicas.step(
        replay_df["timestamp"],
        replay_df["predictive_replicas"],
        where="post",
        color="tab:green",
        linewidth=1.2,
        label="Predictive Replicas",
    )
    ax1_replicas.set_ylabel("Replicas")
    ax1.axvline(spike_time, color="red", linestyle="--", linewidth=1.5, label="Spike Event")

    lines1, labels1 = ax1.get_legend_handles_labels()
    lines2, labels2 = ax1_replicas.get_legend_handles_labels()
    ax1.legend(lines1 + lines2, labels1 + labels2, loc="upper left")

    # Panel 2: Capacity usage for each system.
    ax2.plot(
        replay_df["timestamp"],
        replay_df["reactive_capacity_used_pct"],
        color="tab:blue",
        linewidth=1.4,
        label="Reactive Capacity Used (%)",
    )
    ax2.plot(
        replay_df["timestamp"],
        replay_df["predictive_capacity_used_pct"],
        color="tab:green",
        linewidth=1.4,
        label="Predictive Capacity Used (%)",
    )
    ax2.axhline(80, color="gray", linestyle=":", linewidth=1, label="80% Threshold")
    ax2.axvline(spike_time, color="red", linestyle="--", linewidth=1.5, label="Spike Event")
    ax2.set_ylabel("Capacity Utilization (%)")
    ax2.set_xlabel("Timestamp")
    ax2.grid(alpha=0.25)
    ax2.legend(loc="upper left")

    fig.suptitle("Predictive vs Reactive Autoscaling — Demo")
    fig.tight_layout(rect=(0, 0.02, 1, 0.96))
    fig.savefig(output_path, dpi=150)
    plt.close(fig)


def main() -> None:
    parser = ArgumentParser(description="Replay autoscaling decisions for reactive vs predictive systems.")
    parser.add_argument("--service-name", default="api-gateway", help="Service to replay.")
    parser.add_argument("--data-dir", default="generated_data", help="Directory containing per-service CSV files.")
    parser.add_argument("--models-dir", default=".", help="Directory containing model_p50/p90 .pkl files.")
    parser.add_argument("--output-dir", default=".", help="Directory to write demo artifacts.")
    args = parser.parse_args()

    service_name = args.service_name
    data_path = Path(args.data_dir) / f"{service_name}.csv"
    p50_model_path = Path(args.models_dir) / f"model_p50_{service_name}.pkl"
    p90_model_path = Path(args.models_dir) / f"model_p90_{service_name}.pkl"
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)

    if not p50_model_path.exists():
        raise FileNotFoundError(f"Missing p50 model file: {p50_model_path}")
    if not p90_model_path.exists():
        raise FileNotFoundError(f"Missing p90 model file: {p90_model_path}")

    # Build feature rows, then run p50/p90 forecasts for every eligible minute.
    service_df = load_service_data(data_path=data_path, service_name=service_name)
    X, _ = build_features(df=service_df, service_name=service_name)

    model_p50 = joblib.load(p50_model_path)
    model_p90 = joblib.load(p90_model_path)
    pred_p50 = predict_series(model=model_p50, X=X)
    pred_p90 = predict_series(model=model_p90, X=X)

    # Replay the latest 3-hour window and gather minute-level metrics.
    replay_df = simulate_replay(
        service_df=service_df,
        pred_p50=pred_p50,
        pred_p90=pred_p90,
        service_name=service_name,
    )
    spike_time = detect_spike_timestamp(service_df=service_df, replay_df=replay_df)

    replay_csv_path = output_dir / f"demo_replay_{service_name}.csv"
    replay_plot_path = output_dir / f"predictive_vs_reactive_demo_{service_name}.png"
    replay_df.to_csv(replay_csv_path, index=False)
    plot_simulation(replay_df=replay_df, spike_time=spike_time, output_path=replay_plot_path)

    print(f"Saved replay metrics: {replay_csv_path}")
    print(f"Saved demo plot: {replay_plot_path}")


if __name__ == "__main__":
    main()
