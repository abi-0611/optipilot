# Copilot Instructions for OptiPilot

## Build, test, and lint commands

### Controller (Go)
```bash
cd controller
go build ./...
go test ./...
go test ./internal/config/...   # package-level test run
go run . -config optipilot.yaml
```

### Forecaster (Python, uv)
```bash
cd forecaster
uv sync
uv sync --extra dev
uv run forecaster
uv run pytest
uv run python scripts/test_retrain.py   # targeted retrain flow check
```

### Proto code generation
```bash
make -C proto all
make -C proto go
make -C proto python
cd forecaster && bash scripts/sync_proto.sh
```

### Frontend (Next.js)
```bash
cd optipilot-frontend
npm run dev
npm run build
npm run lint
```

### MCP (workspace)
```bash
# Playwright MCP is configured in .vscode/mcp.json
npx -y @microsoft/mcp-server-playwright --help
```

### Local sample workloads
```bash
docker compose up --build
```

## High-level architecture

OptiPilot is a two-process system connected by a shared gRPC contract:

1. **Contract-first boundary:** `proto/optipilot/v1/prediction.proto` is the canonical API. Generated clients/servers live in `gen/go/` and `gen/python/`.
2. **Controller (`controller/`, Go):**
   - `main.go` wires long-running loops with graceful shutdown.
   - `internal/discovery` resolves service targets (static mode is active; Kubernetes informer mode is a stub with a defined implementation plan).
   - `internal/collector` scrapes Prometheus concurrently and writes metrics to SQLite via `internal/store`.
   - `internal/predictor` sends recent metrics to forecaster (`IngestMetrics`), requests predictions (`GetPrediction`), logs recommendations, persists audit decisions (`Executed=false`), and caches latest predictions in memory.
   - `internal/store` is the persistence interface; SQLite is the current implementation.
3. **Forecaster (`forecaster/`, Python):**
   - `src/forecaster/__main__.py` loads config, initializes metrics/registry stores, and starts gRPC service.
   - `src/forecaster/server/service.py` implements RPC handlers and bridges proto types to internal models.
   - `src/forecaster/ml/trainer.py` handles p50/p90 LightGBM training and model promotion.
   - `src/forecaster/ml/inference.py` loads promoted models, performs prediction, and computes scaling mode/confidence signals.
4. **Frontend (`optipilot-frontend/`, Next.js):**
   - Dashboard UI exists under `app/`.
   - Current UI state is mock/simulated (hook-driven), not yet fully wired to live controller REST/WebSocket APIs.

## V1 scope boundaries (from project context)

- Target workload is **stateless HTTP services on Kubernetes** only.
- Treat `services/` as local test workloads, not product code.
- Prometheus is the only metrics source in v1.
- Horizontal scaling is primary; vertical scaling is experimental and requires explicit per-service opt-in.
- Operational modes are `shadow` (default), `recommend`, and `autonomous`; mode semantics must stay consistent across controller, forecaster, and dashboard work.
- Safety controls are first-class v1 behavior: kill switch, bounds, cooldowns, rate limiting, audit logging, rollback.
- Keep v1 scoped to single-cluster operation; do not introduce multi-cluster federation, service mesh features, or non-Kubernetes runtime support.

## Key conventions

- **Service-agnostic core:** avoid hardcoding service names in controller/forecaster logic. Static names in `controller/optipilot.yaml` are for local dev discovery.
- **Generated stubs are not hand-edited:** treat `gen/go/` and `gen/python/` as generated outputs from `proto/`.
- **Python proto imports are shared-stub imports:** use `from optipilot.v1 import prediction_pb2, prediction_pb2_grpc`. Keep `gen/python` path wiring intact in the forecaster entrypoint.
- **Go proto module uses local replacement:** keep `controller/go.mod` wired to local generated stubs with:
  `replace github.com/optipilot/proto/gen/go => ../gen/go`
- **Structured logging is the default:** Go uses JSON `slog`; forecaster uses JSON logging formatter.
- **Graceful degradation is intentional:** controller loops should continue operating (with warnings) when Prometheus/forecaster are unavailable.
- **Discovery contract matters:** static discovery is stable; Kubernetes discovery must honor `optipilot.io` labels/annotations as described in `internal/discovery/kubernetes.go`.
- **Audit decision records are part of control-loop output:** predictor writes `ScalingDecision` rows with `Executed=false`; future actuator paths should keep this audit trail contract intact.
- **When editing frontend Next.js behavior, read `optipilot-frontend/AGENTS.md` first** because project-specific Next.js guidance is version-sensitive.
