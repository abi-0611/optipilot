# OptiPilot Forecaster

Python gRPC service for metric ingestion and model lifecycle in OptiPilot.

Current behavior:
- `IngestMetrics` persists incoming metrics to SQLite.
- `TriggerRetrain` trains LightGBM quantile models (p50/p90), stores model artifacts, and applies promotion policy.
- Other RPCs still return placeholders.

## Prerequisites

- [uv](https://github.com/astral-sh/uv)

## Setup

```bash
cd forecaster

# Regenerate shared Python stubs under ../gen/python (optional unless proto changed)
bash scripts/sync_proto.sh

# Install dependencies
uv sync
```

## Running

```bash
cd forecaster
uv run forecaster
```

Config defaults come from `forecaster.yaml`.

## Seeding local training data

```bash
cd forecaster
uv run python scripts/seed_test_data.py --clear-existing
```

Useful flags:
- `--minutes 4320` (3 days per service)
- `--services api-gateway,order-service`
- `--metrics-db /tmp/forecaster_metrics.db`

## Triggering training manually

```bash
grpcurl -plaintext -d '{"service_name":"api-gateway"}' \
  localhost:50051 optipilot.v1.OptiPilotService/TriggerRetrain
```

Model artifacts are written to `training.models_dir` (default: `models/`).

## Protos & codegen (single source of truth)

This repo uses a **single canonical proto source** and a **single shared generated output**:

- **Proto source (authoritative):** `../proto/optipilot/v1/*.proto`
- **Generated Python stubs (shared):** `../gen/python/optipilot/v1/`

### Duplicate-proto refactor

Forecaster previously had a second copy of generated stubs under `forecaster/src/forecaster/proto/`.
That directory has been **removed** to prevent drift and “two sources of truth”.

Forecaster now imports stubs directly from the shared output:

```py
from optipilot.v1 import prediction_pb2, prediction_pb2_grpc
```

To make that work when running `uv run forecaster`, the entrypoint (`src/forecaster/__main__.py`)
prepends `../gen/python` to `sys.path` at startup.

If you want the same imports to work in your shell/IDE when running standalone scripts, set:

```bash
export PYTHONPATH="../gen/python"
```

### Regenerating stubs

From repo root:

```bash
make -C proto python
```

From `forecaster/`:

```bash
bash scripts/sync_proto.sh
```
