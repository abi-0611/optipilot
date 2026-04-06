# OptiPilot AI - Refactoring Prerequisites & Knowledge Base

## 🎯 Before You Start: What You MUST Understand

### 1. **Kubernetes Fundamentals** (Critical)

You cannot refactor this without deep K8s knowledge:

#### Core Concepts
- **Custom Resource Definitions (CRDs)** - How K8s extends its API
- **Controllers & Reconciliation Loop** - The heart of operators
  - Watch → Compare → Actuate pattern
  - Eventually consistent state management
  - Informers, Work Queues, Rate Limiting
- **client-go library** - K8s Go client patterns
  - Dynamic client vs Typed client
  - Scheme registration (`runtime.Scheme`)
  - RESTMapper, GVK (GroupVersionKind)
- **controller-runtime** - Kubebuilder framework
  - `Manager` - lifecycle orchestrator
  - `Reconciler` interface - `Reconcile(ctx, Request)`
  - `Client` - cached K8s API client
  - Event Recorder, Predicates, Finalizers

#### Resources You Must Know
```go
// These K8s resources are directly manipulated:
- Deployment (apps/v1)
- HorizontalPodAutoscaler (autoscaling/v2)
- ConfigMap (v1)
- Namespace (v1)
- Unstructured (for Karpenter NodePool)
```

**Study Path:**
1. Read: "Programming Kubernetes" book (Ch 1-4)
2. Build: Simple operator with kubebuilder
3. Understand: `internal/controller/*_controller.go` reconciliation logic

---

### 2. **Prometheus & PromQL** (Critical)

SLO evaluation depends entirely on Prometheus:

#### Must Know
- **PromQL Basics**
  - Rate functions: `rate()`, `increase()`
  - Aggregation: `sum()`, `avg()`, `histogram_quantile()`
  - Label matching: `{namespace="default"}`
- **HTTP API**
  - `/api/v1/query` - instant queries
  - `/api/v1/query_range` - time-series
  - Result formats: vector, matrix, scalar
- **Metric Types**
  - Counter, Gauge, Histogram, Summary

**Study Path:**
1. Read: `/internal/metrics/prometheus_client.go` - HTTP client implementation
2. Read: `/internal/slo/promql_builder.go` - Query construction
3. Read: `/internal/slo/evaluator.go` - Burn-rate calculation

**Example Query This System Generates:**
```promql
histogram_quantile(0.99,
  sum(rate(http_request_duration_seconds_bucket{
    namespace="default",
    deployment="my-app"
  }[5m])) by (le)
)
```

---

### 3. **CEL (Common Expression Language)** (Medium)

Users write policies in CEL:

#### Core Concepts
- **Environment Setup** - Declare variables and functions
- **Type System** - Primitive types, structs, maps, lists
- **Compilation vs Evaluation** - Two-phase execution
- **Extensions** - Custom functions via `cel.Function()`

**Study Path:**
1. Read: `/internal/cel/environment.go` - How to create CEL environment
2. Read: `/internal/cel/types.go` - Struct declarations with `cel:` tags
3. Read: `/internal/cel/functions.go` - Custom function implementations
4. Read: `/internal/cel/engine.go` - PolicyEngine orchestration

**Key Pattern:**
```go
// Step 1: Compile (at policy creation time)
env := cel.NewOptiPilotEnv()
ast, issues := env.Compile("spotRisk(candidate.instance_type) < 0.3")
prg, _ := env.Program(ast)

// Step 2: Evaluate (for each candidate)
result, _ := prg.Eval(map[string]interface{}{
    "candidate": CandidatePlan{InstanceType: "m5.large"},
})
```

---

### 4. **Multi-Objective Optimization** (Medium)

The solver uses Pareto optimization:

#### Concepts You Must Understand
- **Pareto Dominance** - A dominates B if better/equal on all objectives and strictly better on at least one
- **Pareto Front** - Set of non-dominated solutions
- **Weighted Sum Method** - Combining multiple objectives
- **Normalization** - Scaling objectives to 0-1 range

**Study Path:**
1. Read: `/internal/engine/pareto.go` - Pareto front calculation
2. Read: `/internal/engine/scorer.go` - 4-dimensional scoring
3. Read: `/internal/engine/solver.go` - Full pipeline orchestration

**The 4 Objectives:**
```go
// All normalized to 0-1 scale
type CandidateScore struct {
    SLOCompliance  float64  // maximize → higher is better
    Cost           float64  // minimize → lower is better
    Carbon         float64  // minimize → lower is better  
    TenantFairness float64  // maximize → higher is better
}
```

**Algorithm:**
1. Generate 50+ candidate configurations (Cartesian product)
2. Score each on 4 dimensions (normalize 0-1)
3. Filter via CEL constraints (hard/soft)
4. Find Pareto front (4D dominance check)
5. Select best from front (weighted sum + disruption tie-break)

---

### 5. **Go Patterns Used in This Codebase** (Critical)

#### Interface-Based Design
```go
// Abstraction for testability and swappability
type PrometheusClient interface {
    Query(ctx, query) (float64, error)
    QueryRange(ctx, query, start, end, step) ([]DataPoint, error)
}

// Production implementation
type HTTPPrometheusClient struct { ... }

// Test implementation
type MockPrometheusClient struct { ... }
```

#### Dependency Injection
```go
// Controller receives dependencies via constructor
func NewServiceObjectiveReconciler(
    client client.Client,
    scheme *runtime.Scheme,
    evaluator *slo.SLOEvaluator,  // injected
    recorder record.EventRecorder,
) *ServiceObjectiveReconciler { ... }
```

#### Options Pattern
```go
type ActuationOptions struct {
    DryRun    bool
    Canary    bool
    MaxChange float64
}

result := actuator.Apply(ctx, ref, action, ActuationOptions{
    DryRun: true,
    Canary: false,
})
```

#### Context Propagation
```go
// Context carries cancellation, deadlines, and request-scoped values
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Pass ctx down to all I/O operations
    slo, err := r.getSLO(ctx, req.NamespacedName)
    metric, err := r.promClient.Query(ctx, query)
}
```

#### Error Wrapping
```go
if err := r.Client.Update(ctx, slo); err != nil {
    return ctrl.Result{}, fmt.Errorf("updating SLO status: %w", err)
}
```

---

### 6. **Code Generation (Kubebuilder)** (Critical)

Parts of this code are **AUTO-GENERATED**:

#### Generated Files (DO NOT EDIT MANUALLY)
```
api/*/v1alpha1/zz_generated.deepcopy.go  # DeepCopy methods
config/crd/bases/*.yaml                  # CRD manifests
config/rbac/*                            # RBAC roles
```

#### Generation Commands
```bash
# After modifying API types (api/*/v1alpha1/*_types.go)
make generate   # Generates DeepCopy methods
make manifests  # Generates CRDs, RBAC, Webhooks

# MUST run after:
# - Adding/removing fields in CRD structs
# - Changing kubebuilder markers (//+kubebuilder:...)
```

#### Kubebuilder Markers
```go
// Annotation-based code generation directives
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced
//+kubebuilder:printcolumn:name="Compliant",type=string,JSONPath=`.status.conditions[?(@.type=="SLOCompliant")].status`
type ServiceObjective struct { ... }
```

---

### 7. **Testing Infrastructure** (Important)

#### Testing Frameworks
- **Ginkgo/Gomega** - BDD-style testing
- **envtest** - Fake K8s API server for controller tests
- **httptest** - Mock HTTP servers

#### Test Patterns
```go
// Controller tests use envtest (real K8s API)
BeforeEach(func() {
    k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
    Expect(err).NotTo(HaveOccurred())
})

It("should reconcile ServiceObjective", func() {
    ctx := context.Background()
    slo := &slov1alpha1.ServiceObjective{ ... }
    Expect(k8sClient.Create(ctx, slo)).To(Succeed())
    
    Eventually(func() bool {
        _ = k8sClient.Get(ctx, key, slo)
        return meta.IsStatusConditionTrue(slo.Status.Conditions, "SLOCompliant")
    }, timeout, interval).Should(BeTrue())
})

// Unit tests mock dependencies
It("should score candidates", func() {
    mockProm := &MockPrometheusClient{
        QueryFunc: func(ctx, q string) (float64, error) {
            return 0.9995, nil
        },
    }
    scorer := NewScorer(input, mockProm)
    scores := scorer.ScoreAll(candidates)
    Expect(scores[0].SLOCompliance).To(BeNumerically("~", 1.0))
})
```

#### Test Command
```bash
make test        # Unit + controller tests (uses envtest)
make test-e2e    # E2E tests (requires kind cluster)
```

---

### 8. **Architecture Layer Dependencies** (CRITICAL)

This is the **contract map** - how layers interact:

```
┌──────────────────────────────────────────────────────────────┐
│                     cmd/manager/main.go                       │
│                  (Dependency Injection Root)                  │
└────────────┬─────────────────────────────────────────────────┘
             │
             ├──► Controller Layer
             │    ├─ ServiceObjectiveReconciler
             │    │  └─ DEPENDS ON: slo.SLOEvaluator
             │    ├─ OptimizationPolicyReconciler
             │    │  └─ DEPENDS ON: cel.PolicyEngine
             │    └─ OptimizerController (periodic loop)
             │       └─ DEPENDS ON: engine.Solver, actuator.Registry
             │
             ├──► SLO Layer (internal/slo/)
             │    └─ SLOEvaluator
             │       └─ DEPENDS ON: metrics.PrometheusClient
             │
             ├──► CEL Layer (internal/cel/)
             │    └─ PolicyEngine
             │       └─ DEPENDS ON: google/cel-go library
             │
             ├──► Engine Layer (internal/engine/)
             │    └─ Solver
             │       └─ DEPENDS ON: cel.PolicyEngine
             │       └─ OUTPUT: ScalingAction
             │
             ├──► Actuator Layer (internal/actuator/)
             │    └─ Registry (dispatch)
             │       ├─ PodActuator
             │       ├─ NodeActuator
             │       └─ AppTuner
             │       └─ DEPENDS ON: client.Client (K8s API)
             │
             ├──► Explainability Layer (internal/explainability/)
             │    └─ Journal (SQLite)
             │       └─ DEPENDS ON: modernc.org/sqlite
             │
             └──► API Layer (internal/api/)
                  └─ Server (REST API)
                     ├─ DecisionsAPIHandler → Journal
                     ├─ WhatIfAPIHandler → Simulator
                     └─ TenantAPIHandler → TenantManager
```

#### Key Interfaces (Contracts Between Layers)

```go
// Layer: Metrics
type PrometheusClient interface {
    Query(ctx, query) (float64, error)
    QueryRange(ctx, query, start, end, step) ([]DataPoint, error)
}

// Layer: CEL
type PolicyEngine interface {
    Compile(*OptimizationPolicy) error
    Evaluate(policyKey string, ctx EvalContext) (EvalResult, error)
}

// Layer: Engine
type Solver interface {
    Solve(*SolverInput) (ScalingAction, DecisionRecord, error)
}

// Layer: Actuator
type Actuator interface {
    Apply(ctx, ref, action, opts) (ActuationResult, error)
    Rollback(ctx, ref) error
    CanApply(action) bool
}

// Layer: Explainability
type DecisionJournal interface {
    Write(DecisionRecord) error
    Query(QueryFilter) ([]DecisionRecord, error)
}
```

**Refactoring Rule:**
- If you change an interface, you break every implementation
- Always check `grep -r "implements <InterfaceName>"` before modifying

---

### 9. **External Dependencies You Must Understand**

#### controller-runtime (v0.19.3)
```go
import (
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Core patterns:
manager := ctrl.NewManager(...)          // Lifecycle orchestrator
reconciler := &MyReconciler{Client: ...} // Implements reconcile.Reconciler
reconciler.SetupWithManager(manager)     // Register controller
manager.Start(ctx)                       // Run event loop
```

#### google/cel-go (v0.21.0)
```go
import (
    "github.com/google/cel-go/cel"
    "github.com/google/cel-go/ext"
)

env, _ := cel.NewEnv(
    cel.Variable("candidate", cel.ObjectType("CandidatePlan")),
    ext.NativeTypes(&CandidatePlan{}),
    ext.ParseStructTags(true),
)
```

#### modernc.org/sqlite (v1.48.1)
```go
import (
    "database/sql"
    _ "modernc.org/sqlite"
)

db, _ := sql.Open("sqlite", "/path/to/decisions.db")
db.Exec("PRAGMA journal_mode=WAL")  // Write-Ahead Logging
```

#### Prometheus client_golang (v1.20.4)
Used only for exposing metrics, NOT for querying (uses HTTP client).

---

### 10. **Build & Tooling** (Important)

#### Required Tools
```bash
# Go toolchain
go 1.25.0

# K8s development
controller-gen v0.16.4   # CRD/RBAC generation
kustomize v5.4.3         # YAML templating
envtest                  # Fake K8s API for tests

# Container tools
docker                   # Image building
ko v0.17.1              # Distroless Go image builder
kind                     # Local K8s cluster

# Linting
golangci-lint v1.61.0
```

#### Build Commands
```bash
make manifests    # Generate CRDs
make generate     # Generate DeepCopy code
make fmt          # Format Go code
make vet          # Run go vet
make lint         # Run golangci-lint
make test         # Run tests
make build        # Build binary
```

---

## 📋 Refactoring Checklist

Before touching ANY code, ensure you can answer YES to all:

### Phase 1: Understanding
- [ ] Can you explain the Kubernetes reconciliation loop?
- [ ] Do you understand how CRDs extend the K8s API?
- [ ] Can you write a basic PromQL query?
- [ ] Do you understand Pareto optimization?
- [ ] Can you explain dependency injection in Go?
- [ ] Have you read all interface definitions in `internal/*/interface.go`?
- [ ] Can you trace a request from CRD creation → actuation?

### Phase 2: Setup
- [ ] Can you run `make test` successfully?
- [ ] Can you run `make manifests` without errors?
- [ ] Can you build the project with `make build`?
- [ ] Can you deploy to kind with `./hack/quickstart.sh --build-local`?
- [ ] Can you query the API at `http://localhost:8090/api/v1/decisions`?
- [ ] Can you trigger an optimization by creating a ServiceObjective CR?

### Phase 3: Testing Infrastructure
- [ ] Can you write a unit test for a pure function?
- [ ] Can you write a controller test using envtest?
- [ ] Can you mock the PrometheusClient interface?
- [ ] Do you understand Ginkgo's `Describe/Context/It` structure?
- [ ] Can you debug a failing test?

### Phase 4: Code Navigation
- [ ] Can you find where ServiceObjective reconciliation happens?
- [ ] Can you find where candidates are generated?
- [ ] Can you find where CEL constraints are evaluated?
- [ ] Can you find where HPA is patched?
- [ ] Can you find where decisions are written to SQLite?

---

## 🔍 Critical Files to Study (In Order)

### Step 1: Entry Point & Wiring
1. `cmd/manager/main.go` (248 lines) - **Start here**
   - How components are initialized
   - Dependency injection
   - Flag parsing

### Step 2: API Types (CRD Definitions)
2. `api/slo/v1alpha1/serviceobjective_types.go`
3. `api/policy/v1alpha1/optimizationpolicy_types.go`
4. `api/tenant/v1alpha1/tenantprofile_types.go`

### Step 3: Controllers (Business Logic)
5. `internal/controller/serviceobjective_controller.go` (~200 lines)
6. `internal/controller/optimizationpolicy_controller.go` (~300 lines)
7. `internal/controller/optimizer_controller.go` (~400 lines) - **Core loop**

### Step 4: Domain Logic
8. `internal/slo/evaluator.go` - SLO burn-rate calculation
9. `internal/cel/engine.go` - CEL policy compilation
10. `internal/engine/solver.go` - Multi-objective optimization
11. `internal/actuator/pod_actuator.go` - K8s resource manipulation

### Step 5: Infrastructure
12. `internal/metrics/prometheus_client.go` - Prometheus HTTP client
13. `internal/explainability/journal.go` - SQLite decision log
14. `internal/api/server.go` - REST API server

---

## 🚨 Common Pitfalls When Refactoring

### 1. **Breaking Controller Registration**
```go
// WRONG: Forgot to call SetupWithManager
reconciler := &MyReconciler{...}
// Controller never runs!

// RIGHT:
if err := reconciler.SetupWithManager(mgr); err != nil {
    log.Fatal(err)
}
```

### 2. **Forgetting to Regenerate Code**
```go
// After adding a field to ServiceObjective:
type ServiceObjectiveSpec struct {
    NewField string `json:"newField"` // Added this
}

// MUST RUN:
// make generate  # Regenerates DeepCopy
// make manifests # Regenerates CRD YAML
```

### 3. **Context Not Propagated**
```go
// WRONG: Creates new context
func (r *Reconciler) helper() {
    ctx := context.Background() // Lost cancellation!
    r.Client.Get(ctx, key, obj)
}

// RIGHT: Pass context down
func (r *Reconciler) helper(ctx context.Context) {
    r.Client.Get(ctx, key, obj)
}
```

### 4. **Interface Changes Without Updating Mocks**
```go
// Changed interface:
type PrometheusClient interface {
    Query(ctx, query, timeout) (float64, error) // Added timeout param
}

// MUST UPDATE all implementations:
// - HTTPPrometheusClient (production)
// - MockPrometheusClient (tests)
// Otherwise: compilation errors
```

### 5. **Modifying Generated Files**
```
# These are regenerated by make manifests/generate:
api/*/v1alpha1/zz_generated.deepcopy.go  # DO NOT EDIT
config/crd/bases/*.yaml                  # DO NOT EDIT
```

---

## 🎓 Study Plan (1-2 Weeks)

### Week 1: Fundamentals
**Day 1-2:** Kubernetes
- Read controller-runtime docs
- Build a hello-world operator with kubebuilder

**Day 3:** Prometheus
- Learn PromQL basics
- Query a Prometheus instance manually

**Day 4-5:** Code Reading
- Trace a ServiceObjective CR from creation → reconciliation → SLO check
- Read all interfaces in `internal/*/interface.go`

### Week 2: Deep Dive
**Day 6:** CEL
- Study `/internal/cel/` package
- Write a simple CEL expression evaluator

**Day 7:** Solver
- Study `/internal/engine/` package
- Understand candidate generation → scoring → Pareto selection

**Day 8:** Actuators
- Study `/internal/actuator/` package
- Understand how K8s resources are patched

**Day 9-10:** Testing
- Write a unit test for a pure function
- Write a controller test using envtest
- Run full test suite with `make test`

---

## ✅ You're Ready to Refactor When...

- [ ] You can explain the data flow end-to-end without looking at code
- [ ] You can add a new field to a CRD and regenerate everything correctly
- [ ] You can write a test for any component
- [ ] You can run the project locally and see it optimize a workload
- [ ] You understand every interface and its implementations
- [ ] You can modify one layer without breaking others

---

## 📚 Additional Resources

### Books
- **"Programming Kubernetes"** by Michael Hausenblas & Stefan Schimanski
- **"Kubernetes Patterns"** by Bilgin Ibryam & Roland Huß

### Official Docs
- [Kubebuilder Book](https://book.kubebuilder.io/)
- [controller-runtime GoDoc](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
- [CEL Language Spec](https://github.com/google/cel-spec)
- [Prometheus Querying](https://prometheus.io/docs/prometheus/latest/querying/basics/)

### Code Examples
- [Kubebuilder Examples](https://github.com/kubernetes-sigs/kubebuilder/tree/master/docs/book/src)
- [controller-runtime Examples](https://github.com/kubernetes-sigs/controller-runtime/tree/main/examples)

---

## 🎯 Next Steps

1. **Set up development environment**
   ```bash
   make test      # Verify everything works
   make build     # Build the binary
   ./hack/quickstart.sh --build-local  # Deploy locally
   ```

2. **Read the critical files** (see list above)

3. **Trace a full request** with debugger:
   - Create ServiceObjective CR
   - Watch reconciler trigger
   - See SLO evaluation
   - Watch optimization cycle
   - See actuation

4. **Write a test** for a component you want to refactor
   - Ensures you understand current behavior
   - Safety net for refactoring

5. **Start refactoring** one small component at a time

---

**Remember:** This is a complex distributed system. Don't try to refactor everything at once. Start small, write tests, and validate each change.
