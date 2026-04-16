package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/optipilot/controller/internal/config"
	"github.com/optipilot/controller/internal/kube"
	"github.com/optipilot/controller/internal/models"
)

const (
	optiPilotEnabledLabel = "optipilot.io/enabled"
	defaultLabelSelector  = optiPilotEnabledLabel + "=true"

	annoMetricsPort     = "optipilot.io/metrics-port"
	annoMinReplicas     = "optipilot.io/min-replicas"
	annoMaxReplicas     = "optipilot.io/max-replicas"
	annoMode            = "optipilot.io/mode"
	annoScalingTier     = "optipilot.io/scaling-tier"
	annoVerticalScaling = "optipilot.io/vertical-scaling"
)

// KubernetesDiscovery discovers ServiceTarget values from Kubernetes Deployments
// labeled with optipilot.io/enabled=true.
type KubernetesDiscovery struct {
	cfg                config.KubernetesConfig
	kube               kube.Client
	logger             *slog.Logger
	defaultMode        string
	defaultMinReplicas int32
	defaultMaxReplicas int32

	mu      sync.RWMutex
	targets map[string]models.ServiceTarget
	watchCh chan DiscoveryEvent

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once

	initErr error
}

func NewKubernetesDiscovery(
	cfg config.KubernetesConfig,
	defaultMode string,
	defaultMinReplicas int32,
	defaultMaxReplicas int32,
	kubeClient kube.Client,
	logger *slog.Logger,
) *KubernetesDiscovery {
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())
	d := &KubernetesDiscovery{
		cfg:                cfg,
		kube:               kubeClient,
		logger:             logger.With("component", "kubernetes_discovery"),
		defaultMode:        defaultMode,
		defaultMinReplicas: defaultMinReplicas,
		defaultMaxReplicas: defaultMaxReplicas,
		targets:            make(map[string]models.ServiceTarget),
		watchCh:            make(chan DiscoveryEvent, 128),
		ctx:                ctx,
		cancel:             cancel,
	}

	if kubeClient == nil {
		d.initErr = fmt.Errorf("kubernetes client is nil")
		return d
	}

	labelSelector := strings.TrimSpace(cfg.LabelSelector)
	if labelSelector == "" {
		labelSelector = defaultLabelSelector
	}

	resync := time.Duration(cfg.ResyncIntervalSec) * time.Second
	informer, err := kubeClient.WatchDeployments(ctx, cfg.Namespace, labelSelector, resync)
	if err != nil {
		d.initErr = fmt.Errorf("start deployment watch: %w", err)
		return d
	}

	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			d.handleAddOrUpdate(obj, EventAdded)
		},
		UpdateFunc: func(oldObj, newObj any) {
			oldDep, oldOK := oldObj.(*appsv1.Deployment)
			newDep, newOK := newObj.(*appsv1.Deployment)
			if oldOK && newOK && oldDep.ResourceVersion == newDep.ResourceVersion {
				return
			}
			d.handleAddOrUpdate(newObj, EventUpdated)
		},
		DeleteFunc: d.handleDelete,
	}); err != nil {
		d.initErr = fmt.Errorf("register deployment handlers: %w", err)
	}

	d.logger.Info("kubernetes discovery initialized",
		"namespace", cfg.Namespace,
		"label_selector", labelSelector,
		"resync_interval_sec", cfg.ResyncIntervalSec,
	)

	return d
}

func (d *KubernetesDiscovery) Discover(ctx context.Context) ([]models.ServiceTarget, error) {
	_ = ctx
	if d.initErr != nil {
		return nil, fmt.Errorf("kubernetes discovery unavailable: %w", d.initErr)
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	targets := make([]models.ServiceTarget, 0, len(d.targets))
	for _, t := range d.targets {
		targets = append(targets, t)
	}
	return targets, nil
}

func (d *KubernetesDiscovery) Watch(ctx context.Context) (<-chan DiscoveryEvent, error) {
	if d.initErr != nil {
		return nil, fmt.Errorf("kubernetes discovery unavailable: %w", d.initErr)
	}

	go func() {
		<-ctx.Done()
		d.Stop()
	}()

	return d.watchCh, nil
}

func (d *KubernetesDiscovery) Stop() {
	d.once.Do(func() {
		d.cancel()
	})
}

func (d *KubernetesDiscovery) handleAddOrUpdate(obj any, eventType EventType) {
	dep, ok := obj.(*appsv1.Deployment)
	if !ok || dep == nil {
		return
	}

	target := deploymentToTarget(dep, d.defaultMode, d.defaultMinReplicas, d.defaultMaxReplicas)
	key := deploymentKey(dep.Namespace, dep.Name)

	d.mu.Lock()
	d.targets[key] = target
	d.mu.Unlock()

	d.emit(DiscoveryEvent{
		Type:    eventType,
		Service: target,
	})
}

func (d *KubernetesDiscovery) handleDelete(obj any) {
	dep, ok := obj.(*appsv1.Deployment)
	if !ok || dep == nil {
		// Handle tombstones from informer cache.
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		dep, ok = tombstone.Obj.(*appsv1.Deployment)
		if !ok || dep == nil {
			return
		}
	}

	key := deploymentKey(dep.Namespace, dep.Name)

	d.mu.Lock()
	target, exists := d.targets[key]
	if exists {
		delete(d.targets, key)
	}
	d.mu.Unlock()

	if !exists {
		target = deploymentToTarget(dep, d.defaultMode, d.defaultMinReplicas, d.defaultMaxReplicas)
	}

	d.emit(DiscoveryEvent{
		Type:    EventRemoved,
		Service: target,
	})
}

func (d *KubernetesDiscovery) emit(event DiscoveryEvent) {
	select {
	case d.watchCh <- event:
	default:
		d.logger.Warn("dropping discovery event; channel full",
			"service", event.Service.Name,
			"namespace", event.Service.Namespace,
			"type", event.Type,
		)
	}
}

func deploymentToTarget(
	dep *appsv1.Deployment,
	defaultMode string,
	defaultMinReplicas int32,
	defaultMaxReplicas int32,
) models.ServiceTarget {
	if defaultMinReplicas <= 0 {
		defaultMinReplicas = 2
	}
	if defaultMaxReplicas < defaultMinReplicas {
		defaultMaxReplicas = defaultMinReplicas
	}
	if strings.TrimSpace(defaultMode) == "" {
		defaultMode = config.ScalingModeShadow
	}

	minReplicas := defaultMinReplicas
	maxReplicas := defaultMaxReplicas
	metricsPort := int32(8080)

	annotations := copyStringMap(dep.Annotations)

	if val, ok := parseInt32(annotations[annoMinReplicas]); ok {
		minReplicas = val
	}
	if val, ok := parseInt32(annotations[annoMaxReplicas]); ok {
		maxReplicas = val
	}
	if minReplicas < 1 {
		minReplicas = 1
	}
	if maxReplicas < minReplicas {
		maxReplicas = minReplicas
	}

	if val, ok := parseInt32(annotations[annoMetricsPort]); ok && val > 0 {
		metricsPort = val
	} else if inferred, ok := inferMetricsPort(dep); ok {
		metricsPort = inferred
	}

	mode := strings.TrimSpace(annotations[annoMode])
	if mode == "" {
		mode = defaultMode
	}
	scalingTier := strings.TrimSpace(annotations[annoScalingTier])
	if scalingTier == "" {
		scalingTier = "standard"
	}

	verticalScaling, _ := parseBool(annotations[annoVerticalScaling])

	return models.ServiceTarget{
		Name:            dep.Name,
		Namespace:       dep.Namespace,
		Labels:          copyStringMap(dep.Labels),
		Annotations:     annotations,
		MetricsPort:     int(metricsPort),
		MinReplicas:     minReplicas,
		MaxReplicas:     maxReplicas,
		ScalingMode:     mode,
		ScalingTier:     scalingTier,
		VerticalScaling: verticalScaling,
		LastSeen:        time.Now(),
	}
}

func deploymentKey(namespace, name string) string {
	return namespace + "/" + name
}

func copyStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func parseInt32(raw string) (int32, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(n), true
}

func parseBool(raw string) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, false
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return v, true
}

func inferMetricsPort(dep *appsv1.Deployment) (int32, bool) {
	if dep.Spec.Template.Spec.Containers == nil || len(dep.Spec.Template.Spec.Containers) == 0 {
		return 0, false
	}
	for _, c := range dep.Spec.Template.Spec.Containers {
		for _, p := range c.Ports {
			if p.ContainerPort > 0 {
				return p.ContainerPort, true
			}
		}
	}
	return 0, false
}
