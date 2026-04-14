import pandas as pd


def build_features(df: pd.DataFrame, service_name: str) -> tuple[pd.DataFrame, pd.Series]:
    """Build a LightGBM-ready feature matrix and target for one service."""
    # Validate input schema early so failures are explicit.
    required_columns = {"timestamp", "service_name", "rps"}
    missing_columns = required_columns - set(df.columns)
    if missing_columns:
        missing_str = ", ".join(sorted(missing_columns))
        raise ValueError(f"Missing required columns: {missing_str}")

    # Keep one service, sort by time, and ensure timestamp is datetime.
    service_df = df.loc[df["service_name"] == service_name, ["timestamp", "service_name", "rps"]].copy()
    if service_df.empty:
        raise ValueError(f"No rows found for service_name='{service_name}'")

    service_df["timestamp"] = pd.to_datetime(service_df["timestamp"], errors="raise")
    service_df = service_df.sort_values("timestamp").reset_index(drop=True)

    # Lag features from recent history (minutes).
    lag_minutes = [1, 2, 3, 5, 10, 15, 30]
    for lag in lag_minutes:
        service_df[f"rps_lag_{lag}m"] = service_df["rps"].shift(lag)

    # Rolling aggregates that capture short-term trends and local extremes.
    rolling_windows = [5, 15, 30]
    for window in rolling_windows:
        service_df[f"rps_roll_mean_{window}m"] = service_df["rps"].rolling(
            window=window, min_periods=window
        ).mean()
        service_df[f"rps_roll_max_{window}m"] = service_df["rps"].rolling(
            window=window, min_periods=window
        ).max()

    # Calendar/time context features.
    service_df["hour_of_day"] = service_df["timestamp"].dt.hour
    service_df["day_of_week"] = service_df["timestamp"].dt.dayofweek
    service_df["is_weekend"] = (service_df["day_of_week"] >= 5).astype(int)

    # Prediction target: RPS value 5 minutes into the future.
    service_df["target_rps_t_plus_5m"] = service_df["rps"].shift(-5)

    # Remove rows with missing values created by lag/rolling/target shifts.
    model_df = service_df.dropna().copy()
    model_df = model_df.set_index("timestamp")

    # Keep features in a stable order for reproducible model inputs.
    feature_columns = (
        [f"rps_lag_{lag}m" for lag in lag_minutes]
        + [f"rps_roll_mean_{window}m" for window in rolling_windows]
        + [f"rps_roll_max_{window}m" for window in rolling_windows]
        + ["hour_of_day", "day_of_week", "is_weekend"]
    )

    X = model_df[feature_columns]
    y = model_df["target_rps_t_plus_5m"]

    print(f"Service: {service_name}")
    print(f"X shape: {X.shape}, y shape: {y.shape}")
    print(f"Feature names: {feature_columns}")

    return X, y
