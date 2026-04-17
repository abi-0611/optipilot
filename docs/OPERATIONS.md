# OptiPilot Operations Guide

This guide covers running, tuning, observing, and troubleshooting OptiPilot.

## 1. Operating modes

- **shadow**: generates recommendations and audit records only.
- **recommend**: records pending recommendations for operator approve/reject.
- **autonomous**: applies scaling directly (subject to safety checks).

Set globally in config or per service through:

- `POST /api/services/{name}/mode`

## 2. Deployment options

## Local compose/dev

- `docker compose up --build` for sample services + dependencies.
- Start forecaster (`uv run forecaster`) and controller (`go run . -config optipilot.yaml`).

## Kubernetes via Helm

```bash
helm install optipilot ./charts/optipilot -n optipilot-system --create-namespace
```

Tune values in `charts/optipilot/values.yaml`.

## 3. Key tuning parameters

## Controller scaling safety

- `default_min_replicas`, `default_max_replicas`
- `cooldown_scale_up`, `cooldown_scale_down`
- `max_scale_up_percent`, `max_scale_down_percent`
- `emergency_scale_up_threshold`
- global/service kill switch state

## Collector/predictor intervals

- collection interval (metrics freshness vs cost)
- prediction interval (reactivity vs noise)
- data retention/purge interval

## Forecaster training

- `min_data_points_for_training`
- `full_retrain_interval_hours`
- drift/calibration scheduler intervals
- promotion thresholds (MAPE + relative improvement)

## 4. Day-2 operations

## Check health

```bash
curl http://localhost:8080/api/system/status | jq
```

## Observe decisions

```bash
curl 'http://localhost:8080/api/audit?limit=200' | jq
```

## Stream live events

```bash
wscat -c ws://localhost:8080/ws/events
```

## Trigger retrain

```bash
curl -X POST http://localhost:8080/api/services/api-gateway/retrain
```

## Emergency stop

```bash
curl -X POST http://localhost:8080/api/kill-switch \
  -H 'content-type: application/json' \
  -d '{"enabled":true}'
```

## 5. Monitoring signals

Watch these signals continuously:

- ingestion continuity (metrics arriving for each target service)
- prediction confidence trend
- replica decision frequency and rollback frequency
- forecast error (MAPE) over time
- drift score/status per service
- controller/forecaster process health and restart count

## 6. Troubleshooting

## No services in `/api/services`

- verify discovery mode and configured targets
- verify label selectors/namespace for Kubernetes discovery
- confirm Prometheus can scrape workloads

## Forecaster connectivity errors

- check controller forecaster address config
- confirm gRPC port/service reachability
- inspect forecaster logs for startup or schema issues

## No predictions / fallback only

- insufficient training history
- no promoted model yet
- retrain blocked by minimum data or quality gates

## Scaling not executing

- mode is shadow/recommend instead of autonomous
- global or per-service kill switch active
- safety cooldown/rate limits currently blocking change

## WebSocket clients disconnecting

- client too slow (bounded queue drops slow subscribers)
- proxy timeout / ingress idle timeout
- restart or shutdown in progress

## 7. Production hardening checklist

1. Externalize controller/forecaster SQLite to durable storage or migrate to managed DB.
2. Configure resource requests/limits and disruption budgets.
3. Add centralized logs/metrics/alerts.
4. Protect APIs with authn/authz and network policy.
5. Use TLS termination for dashboard and gRPC paths.
6. Define rollout + rollback playbooks.
7. Keep autonomous mode behind staged promotion gates.

