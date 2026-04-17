# Contributing to OptiPilot

Thanks for contributing. This guide covers local setup, testing, and PR expectations.

## 1. Prerequisites

- Go (for controller)
- Python + [`uv`](https://docs.astral.sh/uv/) (for forecaster)
- Node.js/npm (for frontend)
- Docker / Docker Compose
- `protoc` toolchain for proto generation
- Helm (for chart validation)

## 2. Repository structure

- `controller/` — Go control plane
- `forecaster/` — Python ML service
- `proto/` + `gen/` — gRPC contracts + generated stubs
- `optipilot-frontend/` — Next.js dashboard frontend
- `charts/optipilot/` — Helm chart
- `loadtest/` — Vegeta scenarios

## 3. Local development

## Controller

```bash
cd controller
go build ./...
go test ./...
go run . -config optipilot.yaml
```

## Forecaster

```bash
cd forecaster
bash scripts/sync_proto.sh
uv sync --extra dev
uv run pytest
uv run forecaster
```

## Frontend

```bash
cd optipilot-frontend
npm install
npm run dev
```

## Services and local stack

```bash
docker compose up --build
```

## 4. Proto workflow

When changing `proto/optipilot/v1/prediction.proto`:

```bash
make -C proto all
cd forecaster && bash scripts/sync_proto.sh
```

Do not hand-edit generated files in `gen/` or `forecaster/src/forecaster/proto/`.

## 5. Validation checklist before PR

1. Controller tests/build pass:
   ```bash
   cd controller && go test ./... && go build ./...
   ```
2. Forecaster tests pass:
   ```bash
   cd forecaster && uv run pytest
   ```
3. Helm chart validates:
   ```bash
   helm lint ./charts/optipilot
   helm template optipilot ./charts/optipilot -n optipilot-system >/dev/null
   ```
4. If you changed scripts:
   ```bash
   bash -n loadtest/scenarios/*.sh loadtest/run-all.sh
   ```

## 6. Coding conventions

- Keep controller and forecaster service-agnostic (no hardcoded workload names).
- Preserve mode semantics across stack: `shadow`, `recommend`, `autonomous`.
- Keep structured logging (`slog` JSON in Go; structured logs in Python).
- Favor explicit error handling; avoid silent fallbacks that hide failures.
- Keep changes scoped and avoid touching unrelated files.

## 7. Pull request guidelines

- Use clear title + summary of behavior change.
- Explain config/API/schema impacts.
- Include test evidence and manual verification steps.
- Call out breaking changes explicitly.
- Keep PRs focused; split large unrelated work into separate PRs.

## 8. Security and safety

- Do not commit secrets.
- Treat kill-switch and safety logic as critical behavior.
- For scaling logic changes, include audit-path and rollback considerations.

