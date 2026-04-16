// Package discovery provides service discovery for the controller. Services
// are the unit of autoscaling — each discovered service gets its own metrics
// pipeline, forecasting model, and scaling decisions. Discovery is pluggable:
// static config for local dev, Kubernetes informers for production.
package discovery

import (
	"context"

	"github.com/optipilot/controller/internal/models"
)

// ServiceDiscovery is the contract between the discovery layer and the rest
// of the controller. Implementations must be safe for concurrent use.
type ServiceDiscovery interface {
	// Discover returns the current set of services. For static discovery
	// this always returns the same list; for Kubernetes it reflects the
	// current cluster state.
	Discover(ctx context.Context) ([]models.ServiceTarget, error)

	// Watch returns a channel of events emitted when the service set
	// changes. For static discovery this channel never emits. The channel
	// is closed when Stop is called or ctx is cancelled.
	Watch(ctx context.Context) (<-chan DiscoveryEvent, error)

	// Stop releases resources and closes any watch channel.
	Stop()
}

// EventType classifies what changed in the service set.
type EventType int

const (
	EventAdded EventType = iota
	EventRemoved
	EventUpdated
)

func (e EventType) String() string {
	switch e {
	case EventAdded:
		return "added"
	case EventRemoved:
		return "removed"
	case EventUpdated:
		return "updated"
	default:
		return "unknown"
	}
}

// DiscoveryEvent signals that a service was added, removed, or had its
// configuration updated. Consumers (e.g. the collector) use these to react
// without re-polling the full list.
type DiscoveryEvent struct {
	Type    EventType
	Service models.ServiceTarget
}
