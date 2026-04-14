from pathlib import Path

import matplotlib.pyplot as plt
import numpy as np
import pandas as pd

# Simulation window: 1-minute samples over 21 days.
MINUTES_PER_DAY = 24 * 60
TOTAL_DAYS = 21
TOTAL_POINTS = TOTAL_DAYS * MINUTES_PER_DAY

# Service-level traffic behavior knobs to make each curve feel distinct.
SERVICE_CONFIG = {
    "api-gateway": {
        "baseline": 110,
        "morning_amp": 300,
        "morning_width": 1.7,
        "evening_amp": 420,
        "evening_width": 2.1,
        "noise_std": 14,
        "weekend_scale": 0.78,
    },
    "order-service": {
        "baseline": 55,
        "morning_amp": 170,
        "morning_width": 1.9,
        "evening_amp": 250,
        "evening_width": 2.3,
        "noise_std": 10,
        "weekend_scale": 0.83,
    },
    "payment-service": {
        "baseline": 35,
        "morning_amp": 115,
        "morning_width": 1.4,
        "evening_amp": 175,
        "evening_width": 1.9,
        "noise_std": 8,
        "weekend_scale": 0.88,
    },
}

# One sudden spike event per service, each at a different day/time.
SPIKE_EVENTS = {
    "api-gateway": {"day": 5, "hour": 18, "minute": 42, "duration_min": 50, "spike_gain": 650},
    "order-service": {"day": 11, "hour": 9, "minute": 12, "duration_min": 40, "spike_gain": 420},
    "payment-service": {"day": 17, "hour": 19, "minute": 27, "duration_min": 35, "spike_gain": 300},
}


def gaussian_peak(x: np.ndarray, center: float, width: float) -> np.ndarray:
    """Return a Gaussian-shaped peak centered around a given hour."""
    return np.exp(-0.5 * ((x - center) / width) ** 2)


def generate_service_rps(
    timestamps: pd.DatetimeIndex,
    cfg: dict[str, float],
    event: dict[str, int],
    rng: np.random.Generator,
) -> np.ndarray:
    """Build a realistic RPS curve using daily peaks, weekend effects, noise, and a sudden spike."""
    # Convert to NumPy arrays so downstream math stays mutable.
    hour_of_day = timestamps.hour.to_numpy(dtype=float) + timestamps.minute.to_numpy(dtype=float) / 60.0

    # Daily shape with peaks around 9am and 7pm.
    morning_load = cfg["morning_amp"] * gaussian_peak(hour_of_day, center=9.0, width=cfg["morning_width"])
    evening_load = cfg["evening_amp"] * gaussian_peak(hour_of_day, center=19.0, width=cfg["evening_width"])
    base_load = cfg["baseline"] + morning_load + evening_load

    # Weekend traffic is slightly lower than weekdays.
    weekend_multiplier = np.where(timestamps.dayofweek.to_numpy() >= 5, cfg["weekend_scale"], 1.0)
    rps = np.asarray(base_load * weekend_multiplier, dtype=float).copy()

    # Add random minute-level noise.
    rps += rng.normal(loc=0.0, scale=cfg["noise_std"], size=len(timestamps))

    # Inject one sudden spike that starts high and decays quickly.
    event_start = timestamps[0] + pd.Timedelta(
        days=event["day"], hours=event["hour"], minutes=event["minute"]
    )
    event_end = event_start + pd.Timedelta(minutes=event["duration_min"])
    spike_mask = np.asarray((timestamps >= event_start) & (timestamps < event_end), dtype=bool)
    if spike_mask.any():
        spike_profile = np.exp(-np.linspace(0, 4, spike_mask.sum()))
        rps[spike_mask] += event["spike_gain"] * spike_profile

    return np.clip(rps, a_min=1.0, a_max=None).round(2)


def main() -> None:
    # Create the 21-day, 1-minute timeline.
    start_ts = pd.Timestamp("2026-01-01 00:00:00")
    timestamps = pd.date_range(start=start_ts, periods=TOTAL_POINTS, freq="min")

    # Build one CSV per service.
    output_dir = Path(__file__).resolve().parent / "generated_data"
    output_dir.mkdir(parents=True, exist_ok=True)

    rng = np.random.default_rng(42)
    service_data: dict[str, pd.DataFrame] = {}

    for service_name, cfg in SERVICE_CONFIG.items():
        rps = generate_service_rps(
            timestamps=timestamps,
            cfg=cfg,
            event=SPIKE_EVENTS[service_name],
            rng=np.random.default_rng(rng.integers(0, 1_000_000)),
        )

        df = pd.DataFrame(
            {
                "timestamp": timestamps,
                "service_name": service_name,
                "rps": rps,
            }
        )
        service_data[service_name] = df
        df.to_csv(output_dir / f"{service_name}.csv", index=False)

    # Plot all services on one graph and save it.
    plt.figure(figsize=(16, 7))
    for service_name, df in service_data.items():
        plt.plot(df["timestamp"], df["rps"], label=service_name, linewidth=1)

    plt.title("Synthetic RPS Forecasting Dataset (1-min resolution, 21 days)")
    plt.xlabel("Timestamp")
    plt.ylabel("Requests per second (RPS)")
    plt.legend()
    plt.grid(alpha=0.25)
    plt.tight_layout()
    plt.savefig(output_dir / "all-services-rps.png", dpi=150)
    plt.close()

    print(f"Synthetic data generated in: {output_dir}")


if __name__ == "__main__":
    main()
