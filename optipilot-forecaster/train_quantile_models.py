from argparse import ArgumentParser
from pathlib import Path

import joblib
import lightgbm as lgb
import matplotlib.pyplot as plt
import pandas as pd

from feature_engineering import build_features

# Two days of minute-level data reserved for final test evaluation.
TEST_WINDOW_MINUTES = 2 * 24 * 60

# Last one day of training window used for early stopping validation.
VALIDATION_WINDOW_MINUTES = 24 * 60


def time_ordered_split(
    X: pd.DataFrame, y: pd.Series, test_window_minutes: int = TEST_WINDOW_MINUTES
) -> tuple[pd.DataFrame, pd.DataFrame, pd.Series, pd.Series]:
    """Split X/y with the newest rows as test data (no shuffling)."""
    if len(X) != len(y):
        raise ValueError("X and y must have the same number of rows.")
    if len(X) <= test_window_minutes:
        raise ValueError("Not enough rows to carve out the requested test window.")

    split_index = len(X) - test_window_minutes
    X_train = X.iloc[:split_index]
    X_test = X.iloc[split_index:]
    y_train = y.iloc[:split_index]
    y_test = y.iloc[split_index:]
    return X_train, X_test, y_train, y_test


def train_validation_split(
    X_train_full: pd.DataFrame,
    y_train_full: pd.Series,
    validation_window_minutes: int = VALIDATION_WINDOW_MINUTES,
) -> tuple[pd.DataFrame, pd.DataFrame, pd.Series, pd.Series]:
    """Split training data into fit/validation chunks for early stopping."""
    if len(X_train_full) != len(y_train_full):
        raise ValueError("Training X and y must have the same number of rows.")
    if len(X_train_full) <= validation_window_minutes:
        raise ValueError("Not enough training rows to carve out validation data.")

    split_index = len(X_train_full) - validation_window_minutes
    X_train = X_train_full.iloc[:split_index]
    X_val = X_train_full.iloc[split_index:]
    y_train = y_train_full.iloc[:split_index]
    y_val = y_train_full.iloc[split_index:]
    return X_train, X_val, y_train, y_val


def train_quantile_model(
    X_train: pd.DataFrame,
    y_train: pd.Series,
    X_val: pd.DataFrame,
    y_val: pd.Series,
    alpha: float,
) -> lgb.Booster:
    """Train one LightGBM quantile model with early stopping."""
    train_data = lgb.Dataset(
        data=X_train,
        label=y_train,
        feature_name=list(X_train.columns),
    )
    val_data = lgb.Dataset(
        data=X_val,
        label=y_val,
        reference=train_data,
    )

    params = {
        "objective": "quantile",
        "alpha": alpha,
        "metric": "l1",
        "learning_rate": 0.03,
        "num_leaves": 63,
        "feature_fraction": 0.9,
        "bagging_fraction": 0.9,
        "bagging_freq": 1,
        "seed": 42,
        "verbosity": -1,
    }

    model = lgb.train(
        params=params,
        train_set=train_data,
        num_boost_round=2000,
        valid_sets=[val_data],
        callbacks=[lgb.early_stopping(stopping_rounds=100, verbose=False)],
    )
    return model


def mean_absolute_percentage_error(y_true: pd.Series, y_pred: pd.Series) -> float:
    """Compute MAPE in percentage units."""
    denominator = y_true.abs().clip(lower=1e-6)
    return (((y_true - y_pred).abs() / denominator).mean()) * 100.0


def plot_predictions(
    y_test: pd.Series,
    pred_p50: pd.Series,
    pred_p90: pd.Series,
    service_name: str,
    output_path: Path,
) -> None:
    """Plot actual RPS and quantile predictions on the test set."""
    x_axis = y_test.index

    plt.figure(figsize=(16, 6))
    plt.plot(x_axis, y_test.values, label="actual_rps", linewidth=1.3)
    plt.plot(x_axis, pred_p50.values, label="predicted_p50", linewidth=1.2)
    plt.plot(x_axis, pred_p90.values, label="predicted_p90", linewidth=1.2)

    plt.title(f"Test Forecasts for {service_name} (5-min ahead)")
    plt.xlabel("Timestamp")
    plt.ylabel("RPS")
    plt.legend()
    plt.grid(alpha=0.25)
    plt.tight_layout()
    plt.savefig(output_path, dpi=150)
    plt.close()


def main() -> None:
    parser = ArgumentParser(description="Train p50/p90 LightGBM quantile models for one microservice.")
    parser.add_argument("--service-name", default="api-gateway", help="Service to train on.")
    parser.add_argument("--data-dir", default="generated_data", help="Directory that contains service CSV files.")
    parser.add_argument("--output-dir", default=".", help="Directory to save model files and plot.")
    args = parser.parse_args()

    # Load one service CSV and build features/target.
    data_path = Path(args.data_dir) / f"{args.service_name}.csv"
    if not data_path.exists():
        raise FileNotFoundError(f"Service CSV not found: {data_path}")

    df = pd.read_csv(data_path)
    X, y = build_features(df=df, service_name=args.service_name)

    # Time-ordered split: newest 2 days are test, older data is training.
    X_train_full, X_test, y_train_full, y_test = time_ordered_split(X=X, y=y)

    # Hold out the newest day inside training for early stopping.
    X_train, X_val, y_train, y_val = train_validation_split(X_train_full=X_train_full, y_train_full=y_train_full)

    # Train quantile models for median demand (p50) and safe upper bound (p90).
    model_p50 = train_quantile_model(X_train=X_train, y_train=y_train, X_val=X_val, y_val=y_val, alpha=0.5)
    model_p90 = train_quantile_model(X_train=X_train, y_train=y_train, X_val=X_val, y_val=y_val, alpha=0.9)

    # Predict on test set.
    pred_p50 = pd.Series(
        model_p50.predict(X_test, num_iteration=model_p50.best_iteration),
        index=y_test.index,
        name="pred_p50",
    )
    pred_p90 = pd.Series(
        model_p90.predict(X_test, num_iteration=model_p90.best_iteration),
        index=y_test.index,
        name="pred_p90",
    )

    # Evaluate p50 model with MAE and MAPE.
    mae_p50 = (y_test - pred_p50).abs().mean()
    mape_p50 = mean_absolute_percentage_error(y_true=y_test, y_pred=pred_p50)
    print(f"Service: {args.service_name}")
    print(f"Test rows: {len(y_test)}")
    print(f"p50 MAE: {mae_p50:.4f}")
    print(f"p50 MAPE: {mape_p50:.4f}%")

    # Save trained models for downstream autoscaling logic.
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)

    p50_model_path = output_dir / f"model_p50_{args.service_name}.pkl"
    p90_model_path = output_dir / f"model_p90_{args.service_name}.pkl"
    joblib.dump(model_p50, p50_model_path)
    joblib.dump(model_p90, p90_model_path)

    # Save comparison plot on test predictions.
    plot_path = output_dir / f"test_predictions_{args.service_name}.png"
    plot_predictions(
        y_test=y_test,
        pred_p50=pred_p50,
        pred_p90=pred_p90,
        service_name=args.service_name,
        output_path=plot_path,
    )

    print(f"Saved: {p50_model_path}")
    print(f"Saved: {p90_model_path}")
    print(f"Saved: {plot_path}")


if __name__ == "__main__":
    main()
