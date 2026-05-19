#!/usr/bin/env bash
set -euo pipefail

# Seed synthetic metrics directly into the forecaster SQLite metrics DB in-cluster.
#
# Default target:
#   namespace:  optipilot-system
#   deployment: optipilot-forecaster
#   services:   api-gateway,order-service,payment-service
#
# Example:
#   bash forecaster/scripts/seed_cluster_forecaster_data.sh --clear-existing --minutes 2880

NAMESPACE="optipilot-system"
DEPLOYMENT="optipilot-forecaster"
CONTAINER=""
DATA_DIR="/data"
SERVICES="api-gateway,order-service,payment-service"
MINUTES=2880
SEED=42
CLEAR_EXISTING=0

usage() {
  cat <<'EOF'
Usage: seed_cluster_forecaster_data.sh [options]

Options:
  -n, --namespace <name>      Kubernetes namespace (default: optipilot-system)
  -d, --deployment <name>     Forecaster deployment name (default: optipilot-forecaster)
  -c, --container <name>      Container name (optional)
      --data-dir <path>       Mounted data directory in container (default: /data)
      --services <csv>        Comma-separated services (default: api-gateway,order-service,payment-service)
      --minutes <n>           Minutes of data per service (default: 2880)
      --seed <n>              Random seed for repeatability (default: 42)
      --clear-existing        Delete existing metrics/predictions for selected services first
  -h, --help                  Show this help

What it does:
  1) Connects to /data/forecaster_metrics.db inside forecaster pod
  2) Optionally clears existing rows for selected services
  3) Inserts synthetic minute-level metrics for selected services
  4) Prints inserted count + per-service totals
EOF
}

fail() {
  echo "[error] $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -n|--namespace)
        [[ $# -ge 2 ]] || fail "missing value for $1"
        NAMESPACE="$2"
        shift 2
        ;;
      -d|--deployment)
        [[ $# -ge 2 ]] || fail "missing value for $1"
        DEPLOYMENT="$2"
        shift 2
        ;;
      -c|--container)
        [[ $# -ge 2 ]] || fail "missing value for $1"
        CONTAINER="$2"
        shift 2
        ;;
      --data-dir)
        [[ $# -ge 2 ]] || fail "missing value for $1"
        DATA_DIR="$2"
        shift 2
        ;;
      --services)
        [[ $# -ge 2 ]] || fail "missing value for $1"
        SERVICES="$2"
        shift 2
        ;;
      --minutes)
        [[ $# -ge 2 ]] || fail "missing value for $1"
        MINUTES="$2"
        shift 2
        ;;
      --seed)
        [[ $# -ge 2 ]] || fail "missing value for $1"
        SEED="$2"
        shift 2
        ;;
      --clear-existing)
        CLEAR_EXISTING=1
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "unknown argument: $1"
        ;;
    esac
  done

  [[ "$MINUTES" =~ ^[0-9]+$ ]] || fail "--minutes must be a non-negative integer"
  [[ "$SEED" =~ ^[0-9]+$ ]] || fail "--seed must be a non-negative integer"
  (( MINUTES > 0 )) || fail "--minutes must be > 0"
  [[ -n "$SERVICES" ]] || fail "--services must not be empty"
}

kubectl_exec() {
  if [[ -n "$CONTAINER" ]]; then
    kubectl -n "$NAMESPACE" exec -i "deploy/$DEPLOYMENT" -c "$CONTAINER" -- "$@"
  else
    kubectl -n "$NAMESPACE" exec -i "deploy/$DEPLOYMENT" -- "$@"
  fi
}

main() {
  parse_args "$@"
  require_cmd kubectl

  echo "== OptiPilot Forecaster Cluster Seeder =="
  echo "Namespace:      $NAMESPACE"
  echo "Deployment:     $DEPLOYMENT"
  if [[ -n "$CONTAINER" ]]; then
    echo "Container:      $CONTAINER"
  fi
  echo "Data dir:       $DATA_DIR"
  echo "Services:       $SERVICES"
  echo "Minutes/service: $MINUTES"
  echo "Seed:           $SEED"
  echo "Clear existing: $CLEAR_EXISTING"
  echo

  echo "[1/3] Checking deployment access..."
  kubectl -n "$NAMESPACE" get deploy "$DEPLOYMENT" >/dev/null

  echo "[2/3] Seeding metrics in pod..."
  kubectl_exec python - "$DATA_DIR" "$SERVICES" "$MINUTES" "$SEED" "$CLEAR_EXISTING" <<'PY'
import math
import random
import sqlite3
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path


def clamp(v: float, lo: float, hi: float) -> float:
    return max(lo, min(hi, v))


def service_phase(name: str) -> float:
    return float(sum(ord(c) for c in name) % 360)


def service_base_rps(name: str) -> float:
    known = {
        "api-gateway": 160.0,
        "order-service": 110.0,
        "payment-service": 90.0,
    }
    return known.get(name, 100.0)


def build_rows(service_name: str, start_ts: datetime, minutes: int, rng: random.Random):
    rows = []
    phase = service_phase(service_name)
    base_rps = service_base_rps(service_name)

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
        cpu_usage = clamp(18.0 + (utilization * 36.0) + rng.gauss(0.0, 3.0), 1.0, 98.0)
        memory_mb = max(128.0, 280.0 + (utilization * 260.0) + rng.gauss(0.0, 12.0))
        error_rate = clamp(
            0.002 + max(0.0, utilization - 1.0) * 0.01 + abs(rng.gauss(0.0, 0.0015)),
            0.0,
            0.2,
        )

        avg_latency_ms = max(2.0, 8.0 + utilization * 14.0 + rng.gauss(0.0, 1.2))
        p99_latency_ms = max(avg_latency_ms + 4.0, avg_latency_ms * 2.4 + rng.gauss(0.0, 5.0))
        active_connections = max(1, int(rps * 0.55 + rng.gauss(0.0, 4.0)))

        rows.append(
            (
                service_name,
                float(rps),
                float(avg_latency_ms),
                float(p99_latency_ms),
                int(active_connections),
                float(cpu_usage),
                float(memory_mb),
                float(error_rate),
                ts.isoformat(),
            )
        )

    return rows


def main() -> None:
    data_dir = Path(sys.argv[1])
    services = [s.strip() for s in sys.argv[2].split(",") if s.strip()]
    minutes = int(sys.argv[3])
    seed = int(sys.argv[4])
    clear_existing = bool(int(sys.argv[5]))

    if not services:
        raise SystemExit("no services provided")

    db_path = data_dir / "forecaster_metrics.db"
    if not db_path.exists():
        raise SystemExit(f"metrics db not found: {db_path}")

    now = datetime.now(timezone.utc).replace(second=0, microsecond=0)
    start_ts = now - timedelta(minutes=minutes)
    rng = random.Random(seed)

    conn = sqlite3.connect(str(db_path))
    cur = conn.cursor()

    placeholders = ",".join("?" for _ in services)
    if clear_existing:
        cur.execute(f"DELETE FROM predictions WHERE service_name IN ({placeholders})", services)
        deleted_predictions = cur.rowcount if cur.rowcount is not None else 0
        cur.execute(f"DELETE FROM metrics WHERE service_name IN ({placeholders})", services)
        deleted_metrics = cur.rowcount if cur.rowcount is not None else 0
        print(f"cleared rows: metrics={deleted_metrics}, predictions={deleted_predictions}")

    payload = []
    for service in services:
        payload.extend(build_rows(service, start_ts, minutes, rng))

    cur.executemany(
        """
        INSERT INTO metrics
          (service_name, rps, avg_latency_ms, p99_latency_ms,
           active_connections, cpu_usage_percent, memory_usage_mb,
           error_rate, timestamp)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
        payload,
    )

    conn.commit()

    print(
        f"seeded rows: {len(payload)} | db={db_path} | "
        f"window=[{start_ts.isoformat()}..{now.isoformat()}]"
    )

    print("totals per service:")
    for row in cur.execute(
        "SELECT service_name, COUNT(*) FROM metrics GROUP BY service_name ORDER BY service_name"
    ):
        print(row)

    conn.close()


if __name__ == "__main__":
    main()
PY

  echo "[3/3] Showing latest points after seed..."
  kubectl_exec python - "$DATA_DIR" <<'PY'
import sqlite3
import sys
from pathlib import Path

metrics_db = Path(sys.argv[1]) / "forecaster_metrics.db"
conn = sqlite3.connect(str(metrics_db))
cur = conn.cursor()

for row in cur.execute(
    """
    SELECT service_name, timestamp, rps, avg_latency_ms, error_rate
    FROM metrics
    ORDER BY timestamp DESC
    LIMIT 9
    """
):
    print(row)

conn.close()
PY

  echo
  echo "Done."
}

main "$@"
