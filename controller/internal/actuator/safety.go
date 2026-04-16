package actuator

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/optipilot/controller/internal/config"
	"github.com/optipilot/controller/internal/kube"
	"github.com/optipilot/controller/internal/models"
)

const (
	annoKillSwitch        = "optipilot.io/kill-switch"
	annoCooldownScaleUp   = "optipilot.io/cooldown-scale-up"
	annoCooldownScaleDown = "optipilot.io/cooldown-scale-down"
	killSwitchCacheTTL    = 30 * time.Second
	defaultCMGlobalKey    = "global"
	defaultCMServiceKey   = "service."
)

type SafetyDecision struct {
	Allowed        bool
	TargetReplicas int32
	Reason         string
}

type killSwitchSnapshot struct {
	fetchedAt time.Time
	global    bool
	services  map[string]bool
}

type Safety struct {
	cfg     config.ScalingConfig
	kubeCfg config.KubeConfig
	kube    kube.Client
	logger  *slog.Logger

	mu                sync.Mutex
	lastScaleUpAt     map[string]time.Time
	lastScaleDownAt   map[string]time.Time
	conservativeUntil map[string]time.Time
	killSwitch        killSwitchSnapshot
}

func NewSafety(cfg config.ScalingConfig, kubeCfg config.KubeConfig, kubeClient kube.Client, logger *slog.Logger) *Safety {
	if logger == nil {
		logger = slog.Default()
	}
	return &Safety{
		cfg:               cfg,
		kubeCfg:           kubeCfg,
		kube:              kubeClient,
		logger:            logger.With("component", "safety"),
		lastScaleUpAt:     make(map[string]time.Time),
		lastScaleDownAt:   make(map[string]time.Time),
		conservativeUntil: make(map[string]time.Time),
		killSwitch: killSwitchSnapshot{
			services: make(map[string]bool),
		},
	}
}

func (s *Safety) Evaluate(
	ctx context.Context,
	target models.ServiceTarget,
	currentReplicas int32,
	recommendedReplicas int32,
) (SafetyDecision, error) {
	serviceKey := fullServiceName(target)
	now := time.Now()

	if currentReplicas < 1 {
		currentReplicas = 1
	}
	if recommendedReplicas < 1 {
		recommendedReplicas = 1
	}

	if until := s.conservativeModeUntil(serviceKey); now.Before(until) {
		return SafetyDecision{
			Allowed:        false,
			TargetReplicas: currentReplicas,
			Reason:         fmt.Sprintf("service in conservative mode until %s", until.UTC().Format(time.RFC3339)),
		}, nil
	}

	globalKilled, serviceKilled, err := s.killSwitchState(ctx, target)
	if err != nil {
		return SafetyDecision{}, fmt.Errorf("check kill switch: %w", err)
	}
	if globalKilled {
		return SafetyDecision{Allowed: false, TargetReplicas: currentReplicas, Reason: "global kill switch enabled"}, nil
	}
	if serviceKilled {
		return SafetyDecision{Allowed: false, TargetReplicas: currentReplicas, Reason: "service kill switch enabled"}, nil
	}

	minReplicas := target.MinReplicas
	maxReplicas := target.MaxReplicas
	if minReplicas <= 0 {
		minReplicas = s.cfg.DefaultMinReplicas
	}
	if maxReplicas < minReplicas {
		maxReplicas = s.cfg.DefaultMaxReplicas
		if maxReplicas < minReplicas {
			maxReplicas = minReplicas
		}
	}

	next := recommendedReplicas
	if next < minReplicas {
		next = minReplicas
	}
	if next > maxReplicas {
		next = maxReplicas
	}

	if next != currentReplicas {
		maxDelta := int32(math.Ceil(float64(currentReplicas) * float64(s.cfg.MaxChangePercent) / 100.0))
		if maxDelta < 1 {
			maxDelta = 1
		}
		if next > currentReplicas+maxDelta {
			next = currentReplicas + maxDelta
		}
		if next < currentReplicas-maxDelta {
			next = currentReplicas - maxDelta
			if next < 1 {
				next = 1
			}
		}
	}

	if next == currentReplicas {
		return SafetyDecision{Allowed: false, TargetReplicas: currentReplicas, Reason: "no replica change after safety checks"}, nil
	}

	cooldown, up := s.cooldownForChange(target, currentReplicas, next)
	if cooldown > 0 {
		if blocked, wait := s.cooldownBlocked(serviceKey, up, now, cooldown); blocked {
			return SafetyDecision{
				Allowed:        false,
				TargetReplicas: currentReplicas,
				Reason:         fmt.Sprintf("cooldown active for %s", wait.Round(time.Second)),
			}, nil
		}
	}

	return SafetyDecision{
		Allowed:        true,
		TargetReplicas: next,
		Reason:         "safety checks passed",
	}, nil
}

func (s *Safety) MarkScaled(target models.ServiceTarget, oldReplicas, newReplicas int32) {
	serviceKey := fullServiceName(target)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if newReplicas > oldReplicas {
		s.lastScaleUpAt[serviceKey] = now
	} else if newReplicas < oldReplicas {
		s.lastScaleDownAt[serviceKey] = now
	}
}

func (s *Safety) SetConservativeMode(target models.ServiceTarget) {
	minutes := s.cfg.ConservativeModeMinutes
	if minutes <= 0 {
		minutes = 30
	}
	until := time.Now().Add(time.Duration(minutes) * time.Minute)
	serviceKey := fullServiceName(target)
	s.mu.Lock()
	s.conservativeUntil[serviceKey] = until
	s.mu.Unlock()

	s.logger.Warn("service moved to conservative mode",
		"service", target.Name,
		"namespace", target.Namespace,
		"until", until.UTC().Format(time.RFC3339),
	)
}

func (s *Safety) conservativeModeUntil(serviceKey string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.conservativeUntil[serviceKey]
	if !ok {
		return time.Time{}
	}
	if time.Now().After(until) {
		delete(s.conservativeUntil, serviceKey)
		return time.Time{}
	}
	return until
}

func (s *Safety) cooldownForChange(target models.ServiceTarget, oldReplicas, newReplicas int32) (time.Duration, bool) {
	if newReplicas > oldReplicas {
		if d, ok := parseAnnotationDuration(target.Annotations[annoCooldownScaleUp]); ok {
			return d, true
		}
		return time.Duration(s.cfg.ScaleUpCooldownSec) * time.Second, true
	}
	if d, ok := parseAnnotationDuration(target.Annotations[annoCooldownScaleDown]); ok {
		return d, false
	}
	return time.Duration(s.cfg.ScaleDownCooldownSec) * time.Second, false
}

func (s *Safety) cooldownBlocked(
	serviceKey string,
	scaleUp bool,
	now time.Time,
	cooldown time.Duration,
) (bool, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var last time.Time
	if scaleUp {
		last = s.lastScaleUpAt[serviceKey]
	} else {
		last = s.lastScaleDownAt[serviceKey]
	}
	if last.IsZero() {
		return false, 0
	}
	remaining := cooldown - now.Sub(last)
	return remaining > 0, remaining
}

func (s *Safety) killSwitchState(ctx context.Context, target models.ServiceTarget) (global bool, service bool, err error) {
	if s.cfg.GlobalKillSwitch {
		return true, false, nil
	}
	if v, ok := parseBool(target.Annotations[annoKillSwitch]); ok && v {
		return false, true, nil
	}

	namespace := strings.TrimSpace(s.kubeCfg.KillSwitchConfigMapNamespace)
	if namespace == "" {
		namespace = target.Namespace
	}
	name := strings.TrimSpace(s.kubeCfg.KillSwitchConfigMapName)
	if name == "" || s.kube == nil {
		return false, false, nil
	}

	snapshot, err := s.getKillSwitchSnapshot(ctx, namespace, name)
	if err != nil {
		return false, false, err
	}
	if snapshot.global {
		return true, false, nil
	}

	serviceKey := fullServiceName(target)
	byFullService := snapshot.services[serviceKey]
	byServiceName := snapshot.services[target.Name]
	return false, byFullService || byServiceName, nil
}

func (s *Safety) getKillSwitchSnapshot(ctx context.Context, namespace, name string) (killSwitchSnapshot, error) {
	s.mu.Lock()
	cached := s.killSwitch
	s.mu.Unlock()
	if !cached.fetchedAt.IsZero() && time.Since(cached.fetchedAt) < killSwitchCacheTTL {
		return cached, nil
	}

	cm, err := s.kube.GetConfigMap(ctx, namespace, name)
	if err != nil {
		return killSwitchSnapshot{}, fmt.Errorf("read kill-switch configmap %s/%s: %w", namespace, name, err)
	}

	globalKey := strings.TrimSpace(s.kubeCfg.KillSwitchGlobalKey)
	if globalKey == "" {
		globalKey = defaultCMGlobalKey
	}
	servicePrefix := strings.TrimSpace(s.kubeCfg.KillSwitchServicePrefix)
	if servicePrefix == "" {
		servicePrefix = defaultCMServiceKey
	}

	snapshot := killSwitchSnapshot{
		fetchedAt: time.Now(),
		services:  make(map[string]bool),
	}
	for key, raw := range cm.Data {
		val, ok := parseBool(raw)
		if !ok || !val {
			continue
		}
		if key == globalKey {
			snapshot.global = true
			continue
		}
		if strings.HasPrefix(key, servicePrefix) {
			serviceKey := strings.TrimPrefix(key, servicePrefix)
			serviceKey = strings.TrimSpace(serviceKey)
			if serviceKey != "" {
				snapshot.services[serviceKey] = true
			}
		}
	}

	s.mu.Lock()
	s.killSwitch = snapshot
	s.mu.Unlock()
	return snapshot, nil
}

func fullServiceName(target models.ServiceTarget) string {
	return target.Namespace + "/" + target.Name
}

func parseAnnotationDuration(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs <= 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
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
