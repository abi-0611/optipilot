# OptiPilot API Reference

This document covers the controller REST API, WebSocket events, and forecaster gRPC API.

## Base URLs

- **Controller REST/WS (default local):** `http://localhost:8080`
- **Forecaster gRPC (default local):** `localhost:50051`

---

## 1. REST API (Controller)

All responses are JSON.

## 1.1 Service and history endpoints

## `GET /api/services`

List monitored services with latest state.

**Response (200)**

```json
{
  "services": [
    {
      "name": "api-gateway",
      "namespace": "default",
      "min_replicas": 2,
      "max_replicas": 15,
      "current_replicas": 3,
      "mode": "shadow",
      "paused": false,
      "last_metrics": { "service_name": "api-gateway", "rps": 62.1 },
      "last_prediction": { "rps_p50": 70.0, "rps_p90": 95.0 },
      "model_status": { "model_version": "v3", "current_mape": 0.18 }
    }
  ]
}
```

## `GET /api/services/{name}/metrics?minutes=60`

Historical metrics for one service.

## `GET /api/services/{name}/predictions?limit=100`

Recent prediction history for one service.

## `GET /api/services/{name}/decisions?limit=100`

Recent scaling decisions (audit trail) for one service.

## `GET /api/services/{name}/model`

Model status for one service.

- `200` with status payload.
- `404` if status unavailable.

---

## 1.2 Control endpoints

## `POST /api/services/{name}/mode`

Set per-service operating mode.

**Request**

```json
{
  "mode": "recommend",
  "triggered_by": "operator"
}
```

`mode` values: `shadow`, `recommend`, `autonomous`.

## `POST /api/services/{name}/retrain`

Trigger forecaster retrain for a service.

- `200` when request reaches forecaster.
- `502` when forecaster is unreachable.

## `POST /api/services/{name}/pause`

Pause/unpause scaling for one service.

**Request**

```json
{ "paused": true }
```

## `POST /api/kill-switch`

Toggle global kill switch.

**Request**

```json
{ "enabled": true }
```

## `POST /api/recommendations/{id}/approve`

Approve one pending recommendation (recommend mode).

## `POST /api/recommendations/{id}/reject`

Reject one pending recommendation.

---

## 1.3 Audit and health endpoints

## `GET /api/audit?limit=200`

Cross-service recent scaling decision log.

## `GET /api/system/status`

Controller-level health snapshot.

**Response (200)**

```json
{
  "controller": {
    "healthy": true,
    "uptime_sec": 311,
    "metrics_row_count": 982,
    "global_kill_switch": false
  },
  "forecaster": { "connected": true, "error": "" },
  "prometheus": { "connected": true, "error": "" },
  "websocket": { "connections": 2 }
}
```

---

## 2. WebSocket API (Controller)

## Endpoint

`GET /ws/events`

Optional filter:

`GET /ws/events?types=alert,prediction`

## Event envelope

```json
{
  "type": "prediction",
  "data": { "service": "api-gateway" },
  "timestamp": "2026-04-17T01:00:00Z"
}
```

## Event types

## `metrics_update`

```json
{
  "service": "api-gateway",
  "rps": 71.2,
  "cpu": 63.4,
  "memory": 421.1,
  "latency": 24.8,
  "timestamp": "2026-04-17T01:00:00Z"
}
```

## `prediction`

```json
{
  "service": "api-gateway",
  "p50": 80.0,
  "p90": 110.0,
  "replicas": 4,
  "mode": "recommend",
  "confidence": 0.84,
  "model_version": "v5"
}
```

## `scaling_decision`

```json
{
  "service": "api-gateway",
  "old_replicas": 3,
  "new_replicas": 4,
  "reason": "autonomous scale from 3 to 4",
  "executed": true,
  "timestamp": "2026-04-17T01:00:00Z"
}
```

## `mode_change`

```json
{
  "service": "api-gateway",
  "old_mode": "shadow",
  "new_mode": "autonomous",
  "triggered_by": "operator"
}
```

## `alert`

```json
{
  "service": "*",
  "severity": "critical",
  "message": "global kill switch enabled",
  "timestamp": "2026-04-17T01:00:00Z"
}
```

## `model_update`

```json
{
  "service": "api-gateway",
  "new_version": "v6",
  "mape": 0.14,
  "promoted": true
}
```

---

## 3. gRPC API (Forecaster)

Service: `optipilot.v1.OptiPilotService`

Defined in `proto/optipilot/v1/prediction.proto`.

## RPCs

1. `GetPrediction(GetPredictionRequest) returns (GetPredictionResponse)`
2. `GetModelStatus(GetModelStatusRequest) returns (GetModelStatusResponse)`
3. `GetAllServicesStatus(AllServicesStatusRequest) returns (AllServicesStatusResponse)`
4. `GetServiceMetricsHistory(MetricsHistoryRequest) returns (MetricsHistoryResponse)`
5. `IngestMetrics(IngestMetricsRequest) returns (IngestMetricsResponse)`
6. `TriggerRetrain(TriggerRetrainRequest) returns (TriggerRetrainResponse)`

## Core message fields

- `GetPredictionRequest`:
  - `service_name`
  - `recent_rps[]`
  - `timestamp`
- `GetPredictionResponse`:
  - `rps_p50`, `rps_p90`
  - `recommended_replicas`
  - `scaling_mode` (PREDICTIVE/CONSERVATIVE/REACTIVE)
  - `confidence_score`, `reason`, `model_version`
- `ServiceMetric`:
  - `rps`, `avg_latency_ms`, `p99_latency_ms`
  - `active_connections`, `cpu_usage_percent`, `memory_usage_mb`, `error_rate`
  - `timestamp`
- `TriggerRetrainResponse`:
  - `success`, `new_model_version`, `new_mape`, `message`

## Example grpcurl

```bash
grpcurl -plaintext -d '{"service_name":"api-gateway","recent_rps":[40,45,42,51]}' \
  localhost:50051 optipilot.v1.OptiPilotService/GetPrediction
```

---

## 4. Notes on current behavior

- `GetPrediction` in forecaster uses real model inference when enough history + promoted model are available; otherwise it returns fallback (`stub-v0`).
- `TriggerRetrain` is functional and can train/promote models.
- Some status surfaces may still return limited/placeholder data depending on dataset maturity.

