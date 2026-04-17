package actuator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/optipilot/controller/internal/config"
	"github.com/optipilot/controller/internal/discovery"
	"github.com/optipilot/controller/internal/forecaster"
	"github.com/optipilot/controller/internal/kube"
	"github.com/optipilot/controller/internal/models"
	"github.com/optipilot/controller/internal/store"
)

const (
	annoVerticalCPU       = "optipilot.io/vertical-cpu"
	annoVerticalMemory    = "optipilot.io/vertical-memory"
	annoVerticalContainer = "optipilot.io/vertical-container"
)

type Actuator struct {
	store  store.Store
	kube   kube.Client
	safety *Safety
	cfg    config.ScalingConfig
	logger *slog.Logger

	modeMu        sync.RWMutex
	modeOverrides map[string]string

	targetResolver func(context.Context, string) (models.ServiceTarget, error)
	decisionHook   func(models.ScalingDecision)
}

func New(
	st store.Store,
	kubeClient kube.Client,
	cfg config.ScalingConfig,
	kubeCfg config.KubeConfig,
	logger *slog.Logger,
) *Actuator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Actuator{
		store:         st,
		kube:          kubeClient,
		cfg:           cfg,
		safety:        NewSafety(cfg, kubeCfg, kubeClient, logger),
		logger:        logger.With("component", "actuator"),
		modeOverrides: make(map[string]string),
	}
}

func (a *Actuator) SetDecisionHook(hook func(models.ScalingDecision)) {
	a.decisionHook = hook
}

func (a *Actuator) SetTargetResolver(resolver func(context.Context, string) (models.ServiceTarget, error)) {
	a.targetResolver = resolver
}

func (a *Actuator) SetServiceMode(serviceName, mode string) error {
	mode = strings.TrimSpace(mode)
	switch mode {
	case config.ScalingModeShadow, config.ScalingModeRecommend, config.ScalingModeAutonomous:
	default:
		return fmt.Errorf("unsupported mode %q", mode)
	}
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return errors.New("service name required")
	}
	a.modeMu.Lock()
	a.modeOverrides[serviceName] = mode
	a.modeMu.Unlock()
	return nil
}

func (a *Actuator) GetServiceMode(serviceName string) (string, bool) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return "", false
	}
	a.modeMu.RLock()
	defer a.modeMu.RUnlock()
	mode, ok := a.modeOverrides[serviceName]
	return mode, ok
}

func (a *Actuator) GetEffectiveServiceMode(target models.ServiceTarget) string {
	return a.effectiveMode(target)
}

func (a *Actuator) SetGlobalKillSwitch(enabled bool) {
	a.safety.SetGlobalKillSwitch(enabled)
}

func (a *Actuator) GlobalKillSwitchEnabled() bool {
	return a.safety.GlobalKillSwitchEnabled()
}

func (a *Actuator) SetServicePaused(serviceName string, paused bool) {
	a.safety.SetServicePaused(serviceName, paused)
}

func (a *Actuator) ServicePaused(serviceName string) bool {
	return a.safety.ServicePaused(serviceName)
}

func (a *Actuator) RunWatcher(ctx context.Context, disc discovery.ServiceDiscovery) {
	ch, err := disc.Watch(ctx)
	if err != nil {
		a.logger.Warn("actuator watcher unavailable", "error", err)
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			a.logger.Info("actuator observed discovery event",
				"type", evt.Type.String(),
				"service", evt.Service.Name,
				"namespace", evt.Service.Namespace,
			)
		}
	}
}

func (a *Actuator) ApproveRecommendation(ctx context.Context, id int64) (*models.ScalingDecision, error) {
	decision, err := a.store.GetScalingDecisionByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load recommendation id=%d: %w", id, err)
	}
	if decision == nil {
		return nil, fmt.Errorf("recommendation id=%d not found", id)
	}
	if decision.Executed {
		return decision, nil
	}
	if a.kube == nil {
		return nil, errors.New("kubernetes client unavailable")
	}
	if a.targetResolver == nil {
		return nil, errors.New("target resolver unavailable")
	}

	target, err := a.targetResolver(ctx, decision.ServiceName)
	if err != nil {
		return nil, fmt.Errorf("resolve service target %q: %w", decision.ServiceName, err)
	}
	currentReplicas, _, err := a.currentState(ctx, target)
	if err != nil {
		return nil, err
	}
	safetyDecision, err := a.safety.Evaluate(ctx, target, currentReplicas, decision.NewReplicas)
	if err != nil {
		return nil, fmt.Errorf("safety evaluate: %w", err)
	}
	if !safetyDecision.Allowed {
		reason := "approval blocked: " + safetyDecision.Reason
		if err := a.store.UpdateScalingDecision(ctx, decision.ID, false, reason); err != nil {
			return nil, fmt.Errorf("persist blocked recommendation id=%d: %w", decision.ID, err)
		}
		decision.OldReplicas = currentReplicas
		decision.NewReplicas = safetyDecision.TargetReplicas
		decision.Reason = reason
		decision.Executed = false
		return decision, nil
	}
	if err := a.kube.PatchReplicas(ctx, target.Namespace, target.Name, safetyDecision.TargetReplicas); err != nil {
		reason := "approval failed: patch replicas failed"
		if updateErr := a.store.UpdateScalingDecision(ctx, decision.ID, false, reason); updateErr != nil {
			return nil, fmt.Errorf("patch failed (%v) and update failed: %w", err, updateErr)
		}
		decision.OldReplicas = currentReplicas
		decision.NewReplicas = safetyDecision.TargetReplicas
		decision.Reason = reason
		decision.Executed = false
		return decision, nil
	}
	a.safety.MarkScaled(target, currentReplicas, safetyDecision.TargetReplicas)
	reason := "approved and executed"
	if err := a.store.UpdateScalingDecision(ctx, decision.ID, true, reason); err != nil {
		return nil, fmt.Errorf("persist approved recommendation id=%d: %w", decision.ID, err)
	}
	decision.OldReplicas = currentReplicas
	decision.NewReplicas = safetyDecision.TargetReplicas
	decision.Reason = reason
	decision.Executed = true
	if a.decisionHook != nil {
		a.decisionHook(*decision)
	}
	return decision, nil
}

func (a *Actuator) RejectRecommendation(ctx context.Context, id int64) (*models.ScalingDecision, error) {
	decision, err := a.store.GetScalingDecisionByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load recommendation id=%d: %w", id, err)
	}
	if decision == nil {
		return nil, fmt.Errorf("recommendation id=%d not found", id)
	}
	reason := "rejected by operator"
	if err := a.store.UpdateScalingDecision(ctx, decision.ID, false, reason); err != nil {
		return nil, fmt.Errorf("persist rejected recommendation id=%d: %w", decision.ID, err)
	}
	decision.Reason = reason
	decision.Executed = false
	return decision, nil
}

func (a *Actuator) HandlePrediction(
	ctx context.Context,
	target models.ServiceTarget,
	pred *forecaster.PredictionResponse,
) (action string, reason string, err error) {
	if pred == nil {
		return "", "", fmt.Errorf("nil prediction")
	}

	effectiveMode := a.effectiveMode(target)
	switch effectiveMode {
	case config.ScalingModeShadow:
		return a.persistLoggedDecision(ctx, target, pred, 0, pred.RecommendedReplicas, false, "shadow mode")
	case config.ScalingModeRecommend:
		return a.persistLoggedDecision(ctx, target, pred, 0, pred.RecommendedReplicas, false, "recommend mode (pending approval)")
	case config.ScalingModeAutonomous:
		// continue
	default:
		return a.persistLoggedDecision(ctx, target, pred, 0, pred.RecommendedReplicas, false, "invalid scaling mode; skipped")
	}

	if a.kube == nil {
		return a.persistLoggedDecision(ctx, target, pred, 0, pred.RecommendedReplicas, false, "kubernetes client unavailable")
	}

	currentReplicas, baseline, err := a.currentState(ctx, target)
	if err != nil {
		a.logger.Warn("failed to fetch deployment state; skipping recommendation",
			"service", target.Name,
			"namespace", target.Namespace,
			"error", err,
		)
		return a.persistLoggedDecision(ctx, target, pred, 0, pred.RecommendedReplicas, false, "skipped: deployment unavailable")
	}

	safetyDecision, err := a.safety.Evaluate(ctx, target, currentReplicas, pred.RecommendedReplicas)
	if err != nil {
		return "", "", fmt.Errorf("safety evaluation for %s/%s: %w", target.Namespace, target.Name, err)
	}
	if !safetyDecision.Allowed {
		return a.persistLoggedDecision(ctx, target, pred, currentReplicas, safetyDecision.TargetReplicas, false, safetyDecision.Reason)
	}

	if err := a.kube.PatchReplicas(ctx, target.Namespace, target.Name, safetyDecision.TargetReplicas); err != nil {
		a.logger.Warn("replica patch failed",
			"service", target.Name,
			"namespace", target.Namespace,
			"old_replicas", currentReplicas,
			"new_replicas", safetyDecision.TargetReplicas,
			"error", err,
		)
		return a.persistLoggedDecision(ctx, target, pred, currentReplicas, safetyDecision.TargetReplicas, false, "patch replicas failed")
	}

	a.safety.MarkScaled(target, currentReplicas, safetyDecision.TargetReplicas)
	if err := a.maybePatchVertical(ctx, target); err != nil {
		a.logger.Warn("vertical scaling patch failed",
			"service", target.Name,
			"namespace", target.Namespace,
			"error", err,
		)
	}

	action = "scaled"
	reason = fmt.Sprintf("autonomous scale from %d to %d", currentReplicas, safetyDecision.TargetReplicas)
	if _, _, err := a.persistDecision(ctx, target, pred, currentReplicas, safetyDecision.TargetReplicas, true, reason, action); err != nil {
		return "", "", err
	}

	a.logger.Info("scaling action executed",
		"service", target.Name,
		"namespace", target.Namespace,
		"old_replicas", currentReplicas,
		"new_replicas", safetyDecision.TargetReplicas,
		"mode", effectiveMode,
		"confidence", pred.ConfidenceScore,
		"model_version", pred.ModelVersion,
	)

	if safetyDecision.TargetReplicas > currentReplicas && a.cfg.RollbackMonitoringMinutes > 0 && baseline != nil {
		a.startRollbackMonitor(target, pred, currentReplicas, safetyDecision.TargetReplicas, baseline)
	}

	return action, reason, nil
}

func (a *Actuator) effectiveMode(target models.ServiceTarget) string {
	if mode, ok := a.GetServiceMode(target.Name); ok {
		return mode
	}
	mode := strings.TrimSpace(target.ScalingMode)
	if mode == "" {
		mode = a.cfg.Mode
	}
	return mode
}

func (a *Actuator) currentState(
	ctx context.Context,
	target models.ServiceTarget,
) (currentReplicas int32, baseline *models.ServiceMetrics, err error) {
	if a.kube == nil {
		return 0, nil, fmt.Errorf("kubernetes client unavailable")
	}
	dep, err := a.kube.GetDeployment(ctx, target.Namespace, target.Name)
	if err != nil {
		return 0, nil, fmt.Errorf("get deployment %s/%s: %w", target.Namespace, target.Name, err)
	}
	if dep.Spec.Replicas == nil {
		currentReplicas = 1
	} else {
		currentReplicas = *dep.Spec.Replicas
		if currentReplicas < 1 {
			currentReplicas = 1
		}
	}
	baseline, err = a.store.GetLatestMetrics(ctx, target.Name)
	if err != nil {
		return 0, nil, fmt.Errorf("get latest metrics for %s: %w", target.Name, err)
	}
	return currentReplicas, baseline, nil
}

func (a *Actuator) maybePatchVertical(ctx context.Context, target models.ServiceTarget) error {
	if !target.VerticalScaling || a.kube == nil {
		return nil
	}
	cpu := strings.TrimSpace(target.Annotations[annoVerticalCPU])
	mem := strings.TrimSpace(target.Annotations[annoVerticalMemory])
	container := strings.TrimSpace(target.Annotations[annoVerticalContainer])
	if container == "" {
		container = target.Name
	}
	if cpu == "" || mem == "" {
		a.logger.Info("vertical scaling enabled but cpu/memory annotations are missing",
			"service", target.Name,
			"namespace", target.Namespace,
		)
		return nil
	}
	return a.kube.PatchResources(ctx, target.Namespace, target.Name, container, cpu, mem)
}

func (a *Actuator) startRollbackMonitor(
	target models.ServiceTarget,
	pred *forecaster.PredictionResponse,
	previousReplicas int32,
	appliedReplicas int32,
	baseline *models.ServiceMetrics,
) {
	go func() {
		delay := time.Duration(a.cfg.RollbackMonitoringMinutes) * time.Minute
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		latest, err := a.store.GetLatestMetrics(ctx, target.Name)
		if err != nil || latest == nil || baseline == nil {
			if err != nil {
				a.logger.Warn("rollback monitor metrics fetch failed",
					"service", target.Name,
					"namespace", target.Namespace,
					"error", err,
				)
			}
			return
		}

		if !a.shouldRollback(baseline, latest) {
			return
		}

		if err := a.kube.PatchReplicas(ctx, target.Namespace, target.Name, previousReplicas); err != nil {
			a.logger.Warn("rollback patch failed",
				"service", target.Name,
				"namespace", target.Namespace,
				"from", appliedReplicas,
				"to", previousReplicas,
				"error", err,
			)
			return
		}

		a.safety.SetConservativeMode(target)
		reason := "automatic rollback after degradation detection"
		if _, _, err := a.persistDecision(ctx, target, pred, appliedReplicas, previousReplicas, true, reason, "rollback"); err != nil {
			a.logger.Warn("persist rollback decision failed",
				"service", target.Name,
				"namespace", target.Namespace,
				"error", err,
			)
		}
		a.logger.Warn("rollback executed",
			"service", target.Name,
			"namespace", target.Namespace,
			"from", appliedReplicas,
			"to", previousReplicas,
			"reason", reason,
		)
	}()
}

func (a *Actuator) shouldRollback(baseline, latest *models.ServiceMetrics) bool {
	errorThreshold := a.cfg.RollbackErrorRateThreshold
	if errorThreshold <= 0 {
		errorThreshold = 0.02
	}
	latencyPercent := a.cfg.RollbackLatencyIncreasePercent
	if latencyPercent <= 0 {
		latencyPercent = 30
	}
	latencyThreshold := baseline.P95LatencyMs * (1 + float64(latencyPercent)/100.0)

	errorWorse := latest.ErrorRate > baseline.ErrorRate+errorThreshold
	latencyWorse := latest.P95LatencyMs > math.Max(latencyThreshold, baseline.P95LatencyMs)
	return errorWorse || latencyWorse
}

func (a *Actuator) persistLoggedDecision(
	ctx context.Context,
	target models.ServiceTarget,
	pred *forecaster.PredictionResponse,
	oldReplicas int32,
	newReplicas int32,
	executed bool,
	reason string,
) (action string, persistedReason string, err error) {
	action = "skipped"
	if !executed {
		switch a.effectiveMode(target) {
		case config.ScalingModeShadow:
			action = "shadow_logged"
		case config.ScalingModeRecommend:
			action = "recommend_logged"
		}
	}
	if _, _, err := a.persistDecision(ctx, target, pred, oldReplicas, newReplicas, executed, reason, action); err != nil {
		return "", "", err
	}
	return action, reason, nil
}

func (a *Actuator) persistDecision(
	ctx context.Context,
	target models.ServiceTarget,
	pred *forecaster.PredictionResponse,
	oldReplicas int32,
	newReplicas int32,
	executed bool,
	reason string,
	action string,
) (persistedAction string, persistedReason string, err error) {
	decision := &models.ScalingDecision{
		ServiceName:     target.Name,
		OldReplicas:     oldReplicas,
		NewReplicas:     newReplicas,
		ScalingMode:     pred.ScalingMode,
		ModelVersion:    pred.ModelVersion,
		Reason:          reason,
		RpsP50:          pred.RpsP50,
		RpsP90:          pred.RpsP90,
		ConfidenceScore: pred.ConfidenceScore,
		Executed:        executed,
		CreatedAt:       time.Now(),
	}
	if err := a.store.SaveScalingDecision(ctx, decision); err != nil {
		return "", "", fmt.Errorf("save scaling decision for %s: %w", target.Name, err)
	}
	if a.decisionHook != nil {
		a.decisionHook(*decision)
	}
	return action, reason, nil
}
