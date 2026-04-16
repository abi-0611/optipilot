// Package models defines the shared data types used across all controller
// packages. These types intentionally mirror the gRPC proto messages defined
// in proto/optipilot/v1/prediction.proto so that conversion between wire and
// internal representations is mechanical.
package models

import "time"

// ServiceLifecycleState is the controller's notion of where a service is in
// its onboarding journey. Drives whether scaling decisions get applied.
type ServiceLifecycleState string

const (
	// StateLearning: collecting data, no model yet.
	StateLearning ServiceLifecycleState = "learning"
	// StateShadow: model exists, decisions recorded but not applied.
	StateShadow ServiceLifecycleState = "shadow"
	// StateActive: model trusted, decisions applied to the cluster.
	StateActive ServiceLifecycleState = "active"
	// StateDegraded: model accuracy regressed; fall back to safer behavior.
	StateDegraded ServiceLifecycleState = "degraded"
)

// ServiceTarget describes a service that the controller manages. It is
// produced by the discovery layer (static config or Kubernetes informer) and
// consumed by every other component. The struct deliberately carries no
// metrics or runtime state — see ServiceState for the full picture.
type ServiceTarget struct {
	Name        string
	Namespace   string
	MetricsPort int
	MinReplicas int32
	MaxReplicas int32
	// ScalingMode is a per-service override of the global scaling mode.
	// Empty string means "inherit global default".
	ScalingMode     string
	ScalingTier     string
	VerticalScaling bool
	Labels          map[string]string
	Annotations     map[string]string
	LastSeen        time.Time
}

// ServiceMetrics is one observation of a service's runtime behavior. The
// collector emits one of these per service per scrape interval.
type ServiceMetrics struct {
	ServiceName  string
	RPS          float64
	AvgLatencyMs float64
	P95LatencyMs float64
	P99LatencyMs float64
	ActiveConns  int32
	CPUPercent   float64
	MemoryMB     float64
	ErrorRate    float64
	CollectedAt  time.Time
}

// ScalingDecision records a scaling action the controller chose to take (or
// would have taken in shadow mode). Persisted for audit and replay.
type ScalingDecision struct {
	ID          int64
	ServiceName string
	OldReplicas int32
	NewReplicas int32
	// ScalingMode here is the *strategy* that produced the decision
	// (PREDICTIVE / CONSERVATIVE / REACTIVE), not the per-service mode.
	ScalingMode     string
	ModelVersion    string
	Reason          string
	RpsP50          float64
	RpsP90          float64
	ConfidenceScore float64
	// Executed is true when the decision was actually applied to the
	// cluster; false in shadow/recommend modes where we only record it.
	Executed  bool
	CreatedAt time.Time
}

// ModelStatus captures the freshness and accuracy of a service's forecasting
// model. Updated by the model-management loop after train/recalibrate cycles.
type ModelStatus struct {
	ServiceName        string
	ModelVersion       string
	CurrentMAPE        float64
	ScalingMode        string
	LastTrainedAt      time.Time
	LastRecalibratedAt time.Time
	TrainingDataPoints int64
	UpdatedAt          time.Time
}

// ServiceState is the live, in-memory snapshot for a single service. The
// reconciliation loop assembles one of these per discovered service on every
// cycle by combining target config with the latest metrics and model status.
type ServiceState struct {
	Target          ServiceTarget
	LifecycleState  ServiceLifecycleState
	CurrentReplicas int32
	LatestMetrics   *ServiceMetrics
	LatestModel     *ModelStatus
}
