

**OptiPilot: Machine Learning-Driven Predictive Autoscaling**

**and Orchestration for Cloud-Native Kubernetes Infrastructure**

*Project Statement*

April 2026

*Keywords: predictive autoscaling, Kubernetes, machine learning, cloud orchestration, time-series forecasting,*

*quantile regression, horizontal scaling, vertical scaling, cloud-native infrastructure, container orchestration, microservices, LightGBM*

**Abstract**

OptiPilot is a Kubernetes-native intelligent autoscaling system that employs machine learning-based time-series forecasting to predict microservice traffic demand and proactively adjust computational resources before performance degradation occurs. Unlike conventional reactive autoscaling mechanisms such as the Horizontal Pod Autoscaler (HPA), which respond only after resource utilization thresholds are breached, OptiPilot forecasts requests-per-second (RPS) demand at 5-minute and 15-minute horizons using per-service quantile LightGBM regression models, translates these forecasts into scaling decisions with uncertainty quantification, and actuates both horizontal (replica count) and vertical (CPU and memory allocation) scaling actions through direct Kubernetes API manipulation. The system features automatic service discovery via Kubernetes label selectors, Prometheus-based metrics ingestion via PromQL, a configurable multi-mode operation framework (shadow, recommend, and autonomous), a comprehensive safety layer with kill switches and scaling bounds, and a real-time monitoring dashboard. OptiPilot is architected as a two-component deployment consisting of a Go-based controller responsible for metrics collection, service discovery, scaling orchestration, safety enforcement, and dashboard serving, and a Python-based forecaster responsible for model training, inference, lifecycle management, and drift detection. The system communicates internally via gRPC with Protocol Buffers and exposes real-time operational state to a Next.js dashboard via WebSocket. OptiPilot is designed as an installable product deployable via Helm chart into any Kubernetes cluster, operating service-agnostically over any stateless HTTP workload that the operator labels for monitoring.

# **I. INTRODUCTION**

Modern cloud-native architectures decompose applications into independently deployable microservices orchestrated by container platforms such as Kubernetes. As these systems scale to serve millions of requests across dozens or hundreds of services, maintaining optimal resource allocation becomes a critical operational challenge. Over-provisioning wastes computational resources and incurs unnecessary cloud infrastructure costs; under-provisioning leads to latency spikes, request timeouts, and service-level objective (SLO) violations during demand surges.

The prevailing approach to autoscaling in Kubernetes is the Horizontal Pod Autoscaler (HPA), a reactive controller that monitors resource utilization metrics, typically CPU or memory, and adjusts replica counts when predefined thresholds are exceeded. While functional, this mechanism is fundamentally reactive: it detects demand increases only after they manifest as resource pressure, introduces a decision-to-actuation latency of 60 to 180 seconds as new pods are scheduled, initialized, and become ready, and cannot anticipate predictable traffic patterns such as diurnal cycles, weekly seasonality, or event-driven surges.

OptiPilot addresses this limitation by introducing a predictive control loop that operates alongside conventional scaling infrastructure. The system continuously ingests metrics from Prometheus, trains per-service machine learning models on historical traffic patterns, generates short-term demand forecasts with calibrated uncertainty estimates, and translates these forecasts into proactive scaling actions executed through the Kubernetes API. By scaling infrastructure ahead of demand, OptiPilot eliminates the reactive latency gap and maintains consistent application performance through traffic transitions.

The system supports both horizontal scaling, adjusting the number of pod replicas, and vertical scaling, adjusting the CPU and memory resource allocations of existing pods. Horizontal scaling serves as the primary mechanism for stateless HTTP workloads, while vertical scaling is provided as a configurable experimental capability for workloads that are resource-bound rather than concurrency-bound. A multi-mode operation framework allows operators to deploy OptiPilot in shadow mode for passive observation, recommend mode for human-in-the-loop operation, or autonomous mode for fully automated scaling.

# **II. PROBLEM STATEMENT**

Reactive autoscaling in Kubernetes follows a fixed temporal sequence when demand increases: metrics collection detects threshold breach, the HPA controller computes a desired replica count, the Kubernetes scheduler assigns new pods to nodes, container images are pulled and initialized, readiness probes pass, and traffic is rebalanced across the expanded replica set. This sequence introduces an unavoidable latency of 60 to 180 seconds between demand onset and capacity availability. During this window, the application operates with insufficient resources, resulting in elevated response latency, increased error rates, and potential SLO violations.

This problem is compounded by three characteristics of real-world traffic patterns. First, many demand surges are predictable: diurnal cycles, weekly patterns, promotional events, and billing cycles generate repeatable traffic shapes that a data-driven system can learn and anticipate. Second, reactive scaling is directionally symmetric in its latency, meaning that both scale-up and scale-down decisions are delayed, leading to unnecessary resource consumption after demand subsides. Third, the reactive model applies uniform scaling logic regardless of per-service traffic characteristics, ignoring the reality that different services exhibit fundamentally different demand signatures and benefit from different scaling strategies.

OptiPilot is designed to solve these problems by forecasting demand before it arrives, making service-specific scaling decisions grounded in uncertainty-quantified predictions, and providing operators with configurable control over how aggressively the system acts on its forecasts.

# **III. SYSTEM ARCHITECTURE**

## **A. Architectural Overview**

OptiPilot is architected as a two-component system deployed within the target Kubernetes cluster. The first component is the OptiPilot Controller, a single Go binary that serves as the operational core of the system. The second component is the OptiPilot Forecaster, a Python service that encapsulates all machine learning functionality. These components communicate via gRPC using a shared Protocol Buffer contract, enabling strongly-typed, low-latency bidirectional communication.

The decision to separate the controller and forecaster into distinct processes reflects a deliberate architectural choice: the controller requires low-latency, high-reliability operation with direct Kubernetes API access, characteristics well-suited to Go’s concurrency model and static compilation. The forecaster requires access to the Python scientific computing ecosystem, particularly LightGBM, pandas, and scikit-learn, for model training and inference. Separating these concerns allows each component to be developed, tested, scaled, and restarted independently without mutual disruption.

## **B. OptiPilot Controller**

The controller is implemented as a single Go binary that executes multiple concurrent responsibilities within a unified process. This single-binary architecture minimizes deployment complexity while maintaining clear internal separation of concerns through Go’s package system. The controller encompasses the following subsystems:

* Metrics Collector: Queries Prometheus at configurable intervals (default 15 seconds) via the PromQL HTTP API. Collects request rate, CPU utilization, memory utilization, response latency percentiles (p50, p95, p99), and error rate for all discovered services. Stores collected metrics in a local SQLite database and maintains an in-memory cache for low-latency dashboard access.

* Service Discovery Engine: Watches Kubernetes Deployments using client-go informers filtered by the label selector optipilot.io/enabled=true. Automatically detects new services, removed services, and configuration changes. Reads per-service settings from Kubernetes annotations including metrics port, scaling bounds, and scaling tier.

* Scaling Actuator: Receives forecast-based scaling recommendations from the forecaster via gRPC, applies safety checks (scaling bounds, cooldown timers, rate limiting), and executes scaling actions through the Kubernetes API. Supports horizontal scaling via Deployment replica patching and experimental vertical scaling via resource request and limit modification.

* Safety Engine: Enforces operational safety constraints including global and per-service kill switches, minimum and maximum replica bounds, scale-up and scale-down cooldown timers, maximum replica change rate per scaling cycle, and automatic rollback on degradation detection. All safety constraint evaluations are logged.

* Dashboard Server: Serves a Next.js frontend application as static files, exposes REST API endpoints for historical data queries, and maintains WebSocket connections for real-time event streaming of scaling decisions, metric updates, and system state changes.

* Audit Logger: Records every scaling decision with full contextual metadata including timestamp, service identifier, previous and new replica counts, model version, confidence score, scaling mode, and human-readable reasoning. Audit records are persisted in SQLite and exposed via the dashboard API.

## **C. OptiPilot Forecaster**

The forecaster is implemented as a Python service exposing both gRPC (for controller communication) and HTTP (for health monitoring) interfaces. It encapsulates all machine learning functionality including feature engineering, model training, inference, model lifecycle management, and drift detection. The forecaster encompasses the following subsystems:

* Inference Engine: Receives recent metrics data from the controller via gRPC, constructs feature vectors, executes per-service LightGBM quantile models, and returns predictions including median (p50) and upper-bound (p90) RPS forecasts, recommended replica counts, confidence scores, and scaling mode recommendations.

* Model Registry: Maintains a SQLite-backed registry of trained models indexed by service name, tracking model version, training timestamp, validation MAPE, feature configuration, and promotion status. Supports atomic model promotion with automatic rollback if a newly trained model underperforms its predecessor.

* Training Pipeline: Executes scheduled model training on configurable intervals. Full retraining operates on a rolling data window with holdout validation. Incremental recalibration applies bias correction from recent residuals without full retraining. New models are promoted to serving only upon validation against the holdout set.

* Drift Detector: Continuously monitors prediction accuracy via rolling MAPE computation. When per-service forecast error exceeds configurable thresholds, the detector triggers mode degradation (from predictive to conservative to reactive) and optionally initiates emergency retraining.

* Confidence Scorer: Computes a composite confidence metric from prediction interval width, recent forecast accuracy, and data freshness. This score gates the aggressiveness of scaling actions: high confidence enables full predictive scaling, moderate confidence triggers conservative scaling with reduced step size, and low confidence suspends predictive scaling entirely.

## **D. Communication Architecture**

| Communication Path | Protocol | Purpose |
| :---- | :---- | :---- |
| Controller ↔ Forecaster | gRPC (Protocol Buffers) | Metrics ingestion, prediction requests, model status queries, retrain triggers |
| Controller → Prometheus | HTTP (PromQL API) | Metrics collection for all discovered services |
| Controller → Kubernetes API | client-go (native Go client) | Service discovery, Deployment scaling, pod status monitoring |
| Controller → Dashboard | WebSocket \+ REST (HTTP/JSON) | Real-time event streaming and historical data queries |
| Controller → SQLite | modernc.org/sqlite (embedded) | Metrics storage, scaling audit trail, system configuration |
| Forecaster → SQLite | Python sqlite3 (embedded) | Model registry, training metadata, feature configuration |

# **IV. SERVICE DISCOVERY AND CONFIGURATION**

## **A. Label-Based Auto-Discovery**

OptiPilot discovers services to monitor through Kubernetes label selectors. Any Deployment within the configured namespace (or across all namespaces) bearing the label optipilot.io/enabled: "true" is automatically registered for monitoring. The controller uses client-go informers to maintain a real-time watch on Deployment resources, detecting additions, modifications, and deletions without polling.

Per-service configuration is specified through Kubernetes annotations on the Deployment resource, enabling operators to tune OptiPilot’s behavior for individual services without modifying any central configuration file. The following annotations are recognized:

| Annotation | Default | Description |
| :---- | :---- | :---- |
| optipilot.io/metrics-port | First container port | Port on which the service exposes metrics to Prometheus |
| optipilot.io/min-replicas | 2 | Minimum replica count OptiPilot will maintain |
| optipilot.io/max-replicas | 15 | Maximum replica count OptiPilot will not exceed |
| optipilot.io/scaling-tier | standard | Scaling aggressiveness tier (critical, standard, relaxed) |
| optipilot.io/mode | Global default | Per-service operating mode override (shadow, recommend, autonomous) |
| optipilot.io/vertical-scaling | false | Enable experimental vertical scaling for this service |
| optipilot.io/cooldown-scale-up | 120s | Per-service scale-up cooldown override |
| optipilot.io/cooldown-scale-down | 600s | Per-service scale-down cooldown override |

## **B. Global Configuration**

System-wide settings are managed through a single YAML configuration file mounted as a Kubernetes ConfigMap. The configuration governs Prometheus connectivity, default scaling parameters, forecaster endpoint, dashboard server settings, and collection intervals. Environment variables with the OPTIPILOT\_ prefix may override any configuration value, supporting twelve-factor application principles.

# **V. METRICS INGESTION**

## **A. Prometheus Integration**

OptiPilot collects metrics by querying an existing Prometheus instance via the PromQL HTTP API. This design avoids instrumenting or modifying target services and leverages the metrics infrastructure already present in most production Kubernetes environments. The controller issues PromQL queries at configurable intervals (default 15 seconds) for each discovered service. The following Prometheus metrics are queried for each monitored service:

| Metric Category | PromQL Source | Usage in Forecasting |
| :---- | :---- | :---- |
| Request rate (RPS) | rate(http\_requests\_total\[1m\]) | Primary forecasting target for horizontal scaling decisions |
| CPU utilization | container\_cpu\_usage\_seconds\_total | Input feature for forecasting; primary signal for vertical scaling |
| Memory utilization | container\_memory\_working\_set\_bytes | Input feature for forecasting; vertical scaling signal |
| Response latency (p50) | histogram\_quantile(0.50, http\_request\_duration\_seconds\_bucket) | Service health indicator; anomaly detection input |
| Response latency (p95) | histogram\_quantile(0.95, ...) | SLO monitoring; degradation detection for rollback trigger |
| Response latency (p99) | histogram\_quantile(0.99, ...) | Tail latency tracking; capacity saturation indicator |
| Error rate | rate(http\_requests\_total{code=\~"5.."}\[1m\]) | Safety signal; high error rate inhibits scale-down |

## **B. Data Storage and Retention**

Collected metrics are persisted in a SQLite database embedded within the controller process. The database is indexed by service name and collection timestamp for efficient range queries. A configurable retention policy (default 24 hours) automatically purges aged metrics to bound storage growth. The controller additionally maintains an in-memory cache of the most recent metric snapshot per service, enabling sub-millisecond dashboard queries without database access.

# **VI. MACHINE LEARNING FORECASTING LAYER**

## **A. Model Architecture**

OptiPilot employs per-service isolated LightGBM quantile regression models for demand forecasting. Each monitored service receives its own independently trained model, reflecting the observation that different microservices exhibit fundamentally different traffic patterns: an API gateway serving external requests displays different diurnal and weekly seasonality than an internal order processing service or a payment gateway with end-of-month surges.

The choice of LightGBM quantile regression is driven by the operational constraints of a production autoscaling system. The model must satisfy five requirements simultaneously: sub-second inference latency to support 60-second prediction cycles across many services, uncertainty quantification to enable confidence-gated scaling decisions, robustness to missing or stale input data, fast retraining cycles to adapt to evolving traffic patterns, and interpretability for debugging and audit purposes. LightGBM satisfies all five constraints. Deep learning alternatives such as LSTM networks or Temporal Fusion Transformers offer marginal accuracy improvements on clean datasets but fail to meet the latency, robustness, and interpretability requirements.

## **B. Quantile Regression Outputs**

Each model produces two quantile predictions per forecast horizon:

* p50 (median): The expected demand level. Represents the most likely RPS value at the forecast horizon. Used for informational display and as a baseline for confidence interval computation.

* p90 (upper bound): The 90th percentile demand estimate. Represents the value that actual demand will fall below 90% of the time. This value drives scaling decisions, ensuring that provisioned capacity meets demand with 90% probability without requiring excessive over-provisioning.

The use of quantile outputs rather than point forecasts is architecturally significant. Point forecasts require the addition of a static buffer (typically 20–30%) to provide safety headroom, yielding scaling decisions disconnected from actual prediction uncertainty. Quantile regression directly estimates the demand distribution, grounding headroom decisions in calibrated uncertainty rather than arbitrary constants.

## **C. Forecast Horizons**

The system generates forecasts at two horizons:

* 5-minute horizon (primary): Drives all scaling decisions. A 5-minute lead time provides sufficient advance warning for Kubernetes to schedule, initialize, and ready new pods (typically 10–30 seconds) while maintaining high forecast accuracy. The scaling actuator receives and acts on 5-minute forecasts.

* 15-minute horizon (secondary): Provides extended visibility for dashboard visualization and trend awareness. The 15-minute forecast is displayed on the monitoring dashboard to give operators advance notice of approaching demand changes but does not directly drive scaling actions.

## **D. Feature Engineering**

The feature vector constructed for each prediction comprises three categories of engineered features derived from the collected Prometheus metrics:

| Feature Category | Features | Dimensionality |
| :---- | :---- | :---- |
| Lag values | RPS at t-1, t-2, t-3, t-5, t-10, t-15, t-30 minutes | 7 |
| Rolling statistics | Mean and maximum of RPS over 5-minute, 15-minute, and 30-minute windows | 6 |
| Temporal features | Hour of day (0–23), day of week (0–6), binary weekend indicator | 3 |
| Resource features | CPU utilization, memory utilization, error rate (current values) | 3 |

The feature set is deliberately constrained to 19 dimensions to minimize overfitting risk on limited training data while capturing the primary signals relevant to demand forecasting: recent momentum (lag values), sustained trend (rolling statistics), predictable seasonality (temporal features), and current system state (resource features).

## **E. Model Lifecycle Management**

OptiPilot implements a continuous model lifecycle with four operational stages:

**Initial Training.** When a new service is discovered and sufficient historical data has been accumulated (minimum 24 hours, recommended 7 days), the forecaster trains an initial model using the complete available history. The model is validated against a held-out time window (the most recent 20% of data) and promoted to serving only if validation MAPE falls below the configured acceptance threshold.

**Bias Recalibration.** At short intervals (default 5 minutes), the forecaster computes the median residual between recent predictions and actual observations over a trailing window (default 6 hours). This median residual is applied as a bias correction offset to subsequent predictions, enabling rapid adaptation to level shifts without full retraining.

**Incremental Retraining.** At moderate intervals (default 10 minutes), the forecaster retrains the model on a recent data window (default 2 hours). The retrained model is evaluated against the current production model; promotion occurs only if the new model achieves equal or better MAPE.

**Full Retraining.** At long intervals (default 24 hours), the forecaster executes a complete retraining cycle on the full rolling data window (default 90 days). This captures long-term seasonal patterns and structural changes in traffic behavior. Full retraining includes hyperparameter validation and comprehensive holdout evaluation.

## **F. Confidence Gating**

Every prediction is accompanied by a composite confidence score that gates the aggressiveness of resulting scaling actions. The confidence score integrates three signals: recent forecast accuracy (rolling MAPE over the trailing 3 hours), prediction interval width (ratio of p90 to p50, normalized), and data freshness (time since last successful metric collection). Based on this composite score, the system assigns one of three scaling modes:

| Scaling Mode | Confidence Condition | Actuator Behavior |
| :---- | :---- | :---- |
| Predictive | MAPE \< 20% AND interval width \< 40% | Full proactive scaling applied based on p90 forecast |
| Conservative | MAPE 20–30% OR interval width 40–60% | Scaling step reduced to 50% of recommendation |
| Reactive | MAPE \> 30% OR interval width \> 60% | Predictive scaling suspended; system defers to baseline behavior |

## **G. Drift Detection**

The forecaster continuously monitors per-service prediction accuracy via a rolling MAPE computation over a configurable trailing window. When forecast error exceeds the service-specific threshold, the drift detector initiates a cascading response: first degrading the scaling mode from predictive to conservative or reactive, then triggering emergency retraining if degradation persists. This mechanism ensures that the system automatically reduces its operational authority when model quality deteriorates, preventing stale or inaccurate models from driving harmful scaling decisions.

# **VII. SCALING MECHANISMS**

## **A. Horizontal Scaling**

Horizontal scaling is the primary resource adjustment mechanism in OptiPilot. Upon receiving a forecast-based recommendation from the forecaster, the controller’s scaling actuator computes a target replica count from the p90 RPS forecast, a per-service capacity constant (requests per replica), and a configurable headroom factor. The actuator then patches the Deployment’s spec.replicas field through the Kubernetes API using client-go.

OptiPilot directly controls replica counts rather than manipulating HPA parameters. This design provides deterministic scaling behavior: the controller sets the exact replica count it has computed, without interference from a secondary control loop. The operator retains the option to run HPA alongside OptiPilot as an independent safety net; in this configuration, HPA may scale above OptiPilot’s floor but the two systems do not coordinate.

## **B. Vertical Scaling (Experimental)**

Vertical scaling adjusts the CPU and memory resource requests and limits of existing pods within a Deployment. OptiPilot supports vertical scaling as a configurable experimental feature, enabled per service via the optipilot.io/vertical-scaling annotation. When enabled, the forecaster generates resource allocation recommendations based on CPU and memory utilization trends, and the actuator patches the Deployment’s container resource specifications.

Vertical scaling carries inherent operational risks. In Kubernetes versions prior to 1.27, modifying resource requests requires pod recreation, introducing brief service disruption. Even with the In-Place Pod Resize feature available in Kubernetes 1.27 and later, vertical scaling operates on a slower feedback loop and is more disruptive than horizontal scaling. OptiPilot documents these risks and requires explicit operator opt-in per service.

## **C. Replica Count Computation**

The target replica count is computed from the p90 RPS forecast using the following formula:

*replicas \= ⌈ rps\_p90 / capacity\_per\_replica ⌉ × (1 \+ headroom\_factor)*

where capacity\_per\_replica is a per-service constant representing the maximum sustainable request rate of a single replica at target utilization, and headroom\_factor is a configurable safety margin (default 20%). The computed value is clamped to the per-service minimum and maximum replica bounds before actuation.

# **VIII. OPERATING MODES**

OptiPilot supports three operating modes, configurable globally and overridable per service. The operating mode determines how the system acts on its forecasts and scaling recommendations.

## **A. Shadow Mode**

In shadow mode, OptiPilot performs the complete forecasting and decision pipeline — collecting metrics, generating predictions, computing scaling recommendations — but does not execute any scaling actions. All recommendations are logged to the audit trail and displayed on the dashboard with the annotation that they represent hypothetical actions. Shadow mode is the default operating mode for newly discovered services and serves as the initial observation phase during which operators evaluate forecast accuracy before granting the system scaling authority.

## **B. Recommend Mode**

In recommend mode, OptiPilot computes scaling recommendations and presents them to the operator via the dashboard as pending actions. The operator reviews each recommendation and explicitly approves or rejects it through the dashboard interface. Approved actions are executed immediately; rejected actions are logged for future model evaluation. Recommend mode provides a human-in-the-loop operational model suited to risk-sensitive environments or initial deployment phases.

## **C. Autonomous Mode**

In autonomous mode, OptiPilot executes scaling recommendations automatically, subject to safety constraints (scaling bounds, cooldown timers, rate limits). The operator monitors actions through the dashboard and audit trail and retains the ability to pause or revert actions via the kill switch or per-service pause controls. Autonomous mode provides the full value of predictive scaling by eliminating human latency from the scaling loop.

# **IX. SERVICE LIFECYCLE**

OptiPilot manages each monitored service through a defined lifecycle that governs the progression from initial observation to autonomous scaling:

**Learning Phase.** Upon discovery of a newly labeled service, OptiPilot begins collecting metrics from Prometheus and forwarding them to the forecaster for storage. No predictions are generated and no scaling actions are taken. The dashboard displays the service with a “Learning” status indicator. This phase continues until sufficient historical data has accumulated for model training (configurable minimum, default 24 hours).

**Shadow Phase.** Once the forecaster has trained an initial model, the service transitions to shadow mode. Predictions are generated and logged, and the dashboard displays forecast accuracy metrics alongside the service’s actual traffic. Operators observe prediction quality without operational risk.

**Active Phase.** When the operator is satisfied with forecast accuracy, they promote the service to recommend or autonomous mode via the dashboard or annotation update. OptiPilot begins generating actionable scaling recommendations or executing scaling actions depending on the selected mode.

**Degraded Phase.** If the drift detector identifies sustained forecast degradation, the service automatically reverts to a less aggressive mode (autonomous → recommend → shadow) until model quality recovers through retraining. This ensures that model degradation does not propagate to harmful scaling decisions.

# **X. SAFETY AND OPERATIONAL CONTROLS**

## **A. Kill Switch**

OptiPilot provides a global kill switch accessible via the dashboard interface and Kubernetes API (ConfigMap or annotation update). Activating the kill switch immediately suspends all scaling actions across all services. The system transitions to shadow mode and continues collecting metrics and generating predictions without executing any Deployment modifications. Existing replica counts remain unchanged. The kill switch is also available at per-service granularity, allowing operators to pause OptiPilot on individual services while maintaining active management of others.

## **B. Scaling Bounds**

Every monitored service is subject to minimum and maximum replica constraints, configurable via Kubernetes annotations with system-wide defaults in the YAML configuration. OptiPilot will never scale a service below its minimum or above its maximum replica count regardless of forecast values. Scaling bounds serve as a hard safety floor and ceiling that cannot be overridden by the forecasting or actuation logic.

## **C. Audit Trail**

Every scaling decision, whether executed, recommended, or simulated in shadow mode, is recorded in the audit trail with complete contextual metadata: timestamp, service identifier, previous replica count, target replica count, forecast values (p50 and p90), model version, confidence score, scaling mode, and human-readable reasoning. The audit trail is persisted in SQLite and accessible through the dashboard API, supporting post-incident analysis, capacity planning reviews, and model evaluation.

## **D. Cooldown Enforcement**

Scale-up and scale-down actions are subject to independent configurable cooldown timers (defaults: 120 seconds for scale-up, 600 seconds for scale-down). A scaling action for a given service is suppressed if the most recent action of the same direction occurred within the cooldown window. This prevents oscillatory scaling behavior and provides system stability during transient fluctuations.

## **E. Rate Limiting**

The maximum replica change per scaling cycle is bounded to prevent extreme scaling actions from a single forecast. By default, the system limits replica changes to 50% of the current count per cycle, ensuring that even an erroneous forecast cannot cause a disruptive instantaneous capacity change.

## **F. Automatic Rollback**

When OptiPilot executes a scaling action and subsequently detects service degradation (increased error rate or latency above configurable thresholds) within a monitoring window following the action, the system automatically reverts to the previous replica count, logs the rollback event, and transitions the service to conservative or shadow mode for investigation.

# **XI. MONITORING DASHBOARD**

OptiPilot includes a web-based monitoring dashboard implemented with Next.js and served as static files by the Go controller. The dashboard provides real-time operational visibility into the system’s behavior through a dark-themed, Grafana-inspired interface.

The dashboard communicates with the controller via two channels: a WebSocket connection for real-time event streaming (scaling decisions, metric updates, mode changes, alerts) and REST API endpoints for historical data queries and control operations (mode switching, kill switch activation, retrain triggers). Dashboard panels include:

* Live Traffic Panel: Real-time RPS per service with forecast overlay displaying predicted p50 and p90 bands against actual observed traffic.

* Replica Status Panel: Current replica count per service with animated visualization of scaling events as they occur.

* Model Health Panel: Per-service model version, current MAPE, confidence score, scaling mode, last training and recalibration timestamps.

* Forecast Accuracy Panel: Historical comparison of predicted versus actual RPS over configurable time windows, enabling operators to evaluate model performance.

* Scaling Audit Panel: Scrolling log of all scaling decisions with full contextual metadata, filterable by service, mode, and time range.

* Control Panel: Global and per-service mode switching, kill switch, manual retrain trigger, and scaling parameter overrides.

# **XII. DEPLOYMENT MODEL**

## **A. Helm Chart**

OptiPilot is distributed as a Helm chart for standardized Kubernetes deployment. The chart provisions all required resources: controller Deployment, forecaster Deployment, ConfigMap for system configuration, ServiceAccount with RBAC permissions for Deployment and HPA manipulation, PersistentVolumeClaims for SQLite storage, and Service resources for internal gRPC communication and dashboard access.

## **B. Prerequisites**

OptiPilot requires a running Prometheus instance within the target cluster (or accessible via network) with standard Kubernetes service metrics available. The system is compatible with Kubernetes versions 1.24 and later. No modifications to monitored services are required beyond applying the optipilot.io/enabled label to their Deployments.

## **C. Resource Requirements**

The controller operates with minimal resource footprint: approximately 128MB memory and 100m CPU under typical workloads. The forecaster requires additional resources for model training: approximately 512MB memory and 500m CPU, with temporary spikes during full retraining cycles. Both components support configurable resource requests and limits via Helm values.

# **XIII. TECHNOLOGY STACK**

| Component | Technology | Justification |
| :---- | :---- | :---- |
| Controller | Go 1.23+ | Static compilation, native Kubernetes client, efficient concurrency via goroutines |
| Forecaster | Python 3.11+, FastAPI | Access to scientific computing ecosystem, LightGBM, pandas, scikit-learn |
| ML Model | LightGBM (quantile regression) | Sub-millisecond inference, native quantile support, minimal resource requirements |
| Inter-service Communication | gRPC with Protocol Buffers | Strongly-typed contract, low-latency serialization, code generation for both languages |
| Dashboard Frontend | Next.js (React) | Component-based UI, WebSocket integration, extensive charting ecosystem |
| Dashboard Transport | WebSocket \+ REST | Real-time event push and historical data queries |
| Kubernetes Integration | client-go (informers, typed clients) | Native Kubernetes API access with watch, list, and patch capabilities |
| Metrics Source | Prometheus (PromQL HTTP API) | Industry-standard metrics infrastructure, no service instrumentation required |
| Storage | SQLite (configurable to PostgreSQL) | Embedded zero-dependency storage for controller and forecaster data |
| Scheduling | APScheduler (Python) | Background task scheduling for retraining and recalibration within forecaster |
| Containerization | Docker (multi-stage builds) | Minimal production images for both controller and forecaster |
| Deployment | Helm Chart | Standardized Kubernetes application packaging and configuration |
| Local Development | kind (Kubernetes in Docker) | Lightweight local cluster for development and integration testing |
| Load Testing | Vegeta | Scriptable HTTP load generation for validation and benchmarking |

# **XIV. SCOPE AND BOUNDARIES**

## **A. Within Scope**

* Predictive horizontal autoscaling of stateless HTTP microservices running as Kubernetes Deployments

* Experimental vertical scaling of CPU and memory resource allocations with per-service opt-in

* Per-service isolated LightGBM quantile regression models with automated lifecycle management

* Prometheus-based metrics ingestion via PromQL for request rate, CPU, memory, latency, and error rate

* Kubernetes label-based automatic service discovery with annotation-driven per-service configuration

* Three operating modes: shadow, recommend, and autonomous with per-service and global configurability

* Safety layer: kill switch, scaling bounds, cooldown enforcement, rate limiting, automatic rollback, audit trail

* Real-time monitoring dashboard with WebSocket-driven updates and operational control interface

* Helm chart for standardized deployment into any Kubernetes cluster with Prometheus

* gRPC-based internal communication with Protocol Buffer contract

* SQLite and PostgreSQL configurable storage backends

* Single-cluster operation

## **B. Outside Scope**

* Stateful workloads including databases, caches, and message queues

* Multi-cluster federation and cross-cluster scaling coordination

* Deep learning forecasting models (LSTM, Transformer, Temporal Fusion Transformer)

* Service mesh integration (Istio, Linkerd) for traffic management

* Cost optimization and cloud provider billing integration

* Authentication and multi-tenant access control for the dashboard

* Metrics source integrations beyond Prometheus (Datadog, CloudWatch, direct scraping)

*End of Project Statement*