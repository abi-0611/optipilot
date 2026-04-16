### **Project Identity**
* **Name:** OptiPilot
* [cite_start]**Tagline:** Machine Learning-Driven Predictive Autoscaling and Orchestration for Cloud-Native Kubernetes Infrastructure[cite: 1, 2].
* [cite_start]**Core Definition:** OptiPilot is a Kubernetes-native intelligent autoscaling product that employs machine learning-based time-series forecasting to predict microservice traffic demand[cite: 8]. [cite_start]It proactively adjusts computational resources before performance degradation occurs, operating service-agnostically over any stateless HTTP workload labeled for monitoring[cite: 8, 13].

### **Team & Timeline**
* **You:** Go controller, infrastructure, dashboard, and ML integration.
* **Friend (Unavailable):** Originally assigned Python ML, now covered by you.
* **Third Person:** Documentation and presentation.
* **Timeline:** 3 weeks total (Currently building v1).

---

### **The Core Problem**
[cite_start]The prevailing approach to Kubernetes autoscaling, the Horizontal Pod Autoscaler (HPA), is fundamentally reactive[cite: 19, 20]. [cite_start]It detects demand increases only after they manifest as resource pressure, introducing an unavoidable latency of 60 to 180 seconds while new pods are scheduled and initialized[cite: 20, 29]. [cite_start]This latency gap causes elevated response latency, increased error rates, and Service-Level Objective (SLO) violations during demand surges[cite: 18, 30]. [cite_start]OptiPilot solves this by forecasting demand 5 and 15 minutes ahead, scaling infrastructure proactively before the spike arrives[cite: 9, 23].

---

### **System Architecture (Two-Component Deployment)**
[cite_start]OptiPilot is architected as two distinct components deployed within the target Kubernetes cluster[cite: 38]. 

#### **1. Pod 1: OptiPilot Controller (Go)**
[cite_start]A single Go binary serving as the operational core[cite: 39, 46]. 
* [cite_start]**Metrics Collector:** Queries Prometheus via the PromQL HTTP API every 15 seconds, caching data in an embedded SQLite database[cite: 49, 51, 92].
* [cite_start]**Service Discovery:** Automatically detects services and configuration changes by watching Kubernetes Deployments labeled with `optipilot.io/enabled=true` via client-go informers[cite: 52, 53].
* [cite_start]**Scaling Actuator:** Applies forecasting decisions through direct Kubernetes API manipulation (patching replica counts or resource requests/limits)[cite: 54, 55].
* [cite_start]**Safety Engine:** Enforces bounds, cooldowns, rate limits, and kill switches[cite: 56].
* [cite_start]**Dashboard Server:** Serves the Next.js frontend as static files, maintaining WebSockets for real-time streaming and REST for historical queries[cite: 58].

#### **2. Pod 2: OptiPilot Forecaster (Python)**
[cite_start]A Python service handling all machine learning functionality[cite: 40, 63].
* [cite_start]**Inference Engine:** Executes per-service LightGBM quantile models to generate expected demand (p50) and upper-bound forecasts (p90)[cite: 65, 109, 111].
* [cite_start]**Model Registry:** Manages model versions and promotion using an embedded SQLite database[cite: 66, 67].
* [cite_start]**Training Pipeline:** Executes scheduled incremental and full retraining cycles, as well as bias recalibration[cite: 68, 69].
* [cite_start]**Drift Detector:** Monitors rolling Mean Absolute Percentage Error (MAPE) to automatically degrade operational modes if forecast quality drops[cite: 71, 72].

#### **Communication Pathways**
* [cite_start]**Controller ↔ Forecaster:** gRPC using Protocol Buffers[cite: 41, 76].
* [cite_start]**Controller → Kubernetes API:** Native Go `client-go`[cite: 76].
* [cite_start]**Controller → Prometheus:** HTTP via PromQL API[cite: 76].
* [cite_start]**Controller → Dashboard:** WebSocket (events) and REST (HTTP/JSON)[cite: 76].

---

### **Machine Learning Strategy**
* [cite_start]**Model:** Per-service isolated LightGBM quantile regression models[cite: 102].
* [cite_start]**Outputs:** The p50 quantile provides expected demand, while the p90 quantile drives scaling decisions, ensuring provisioned capacity meets demand with 90% probability[cite: 109, 111, 112].
* [cite_start]**Horizons:** 5-minute primary horizon (drives scaling), 15-minute secondary horizon (dashboard visibility)[cite: 118, 120].
* [cite_start]**Features:** A 19-dimensional feature vector consisting of 7 lag values, 6 rolling statistics, 3 temporal features, and 3 resource features[cite: 124, 125].
* [cite_start]**Lifecycle:** Initial Training → Bias Recalibration (5-min intervals) → Incremental Retraining (10-min intervals) → Full Retraining (24-hour intervals)[cite: 128, 130, 132, 135].

---

### **Scaling Mechanisms**
* [cite_start]**Horizontal Scaling (Primary):** Directly controls replica counts by patching the Deployment's `spec.replicas` field[cite: 148, 150]. [cite_start]Target replicas are computed from the p90 RPS forecast[cite: 163].
* **Vertical Scaling (Experimental):** Adjusts CPU and memory resource requests/limits of existing pods. [cite_start]Requires explicit per-service opt-in via annotation due to inherent operational risks[cite: 156, 157, 159].

---

### **Operating Modes & Service Lifecycle**
[cite_start]Modes dictate how the system acts on forecasts, configurable globally or per-service[cite: 168, 169].
1.  **Shadow Mode:** Simulates forecasting without executing scaling actions. [cite_start]Used for initial observation[cite: 171, 173].
2.  [cite_start]**Recommend Mode:** Presents scaling recommendations on the dashboard for human-in-the-loop approval[cite: 175, 177].
3.  [cite_start]**Autonomous Mode:** Fully automated execution of scaling actions[cite: 178, 181].

[cite_start]**Service Lifecycle Phase Progression:** Learning (data collection) → Shadow (initial observation) → Active (recommend/autonomous) → Degraded (auto-reverting to a safer mode if drift is detected)[cite: 184, 187, 190, 192].

---

### **Safety Layer (v1)**
* [cite_start]**Kill Switch:** Global and per-service controls to immediately suspend all scaling actions[cite: 196, 197].
* [cite_start]**Scaling Bounds:** Strict minimum and maximum replica floors/ceilings[cite: 201, 203].
* [cite_start]**Audit Trail:** Every simulated, recommended, or executed scaling decision is logged with contextual metadata[cite: 205].
* [cite_start]**Cooldown Enforcement:** Default 120s for scale-up and 600s for scale-down to prevent oscillatory behavior[cite: 208, 210].
* [cite_start]**Rate Limiting:** Replica changes are bounded (default max 50% change per cycle) to prevent disruptive instantaneous shifts[cite: 212, 213].
* [cite_start]**Automatic Rollback:** Automatically reverts the replica count if service degradation is detected following a scaling action[cite: 214, 215].

---

### **Service Discovery & Metrics Configuration**
[cite_start]Services are discovered via the `optipilot.io/enabled: "true"` label[cite: 79]. 
[cite_start]Metrics are fetched exclusively from an existing Prometheus instance[cite: 90]. [cite_start]The queried metrics include: Request Rate (RPS), CPU utilization, Memory utilization, Response Latency (p50, p95, p99), and Error rate[cite: 94]. 

[cite_start]Key per-service annotations include[cite: 83]:
* `optipilot.io/metrics-port`
* `optipilot.io/min-replicas` / `max-replicas`
* `optipilot.io/scaling-tier`
* `optipilot.io/mode`
* `optipilot.io/vertical-scaling`
* `optipilot.io/cooldown-scale-up` / `cooldown-scale-down`

---

### **Monitoring Dashboard**
[cite_start]A Next.js, dark-themed interface providing real-time visibility[cite: 217, 218]. Built via vibe-coding. [cite_start]Features panels for Live Traffic, Replica Status, Model Health, Forecast Accuracy, Scaling Audit, and Operational Control[cite: 221, 222, 223, 224, 225, 226].

---

### **Project Status & Scope**

**What is Built (Current State):**
* gRPC proto contracts and generated Go/Python stubs.
* 3 test Go microservices.
* Go controller foundation (config, models, store, discovery, collector, main).
* Python forecaster foundation (training and live inference).

**What is Left (v1 Scope - 3 Weeks):**
* Controller gRPC client + predictor loop.
* Controller Kubernetes `client-go` integration + actuator.
* Forecaster scheduled retraining, recalibration, and drift detection.
* Controller dashboard REST/WebSocket server and the Next.js UI.
* Infrastructure setup: Kind cluster, Prometheus, Kubernetes manifests, Helm chart, Dockerfiles.
* Load testing scripts (Vegeta) and end-to-end integration testing.

**Explicitly Out of Scope (For v1):**
* [cite_start]Stateful workloads (databases, caches)[cite: 256].
* [cite_start]Multi-cluster federation[cite: 257].
* Non-Kubernetes environments.
* [cite_start]Deep learning models (LSTM, TFT)[cite: 258].
* [cite_start]Service mesh integrations[cite: 259].

***