// Package store defines a persistence interface for controller state and
// provides a SQLite-backed implementation. Keeping the interface narrow lets
// us swap engines later (e.g. Postgres for HA deployments) without touching
// callers, and makes testing trivial via fakes.
package store

import (
	"context"

	"github.com/optipilot/controller/internal/models"
)

// Store is the persistence contract used by every controller subsystem.
// All methods are context-aware so callers can enforce timeouts and cancel
// in-flight queries during shutdown.
type Store interface {
	// Metrics

	// SaveMetrics persists a single observation. Hot path; keep cheap.
	SaveMetrics(ctx context.Context, m *models.ServiceMetrics) error
	// SaveMetricsBatch writes many observations in a single transaction.
	SaveMetricsBatch(ctx context.Context, metrics []models.ServiceMetrics) error
	// GetRecentMetrics returns observations for a service collected within
	// the last `minutes`, ordered oldest-first (suitable for forecasting).
	GetRecentMetrics(ctx context.Context, serviceName string, minutes int) ([]models.ServiceMetrics, error)
	// GetLatestMetrics returns the most recent observation for a service,
	// or (nil, nil) if none exist.
	GetLatestMetrics(ctx context.Context, serviceName string) (*models.ServiceMetrics, error)
	// GetAllLatestMetrics returns the most recent observation for every
	// service that has at least one row.
	GetAllLatestMetrics(ctx context.Context) (map[string]*models.ServiceMetrics, error)

	// Scaling decisions

	// SaveScalingDecision persists one decision (executed or shadow).
	SaveScalingDecision(ctx context.Context, d *models.ScalingDecision) error
	// GetRecentDecisions returns the newest `limit` decisions across all
	// services (newest first).
	GetRecentDecisions(ctx context.Context, limit int) ([]models.ScalingDecision, error)
	// GetServiceDecisions returns the newest `limit` decisions for a
	// single service (newest first).
	GetServiceDecisions(ctx context.Context, serviceName string, limit int) ([]models.ScalingDecision, error)
	// GetScalingDecisionByID fetches one decision by primary key.
	GetScalingDecisionByID(ctx context.Context, id int64) (*models.ScalingDecision, error)
	// UpdateScalingDecision updates executed/reason fields for one decision.
	UpdateScalingDecision(ctx context.Context, id int64, executed bool, reason string) error

	// Model status

	// UpsertModelStatus inserts or replaces the row for the given service.
	UpsertModelStatus(ctx context.Context, s *models.ModelStatus) error
	// GetModelStatus returns the model status for a service, or
	// (nil, nil) if none exists.
	GetModelStatus(ctx context.Context, serviceName string) (*models.ModelStatus, error)
	// GetAllModelStatuses returns model status keyed by service name.
	GetAllModelStatuses(ctx context.Context) (map[string]*models.ModelStatus, error)

	// Maintenance

	// PurgeOldMetrics deletes service_metrics rows older than the cutoff.
	PurgeOldMetrics(ctx context.Context, olderThanHours int) error
	// GetMetricsCount returns the total row count in service_metrics
	// (useful for health/observability).
	GetMetricsCount(ctx context.Context) (int64, error)
	// Close releases all resources (prepared statements, db handles).
	Close() error
}
