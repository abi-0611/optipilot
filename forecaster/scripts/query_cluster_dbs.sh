#!/usr/bin/env bash
set -euo pipefail

# Query forecaster SQLite databases inside a Kubernetes deployment.
#
# Default target:
#   namespace:  optipilot-system
#   deployment: optipilot-forecaster
#
# Example:
#   bash forecaster/scripts/query_cluster_dbs.sh
#   bash forecaster/scripts/query_cluster_dbs.sh -n optipilot-system -d optipilot-forecaster

NAMESPACE="optipilot-system"
DEPLOYMENT="optipilot-forecaster"
CONTAINER=""
DATA_DIR="/data"
SHOW_SAMPLE_ROWS=5

usage() {
  cat <<'EOF'
Usage: query_cluster_dbs.sh [options]

Options:
  -n, --namespace <name>     Kubernetes namespace (default: optipilot-system)
  -d, --deployment <name>    Forecaster deployment name (default: optipilot-forecaster)
  -c, --container <name>     Container name (optional)
      --data-dir <path>      Mounted data directory in container (default: /data)
      --sample-rows <n>      Number of sample rows for latest metrics (default: 5)
  -h, --help                 Show this help

What it does:
  1. Verifies pod/deployment access.
  2. Confirms DB files exist.
  3. Prints table list and row counts for:
     - forecaster_metrics.db
     - forecaster_registry.db
  4. Prints useful summaries (metrics by service, latest metrics, model status, promoted models).
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
      --sample-rows)
        [[ $# -ge 2 ]] || fail "missing value for $1"
        SHOW_SAMPLE_ROWS="$2"
        shift 2
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

  [[ "$SHOW_SAMPLE_ROWS" =~ ^[0-9]+$ ]] || fail "--sample-rows must be a non-negative integer"
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

  echo "== OptiPilot Forecaster DB Query =="
  echo "Namespace:  $NAMESPACE"
  echo "Deployment: $DEPLOYMENT"
  if [[ -n "$CONTAINER" ]]; then
    echo "Container:  $CONTAINER"
  fi
  echo "Data dir:   $DATA_DIR"
  echo

  echo "[1/5] Checking deployment exists..."
  kubectl -n "$NAMESPACE" get deploy "$DEPLOYMENT" >/dev/null

  echo "[2/5] Listing pods for deployment..."
  kubectl -n "$NAMESPACE" get pods -l "app=$DEPLOYMENT" -o wide || true
  echo

  echo "[3/5] Verifying DB files in $DATA_DIR..."
  kubectl_exec sh -lc "ls -lah '$DATA_DIR'"
  echo

  echo "[4/5] Inspecting schema and row counts..."
  kubectl_exec python - "$DATA_DIR" <<'PY'
import sqlite3
import sys
from pathlib import Path

data_dir = Path(sys.argv[1])
dbs = [
    data_dir / "forecaster_metrics.db",
    data_dir / "forecaster_registry.db",
]

for db_path in dbs:
    print(f"=== {db_path} ===")
    if not db_path.exists():
        print("missing")
        print()
        continue

    conn = sqlite3.connect(str(db_path))
    cur = conn.cursor()

    tables = [r[0] for r in cur.execute(
        "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"
    )]
    print("tables:", tables)

    for table in tables:
        if table.startswith("sqlite_"):
            continue
        count = cur.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
        print(f"{table}: {count} rows")

    conn.close()
    print()
PY

  echo "[5/5] Running focused queries..."
  kubectl_exec python - "$DATA_DIR" "$SHOW_SAMPLE_ROWS" <<'PY'
import sqlite3
import sys
from pathlib import Path

data_dir = Path(sys.argv[1])
sample_rows = int(sys.argv[2])

metrics_db = data_dir / "forecaster_metrics.db"
registry_db = data_dir / "forecaster_registry.db"

print("=== metrics summary ===")
if metrics_db.exists():
    conn = sqlite3.connect(str(metrics_db))
    cur = conn.cursor()

    print("metrics rows by service:")
    try:
        for row in cur.execute(
            "SELECT service_name, COUNT(*) FROM metrics GROUP BY service_name ORDER BY service_name"
        ):
            print(row)
    except sqlite3.OperationalError as exc:
        print(f"query failed: {exc}")

    print("\nlatest metrics (up to sample_rows):")
    try:
        for row in cur.execute(
            """
            SELECT service_name, timestamp, rps, avg_latency_ms, error_rate
            FROM metrics
            ORDER BY timestamp DESC
            LIMIT ?
            """,
            (sample_rows,),
        ):
            print(row)
    except sqlite3.OperationalError as exc:
        print(f"query failed: {exc}")

    print("\nrecent predictions (up to sample_rows):")
    try:
        for row in cur.execute(
            """
            SELECT service_name, predicted_at, horizon_min, predicted_rps_p50, predicted_rps_p90, model_version
            FROM predictions
            ORDER BY predicted_at DESC
            LIMIT ?
            """,
            (sample_rows,),
        ):
            print(row)
    except sqlite3.OperationalError as exc:
        print(f"query failed: {exc}")

    conn.close()
else:
    print(f"missing: {metrics_db}")

print("\n=== registry summary ===")
if registry_db.exists():
    conn = sqlite3.connect(str(registry_db))
    cur = conn.cursor()

    print("model_status:")
    try:
        for row in cur.execute(
            """
            SELECT service_name, model_version, current_mape, rolling_mape_3h, degraded_to, updated_at
            FROM model_status
            ORDER BY service_name
            """
        ):
            print(row)
    except sqlite3.OperationalError as exc:
        print(f"query failed: {exc}")

    print("\npromoted models:")
    try:
        for row in cur.execute(
            """
            SELECT service_name, version, validation_mape, created_at, file_path
            FROM models
            WHERE is_promoted = 1
            ORDER BY service_name
            """
        ):
            print(row)
    except sqlite3.OperationalError as exc:
        print(f"query failed: {exc}")

    conn.close()
else:
    print(f"missing: {registry_db}")
PY

  echo
  echo "Done."
}

main "$@"
