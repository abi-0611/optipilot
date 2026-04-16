package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DiscoveryModeStatic     = "static"
	DiscoveryModeKubernetes = "kubernetes"
)


const (
	ScalingModeShadow     = "shadow" // observation only, recorded in database for auditing/test
	ScalingModeRecommend  = "recommend" // Advisory mode
	ScalingModeAutonomous = "autonomous" // full automation
)

// Top level configuration for the optipilot.yaml
type Config struct {
	Prometheus PrometheusConfig `yaml:"prometheus"`
	Discovery  DiscoveryConfig  `yaml:"discovery"`
	Collector  CollectorConfig  `yaml:"collector"`
	Scaling    ScalingConfig    `yaml:"scaling"`
	Forecaster ForecasterConfig `yaml:"forecaster"`
	Server     ServerConfig     `yaml:"server"`
	Storage    StorageConfig    `yaml:"storage"`
}

// PrometheusConfig holds promethus server address and query timeout settings. 
type PrometheusConfig struct {
	Address         string `yaml:"address"` // address of the Prometheus server, e.g. "http://localhost:9090"
	QueryTimeoutSec int    `yaml:"query_timeout_sec"` // timeout for prometheus queries, in seconds
}

// DiscoveryConfig holds settings related to service discovery
// which determines what services optipilot will attempt to scale
// two modes are available static and kubernetes
// static mode for the development
// kubernetes mode for production discover through kubernetes API
type DiscoveryConfig struct {
	Mode           string                `yaml:"mode"`
	StaticServices []StaticServiceConfig `yaml:"static_services"`
	Kubernetes     KubernetesConfig      `yaml:"kubernetes"`
}

// StaticServiceConfig is the only place specific service names appear in the
// codebase. Used only when Discovery.Mode == "static" (local development).
type StaticServiceConfig struct {
	Name        string `yaml:"name"`
	Namespace   string `yaml:"namespace"`
	MetricsPort int    `yaml:"metrics_port"`
	MinReplicas int32  `yaml:"min_replicas"`
	MaxReplicas int32  `yaml:"max_replicas"`
}

type KubernetesConfig struct {
	Namespace         string `yaml:"namespace"`
	LabelSelector     string `yaml:"label_selector"`
	ResyncIntervalSec int    `yaml:"resync_interval_sec"`
}

type CollectorConfig struct {
	IntervalSec int    `yaml:"interval_sec"`
	MetricsPath string `yaml:"metrics_path"`
}


type ScalingConfig struct {
	Mode                 string  `yaml:"mode"`
	ScaleUpCooldownSec   int     `yaml:"scale_up_cooldown_sec"`
	ScaleDownCooldownSec int     `yaml:"scale_down_cooldown_sec"`
	DefaultMinReplicas   int32   `yaml:"default_min_replicas"`
	DefaultMaxReplicas   int32   `yaml:"default_max_replicas"`

	// restricts how much can scale in single decision cycle
	// example if current replicas is 10, and max_change_percent is 50
	// max scale up is 15, max scale down is 5
	MaxChangePercent     int     `yaml:"max_change_percent"`

	// adds extra capacity to predicted replica count
	// if predicted replicas is 10, and headroom_factor is 0.2, final replicas is 12
	HeadroomFactor       float64 `yaml:"headroom_factor"`
}

type ForecasterConfig struct {
	GrpcAddress string `yaml:"grpc_address"`
	TimeoutSec  int    `yaml:"timeout_sec"`
}

type ServerConfig struct {
	HTTPPort      int    `yaml:"http_port"`
	WebsocketPath string `yaml:"websocket_path"`
}

type StorageConfig struct {
	DBPath         string `yaml:"db_path"`
	RetentionHours int    `yaml:"retention_hours"`
}

// LoadConfig reads YAML from path, overlays OPTIPILOT_* env vars, applies
// defaults for unset optional fields, and validates required fields.
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	if err := applyEnvOverrides(&cfg); err != nil {
		return nil, fmt.Errorf("apply env overrides: %w", err)
	}

	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

// applyEnvOverrides applies the documented OPTIPILOT_* env var overrides.
// Only the explicitly-supported keys are honored — keep the surface narrow
// so users have a single source of truth for what is overridable.
func applyEnvOverrides(c *Config) error {
	if v := os.Getenv("OPTIPILOT_PROMETHEUS_ADDRESS"); v != "" {
		c.Prometheus.Address = v
	}
	if v := os.Getenv("OPTIPILOT_DISCOVERY_MODE"); v != "" {
		c.Discovery.Mode = v
	}
	if v := os.Getenv("OPTIPILOT_SCALING_MODE"); v != "" {
		c.Scaling.Mode = v
	}
	if v := os.Getenv("OPTIPILOT_STORAGE_DB_PATH"); v != "" {
		c.Storage.DBPath = v
	}
	if v := os.Getenv("OPTIPILOT_FORECASTER_GRPC_ADDRESS"); v != "" {
		c.Forecaster.GrpcAddress = v
	}
	if v := os.Getenv("OPTIPILOT_SERVER_HTTP_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("OPTIPILOT_SERVER_HTTP_PORT %q: %w", v, err)
		}
		c.Server.HTTPPort = p
	}
	return nil
}

func applyDefaults(c *Config) {
	if c.Prometheus.QueryTimeoutSec == 0 {
		c.Prometheus.QueryTimeoutSec = 10
	}
	if c.Discovery.Mode == "" {
		c.Discovery.Mode = DiscoveryModeStatic
	}
	if c.Discovery.Kubernetes.ResyncIntervalSec == 0 {
		c.Discovery.Kubernetes.ResyncIntervalSec = 30
	}
	if c.Collector.IntervalSec == 0 {
		c.Collector.IntervalSec = 15
	}
	if c.Collector.MetricsPath == "" {
		c.Collector.MetricsPath = "/metrics"
	}
	if c.Scaling.Mode == "" {
		c.Scaling.Mode = ScalingModeShadow
	}
	if c.Scaling.ScaleUpCooldownSec == 0 {
		c.Scaling.ScaleUpCooldownSec = 120
	}
	if c.Scaling.ScaleDownCooldownSec == 0 {
		c.Scaling.ScaleDownCooldownSec = 600
	}
	if c.Scaling.DefaultMinReplicas == 0 {
		c.Scaling.DefaultMinReplicas = 2
	}
	if c.Scaling.DefaultMaxReplicas == 0 {
		c.Scaling.DefaultMaxReplicas = 15
	}
	if c.Scaling.MaxChangePercent == 0 {
		c.Scaling.MaxChangePercent = 50
	}
	if c.Forecaster.TimeoutSec == 0 {
		c.Forecaster.TimeoutSec = 10
	}
	if c.Server.HTTPPort == 0 {
		c.Server.HTTPPort = 8080
	}
	if c.Server.WebsocketPath == "" {
		c.Server.WebsocketPath = "/ws/events"
	}
	if c.Storage.DBPath == "" {
		c.Storage.DBPath = "optipilot.db"
	}
	if c.Storage.RetentionHours == 0 {
		c.Storage.RetentionHours = 24
	}
}

func validate(c *Config) error {
	if c.Prometheus.Address == "" {
		return fmt.Errorf("prometheus.address is required")
	}

	switch c.Discovery.Mode {
	case DiscoveryModeStatic:
		if len(c.Discovery.StaticServices) == 0 {
			return fmt.Errorf("discovery.static_services must not be empty when discovery.mode is %q", DiscoveryModeStatic)
		}
		seen := make(map[string]struct{}, len(c.Discovery.StaticServices))
		for i, s := range c.Discovery.StaticServices {
			if s.Name == "" {
				return fmt.Errorf("discovery.static_services[%d].name is required", i)
			}
			if _, dup := seen[s.Name]; dup {
				return fmt.Errorf("discovery.static_services: duplicate name %q", s.Name)
			}
			seen[s.Name] = struct{}{}
			if s.MetricsPort <= 0 || s.MetricsPort > 65535 {
				return fmt.Errorf("discovery.static_services[%d] (%s): invalid metrics_port %d", i, s.Name, s.MetricsPort)
			}
			if s.MinReplicas < 0 || s.MaxReplicas < s.MinReplicas {
				return fmt.Errorf("discovery.static_services[%d] (%s): invalid replica bounds min=%d max=%d", i, s.Name, s.MinReplicas, s.MaxReplicas)
			}
		}
	case DiscoveryModeKubernetes:
		if c.Discovery.Kubernetes.Namespace == "" {
			return fmt.Errorf("discovery.kubernetes.namespace is required when discovery.mode is %q", DiscoveryModeKubernetes)
		}
	default:
		return fmt.Errorf("discovery.mode must be one of [%s, %s], got %q",
			DiscoveryModeStatic, DiscoveryModeKubernetes, c.Discovery.Mode)
	}

	switch c.Scaling.Mode {
	case ScalingModeShadow, ScalingModeRecommend, ScalingModeAutonomous:
	default:
		return fmt.Errorf("scaling.mode must be one of [%s, %s, %s], got %q",
			ScalingModeShadow, ScalingModeRecommend, ScalingModeAutonomous, c.Scaling.Mode)
	}

	if c.Scaling.HeadroomFactor < 0 || c.Scaling.HeadroomFactor > 1 {
		return fmt.Errorf("scaling.headroom_factor must be in [0, 1], got %v", c.Scaling.HeadroomFactor)
	}
	if c.Scaling.MaxChangePercent <= 0 || c.Scaling.MaxChangePercent > 100 {
		return fmt.Errorf("scaling.max_change_percent must be in (0, 100], got %d", c.Scaling.MaxChangePercent)
	}

	if c.Forecaster.GrpcAddress == "" {
		return fmt.Errorf("forecaster.grpc_address is required")
	}
	if !strings.HasPrefix(c.Server.WebsocketPath, "/") {
		return fmt.Errorf("server.websocket_path must start with '/', got %q", c.Server.WebsocketPath)
	}

	return nil
}
