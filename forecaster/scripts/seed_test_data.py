#!/usr/bin/env python3
"""
Seed synthetic training metrics into the forecaster metrics database.

The generated series is minute-granularity and includes daily/weekly seasonality,
light trend, and noise so TriggerRetrain has realistic-looking data to train on.
"""

from __future__ import annotations

import argparse
import asyncio
import math
import random
from datetime import datetime, timedelta, timezone

from forecaster.config import load_config
from forecaster.models import ServiceMetric
from forecaster.storage.metrics import MetricsStore


DEFAULT_SERVICES = ["api-gateway", "order-service", "payment-service"]


def _clamp(value: float, low: float, high: float) -> float:
    return max(low, min(high, value))


def _service_phase(service_name: str) -> float:
    # Stable phase offset per service so the waves aren't perfectly aligned.
    return float(sum(ord(c) for c in service_name) % 360)


def _service_base_rps(service_name: str) -> float:
    known = {
        "api-gateway": 160.0,
        "order-service": 110.0,
        "payment-service": 90.0,
    }
    return known.get(service_name, 100.0)


def _generate_metrics(
    service_name: str,
    start_ts: datetime,
    minutes: int,
    rng: random.Random,
) -> list[ServiceMetric]:
    points: list[ServiceMetric] = []
    phase = _service_phase(service_name)
    base_rps = _service_base_rps(service_name)

    for i in range(minutes):
        ts = start_ts + timedelta(minutes=i)
        minute_of_day = ts.hour * 60 + ts.minute
        day_of_week = ts.weekday()

        daily = 0.35 * math.sin((2.0 * math.pi * minute_of_day / 1440.0) + phase)
        weekly = 0.12 * math.sin((2.0 * math.pi * day_of_week / 7.0) + phase / 2.0)
        trend = 0.0008 * i
        noise = rng.gauss(0.0, 0.06)

        load_multiplier = max(0.25, 1.0 + daily + weekly + trend + noise)
        rps = max(1.0, base_rps * load_multiplier)

        utilization = rps / max(base_rps, 1.0)
        cpu_usage = _clamp(18.0 + (utilization * 36.0) + rng.gauss(0.0, 3.0), 1.0, 98.0)
        memory_mb = max(128.0, 280.0 + (utilization * 260.0) + rng.gauss(0.0, 12.0))
        error_rate = _clamp(
            0.002 + max(0.0, utilization - 1.0) * 0.01 + abs(rng.gauss(0.0, 0.0015)),
            0.0,
            0.2,
        )

        avg_latency_ms = max(2.0, 8.0 + utilization * 14.0 + rng.gauss(0.0, 1.2))
        p99_latency_ms = max(avg_latency_ms + 4.0, avg_latency_ms * 2.4 + rng.gauss(0.0, 5.0))
        active_connections = max(1, int(rps * 0.55 + rng.gauss(0.0, 4.0)))

        points.append(
            ServiceMetric(
                service_name=service_name,
                rps=float(rps),
                avg_latency_ms=float(avg_latency_ms),
                p99_latency_ms=float(p99_latency_ms),
                active_connections=int(active_connections),
                cpu_usage_percent=float(cpu_usage),
                memory_usage_mb=float(memory_mb),
                error_rate=float(error_rate),
                timestamp=ts,
            )
        )

    return points


async def _seed(args: argparse.Namespace) -> None:
    cfg = load_config(args.config)
    metrics_db = args.metrics_db or cfg.storage.metrics_db
    services = [s.strip() for s in args.services.split(",") if s.strip()]
    if not services:
        raise ValueError("no services provided")

    now = datetime.now(tz=timezone.utc).replace(second=0, microsecond=0)
    start_ts = now - timedelta(minutes=args.minutes)
    rng = random.Random(args.seed)

    store = MetricsStore(metrics_db)
    await store.initialize()
    try:
        if args.clear_existing:
            await store.delete_services(services)

        payload: list[ServiceMetric] = []
        for service in services:
            payload.extend(_generate_metrics(service, start_ts, args.minutes, rng))

        inserted = await store.insert_batch(payload)
        print(
            f"Seeded {inserted} rows into {metrics_db} "
            f"for services={services} window=[{start_ts.isoformat()}..{now.isoformat()}]"
        )
    finally:
        await store.close()


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Seed synthetic forecaster metrics.")
    parser.add_argument(
        "--config",
        default="forecaster.yaml",
        help="Path to forecaster config (default: forecaster.yaml)",
    )
    parser.add_argument(
        "--metrics-db",
        default="",
        help="Override metrics DB path (default: from config)",
    )
    parser.add_argument(
        "--services",
        default=",".join(DEFAULT_SERVICES),
        help="Comma-separated service names to seed",
    )
    parser.add_argument(
        "--minutes",
        type=int,
        default=2880,
        help="Number of minutes to generate per service (default: 2880)",
    )
    parser.add_argument(
        "--seed",
        type=int,
        default=42,
        help="Random seed for reproducible generation (default: 42)",
    )
    parser.add_argument(
        "--clear-existing",
        action="store_true",
        help="Delete existing rows for selected services before seeding",
    )

    args = parser.parse_args()
    if args.minutes <= 0:
        parser.error("--minutes must be > 0")
    return args


def main() -> None:
    asyncio.run(_seed(_parse_args()))


if __name__ == "__main__":
    main()
