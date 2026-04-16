# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

OptiPilot is a Kubernetes-native **predictive autoscaling system** with two main processes:

- **Controller** (`controller/`) — Go binary. Discovers services, scrapes Prometheus metrics, persists them to SQLite, and will eventually call the forecaster gRPC service to make scaling decisions.
- **Forecaster** (`forecaster/`) — Python service. Receives metrics from the controller via gRPC, stores training data, and will eventually serve ML-based replica predictions. Currently returns stub responses (except `IngestMetrics`, which fully persists data).

The gRPC contract between them lives in `proto/optipilot/v1/prediction.proto`. Generated stubs are committed at `gen/go/` and `gen/python/`.

## Controller (Go)

```bash
cd controller
go build ./...
go test ./...
go test ./internal/config/...           # single package
go run . -config optipilot.yaml
```

Config is read from `optipilot.yaml` (or `OPTIPILOT_CONFIG` env var). All structured logging uses `log/slog` with JSON output. The binary currently runs the collector loop (Prometheus scrape → SQLite) and a purge loop. Discovery, gRPC client, Kubernetes actuator, and HTTP server are stubs or not yet wired.

**Controller env var overrides** (narrow set, not all fields are overridable):
- `OPTIPILOT_PROMETHEUS_ADDRESS`, `OPTIPILOT_DISCOVERY_MODE`, `OPTIPILOT_SCALING_MODE`
- `OPTIPILOT_STORAGE_DB_PATH`, `OPTIPILOT_FORECASTER_GRPC_ADDRESS`, `OPTIPILOT_SERVER_HTTP_PORT`

**Scaling modes**: `shadow` (log only) → `recommend` (advisory) → `autonomous` (full control).

## Forecaster (Python)

```bash
cd forecaster

# First-time setup — copies and patches proto stubs into the package
bash scripts/sync_proto.sh

uv sync                    # installs deps (requires grpcio==1.80.0)
uv sync --extra dev        # also installs pytest

uv run forecaster          # starts gRPC server on :50051
uv run pytest              # run all tests
uv run pytest tests/test_config.py  # single file
```

Config is read from `forecaster.yaml` (or `OPTIPILOT_FC_CONFIG` env var). JSON structured logging by default.

**Forecaster env var overrides** (pattern: `OPTIPILOT_FC_<SECTION>_<FIELD>`):
- `OPTIPILOT_FC_SERVER_PORT`, `OPTIPILOT_FC_STORAGE_REGISTRY_DB`, `OPTIPILOT_FC_LOGGING_FORMAT`, etc.

## Proto / codegen

```bash
make -C proto all       # regenerate both Go and Python stubs
make -C proto go        # Go only
make -C proto python    # Python only

# After regenerating Python stubs, sync them into the forecaster package:
cd forecaster && bash scripts/sync_proto.sh
```

`sync_proto.sh` copies `gen/python/optipilot/` into `src/forecaster/proto/optipilot/` and rewrites the import path in `prediction_pb2_grpc.py` from `optipilot.v1` → `optipilot.v1` resolved via `sys.path`. Do not edit files under `src/forecaster/proto/` manually — they are generated.

**Proto import convention in the forecaster**: `__main__.py` inserts `gen/python` onto `sys.path`, so all proto imports use `from optipilot.v1 import prediction_pb2, prediction_pb2_grpc` (not a `forecaster.proto.*` path).

## Key architectural constraints

**Service-agnostic forecaster**: The forecaster has no knowledge of specific service names. It learns and serves for whatever service names the controller sends. Service names only appear in the controller's `optipilot.yaml` static config.

**Two SQLite databases** in the forecaster:
- `forecaster_registry.db` — model versions and promotion state (one promoted model per service at a time)
- `forecaster_metrics.db` — raw training data ingested from the controller

**One SQLite database** in the controller:
- `optipilot.db` — service metrics snapshots, scaling decisions, model status cache

**gRPC stub status**: `GetPrediction`, `GetModelStatus`, `GetAllServicesStatus`, `GetServiceMetricsHistory`, `TriggerRetrain` all return placeholder data. Only `IngestMetrics` is fully implemented (stores data). ML training/inference is the next layer.

## Fake services for local dev

Three Go HTTP services in `services/` (api-gateway :8081, order-service :8082, payment-service :8083) expose Prometheus metrics. Run them with:
```bash
docker compose up
```
or build individually: `cd services/api-gateway && go build .`

The controller's static discovery points at these ports in `optipilot.yaml`.
