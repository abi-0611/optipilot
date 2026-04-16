package discovery

import (
	"context"
	"fmt"

	"github.com/optipilot/controller/internal/config"
	"github.com/optipilot/controller/internal/models"
)

// TODO: Full Kubernetes discovery implementation plan:
//
// 1. Dependencies: add k8s.io/client-go and k8s.io/apimachinery to go.mod.
//
// 2. Use a SharedInformerFactory scoped to cfg.Namespace and filtered by
//    cfg.LabelSelector to watch Deployment resources. This gives us list+watch
//    with local caching — no polling the API server.
//
// 3. Register AddFunc/UpdateFunc/DeleteFunc event handlers that:
//    a. Extract service metadata from Deployment annotations:
//       - optipilot.io/metrics-port   → ServiceTarget.MetricsPort
//       - optipilot.io/min-replicas   → ServiceTarget.MinReplicas
//       - optipilot.io/max-replicas   → ServiceTarget.MaxReplicas
//       - optipilot.io/mode           → ServiceTarget.ScalingMode
//       - optipilot.io/scaling-tier   → (future use, store in Annotations)
//    b. Construct a models.ServiceTarget from the Deployment's name,
//       namespace, labels, and the annotations above.
//    c. Emit a DiscoveryEvent (EventAdded / EventUpdated / EventRemoved)
//       on the watch channel.
//
// 4. Discover() returns the current contents of the informer's local cache
//    converted to []models.ServiceTarget.
//
// 5. ResyncIntervalSec controls how often the informer does a full re-list
//    to catch any events that may have been missed.
//
// 6. Stop() calls the informer factory's cancel function and closes the
//    watch channel.

var errNotImplemented = fmt.Errorf(
	"kubernetes discovery not implemented yet: requires client-go informers " +
		"watching Deployments with label optipilot.io/enabled=true")

// KubernetesDiscovery watches a Kubernetes cluster for Deployments annotated
// with optipilot.io labels. Not yet implemented — see TODO above.
type KubernetesDiscovery struct {
	cfg config.KubernetesConfig
}

func NewKubernetesDiscovery(cfg config.KubernetesConfig) *KubernetesDiscovery {
	return &KubernetesDiscovery{cfg: cfg}
}

func (d *KubernetesDiscovery) Discover(_ context.Context) ([]models.ServiceTarget, error) {
	return nil, errNotImplemented
}

func (d *KubernetesDiscovery) Watch(_ context.Context) (<-chan DiscoveryEvent, error) {
	return nil, errNotImplemented
}

func (d *KubernetesDiscovery) Stop() {}
