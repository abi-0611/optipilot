package discovery

import (
	"context"
	"sync"

	"github.com/optipilot/controller/internal/config"
	"github.com/optipilot/controller/internal/models"
)

// StaticDiscovery returns a fixed list of services read from the YAML config.
// The watch channel never emits — a static list doesn't change at runtime.
// Used during local development and testing where there is no Kubernetes
// cluster to query.
type StaticDiscovery struct {
	targets []models.ServiceTarget
	watchCh chan DiscoveryEvent
	once    sync.Once
}

// NewStaticDiscovery converts config entries into ServiceTargets and returns
// a ready-to-use discovery instance. The conversion is intentionally eager
// so that callers see any data problems at startup, not mid-flight.
func NewStaticDiscovery(services []config.StaticServiceConfig, globalScalingMode string) *StaticDiscovery {
	targets := make([]models.ServiceTarget, len(services))
	for i, s := range services {
		targets[i] = models.ServiceTarget{
			Name:        s.Name,
			Namespace:   s.Namespace,
			MetricsPort: s.MetricsPort,
			MinReplicas: s.MinReplicas,
			MaxReplicas: s.MaxReplicas,
			ScalingMode: globalScalingMode,
		}
	}
	return &StaticDiscovery{
		targets: targets,
		watchCh: make(chan DiscoveryEvent),
	}
}

func (d *StaticDiscovery) Discover(_ context.Context) ([]models.ServiceTarget, error) {
	out := make([]models.ServiceTarget, len(d.targets))
	copy(out, d.targets)
	return out, nil
}

// Watch returns a channel that is never written to, because the static
// service list never changes.
func (d *StaticDiscovery) Watch(_ context.Context) (<-chan DiscoveryEvent, error) {
	return d.watchCh, nil
}

func (d *StaticDiscovery) Stop() {
	d.once.Do(func() { close(d.watchCh) })
}
