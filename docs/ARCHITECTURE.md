# OptiPilot Architecture

This document describes OptiPilot’s runtime architecture, component responsibilities, and design rationale.

## 1. System context

```mermaid
flowchart TB
  User[Platform Operator] --> UI[Dashboard UI]
  UI -->|REST/WS| Controller[OptiPilot Controller]

  Controller -->|gRPC| Forecaster[OptiPilot Forecaster]
  Controller -->|PromQL HTTP| Prometheus[(Prometheus)]
  Controller -->|Patch APIs| Kube[(Kubernetes API Server)]

  Prometheus -->|scrapes| Workloads[Stateless HTTP Services]
  Kube --> Workloads
```

## 2. Component responsibilities

## Controller (Go)

- Loads config, initializes SQLite store, and manages lifecycle/shutdown.
- Discovers target services (static or Kubernetes informer mode).
- Collects service metrics from Prometheus on a fixed interval.
- Runs predictor loop:
  - pushes metrics to forecaster (`IngestMetrics`)
  - requests forecasts (`GetPrediction`)
  - passes recommendations to actuator.
- Runs actuator + safety logic:
  - mode handling (shadow/recommend/autonomous)
  - cooldown, bounds, rate limiting, kill switches
  - optional vertical patching and rollback monitoring.
- Serves dashboard backend:
  - REST APIs for history/control
  - WebSocket event stream.

## Forecaster (Python)

- Stores ingested metrics.
- Trains per-service LightGBM p50/p90 models.
- Serves inference results with confidence and mode signal.
- Schedules recalibration/retrain/drift detection jobs.
- Maintains model registry and promotion status.

## 3. Runtime interactions

## 3.1 Metrics to prediction flow

```mermaid
sequenceDiagram
  participant P as Prometheus
  participant C as Controller Collector/Predictor
  participant S as Controller Store
  participant F as Forecaster
  participant A as Actuator

  C->>P: Query service metrics
  P-->>C: RPS/latency/cpu/memory/error
  C->>S: Save metrics batch
  C->>F: IngestMetrics(batch)
  C->>F: GetPrediction(service, recent_rps)
  F-->>C: p50, p90, replicas, confidence, model_version
  C->>A: HandlePrediction(target, prediction)
  A->>S: Save scaling decision audit record
```

## 3.2 Recommend mode approval path

```mermaid
sequenceDiagram
  participant UI as Dashboard
  participant D as Dashboard REST
  participant A as Actuator
  participant K as Kubernetes API
  participant S as Store

  UI->>D: POST /api/recommendations/{id}/approve
  D->>A: ApproveRecommendation(id)
  A->>S: Load decision + evaluate safety
  A->>K: Patch replicas (if allowed)
  A->>S: Update executed=true/reason
  D-->>UI: Updated decision
```

## 3.3 Event streaming

```mermaid
sequenceDiagram
  participant Collector
  participant Predictor
  participant Actuator
  participant Bus as EventBus
  participant WS as WebSocket Hub
  participant Client as Dashboard Client

  Collector->>Bus: metrics_update
  Predictor->>Bus: prediction
  Actuator->>Bus: scaling_decision/mode_change/alert
  WS->>Bus: Subscribe(types)
  Bus-->>WS: Event payloads
  WS-->>Client: JSON events
```

## 4. Data flow

```mermaid
flowchart LR
  Metrics[Prometheus metrics] --> ControllerDB[(controller optipilot.db)]
  ControllerDB --> Predictor
  Predictor --> ForecasterIngest[Forecaster metrics DB]
  ForecasterIngest --> Training
  Training --> ModelRegistry[(Forecaster registry DB)]
  ModelRegistry --> Inference
  Inference --> Recommendations
  Recommendations --> DecisionAudit[(controller scaling_decisions)]
  DecisionAudit --> DashboardAPIs
```

## 5. Safety model

```mermaid
flowchart TD
  Pred[Recommended replicas] --> Bounds[Apply min/max bounds]
  Bounds --> Rate[Apply max change %]
  Rate --> Cooldown[Check cooldown]
  Cooldown --> Kill[Check global/service kill switch]
  Kill -->|blocked| LogOnly[Persist decision executed=false]
  Kill -->|allowed| Apply[Patch deployment replicas]
  Apply --> Audit[Persist decision executed=true]
  Apply --> Rollback[Monitor degradation & rollback]
```

## 6. Technology choices and rationale

- **Go controller:** strong concurrency model for periodic loops and Kubernetes integration.
- **Python forecaster:** richer ML ecosystem (LightGBM, pandas, numpy).
- **gRPC + protobuf:** typed, language-agnostic boundary between control plane and ML plane.
- **SQLite:** lightweight persistence for local/dev and demo environments.
- **Prometheus integration:** standard source of service telemetry in Kubernetes.
- **Event-driven dashboard backend:** low-latency operator visibility via WebSockets.

## 7. Current constraints

- v1 targets stateless HTTP services on Kubernetes.
- Some UI wiring is still being aligned with latest backend routes.
- Production-grade HA hardening (multi-instance leader election, external DB, etc.) is out of current scope.

