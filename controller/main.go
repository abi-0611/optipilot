// OptiPilot controller — single binary entry point.
//
// This is the foundation skeleton: it loads configuration, opens the SQLite
// store, kicks off the periodic metrics-purge loop, and blocks on a shutdown
// signal. Subsequent layers (collector, gRPC client, Kubernetes client,
// HTTP/WS server) attach to this skeleton.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/optipilot/controller/internal/actuator"
	"github.com/optipilot/controller/internal/collector"
	"github.com/optipilot/controller/internal/config"
	"github.com/optipilot/controller/internal/dashboard"
	"github.com/optipilot/controller/internal/discovery"
	"github.com/optipilot/controller/internal/forecaster"
	"github.com/optipilot/controller/internal/kube"
	"github.com/optipilot/controller/internal/models"
	"github.com/optipilot/controller/internal/predictor"
	"github.com/optipilot/controller/internal/store"
)

const purgeInterval = time.Hour

func main() {
	// CLI flag wins; otherwise consult OPTIPILOT_CONFIG; otherwise default.
	defaultConfigPath := os.Getenv("OPTIPILOT_CONFIG")
	if defaultConfigPath == "" {
		defaultConfigPath = "optipilot.yaml"
	}
	configPath := flag.String("config", defaultConfigPath, "path to OptiPilot YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath); err != nil {
		slog.Error("controller exited with error", "error", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return err
	}

	st, err := store.NewSQLiteStore(cfg.Storage.DBPath)
	if err != nil {
		return err
	}
	// Always close the store on exit so WAL is checkpointed cleanly.
	defer func() {
		if err := st.Close(); err != nil {
			slog.Error("store close failed", "error", err)
		}
	}()

	var kubeClient kube.Client
	kubeClient, err = kube.InitClient(cfg.Kube.KubeconfigPath, slog.Default())
	if err != nil {
		slog.Warn("kubernetes client unavailable; running in degraded mode", "error", err)
	}

	// Initialize service discovery based on configured mode.
	disc, err := newDiscovery(cfg, kubeClient)
	if err != nil {
		return err
	}
	defer disc.Stop()

	act := actuator.New(st, kubeClient, cfg.Scaling, cfg.Kube, slog.Default())
	events := dashboard.NewEventBus(slog.Default())

	// Dial the forecaster for predictor RPCs. If client construction fails
	// (for example, malformed target), keep running collector/purge loops.
	var pred *predictor.Predictor
	fcClient, err := forecaster.NewClient(
		cfg.Forecaster.GrpcAddress,
		time.Duration(cfg.Forecaster.TimeoutSec)*time.Second,
		slog.Default(),
	)
	if err != nil {
		slog.Warn("forecaster client unavailable; predictor disabled",
			"forecaster_grpc", cfg.Forecaster.GrpcAddress,
			"error", err,
		)
	} else {
		defer func() {
			if err := fcClient.Close(); err != nil {
				slog.Error("forecaster client close failed", "error", err)
			}
		}()
		pred = predictor.New(disc, st, fcClient, predictor.PredictorConfig{
			IntervalSec:     cfg.Predictor.IntervalSec,
			LookbackMinutes: cfg.Predictor.LookbackMinutes,
			MinDataPoints:   cfg.Predictor.MinDataPoints,
			IngestBatchSize: cfg.Predictor.IngestBatchSize,
		}, slog.Default())
		pred.SetRecommendationHandler(act)
		pred.SetPredictionHook(func(target models.ServiceTarget, pred *forecaster.PredictionResponse) {
			events.Publish(dashboard.NewEvent(dashboard.EventTypePrediction, dashboard.PredictionData{
				Service:      pred.ServiceName,
				P50:          pred.RpsP50,
				P90:          pred.RpsP90,
				Replicas:     pred.RecommendedReplicas,
				Mode:         act.GetEffectiveServiceMode(target),
				Confidence:   pred.ConfidenceScore,
				ModelVersion: pred.ModelVersion,
			}))
		})
	}

	act.SetTargetResolver(func(ctx context.Context, serviceName string) (models.ServiceTarget, error) {
		services, err := disc.Discover(ctx)
		if err != nil {
			return models.ServiceTarget{}, err
		}
		for _, svc := range services {
			if svc.Name == serviceName {
				return svc, nil
			}
		}
		return models.ServiceTarget{}, fmt.Errorf("service %q not found", serviceName)
	})
	act.SetDecisionHook(func(d models.ScalingDecision) {
		events.Publish(dashboard.NewEvent(dashboard.EventTypeScalingDecision, dashboard.ScalingDecisionData{
			Service:     d.ServiceName,
			OldReplicas: d.OldReplicas,
			NewReplicas: d.NewReplicas,
			Reason:      d.Reason,
			Executed:    d.Executed,
			Timestamp:   d.CreatedAt.UTC(),
		}))
	})

	startupFields := []any{
		"discovery_mode", cfg.Discovery.Mode,
		"scaling_mode", cfg.Scaling.Mode,
		"prometheus_address", cfg.Prometheus.Address,
		"db_path", cfg.Storage.DBPath,
		"http_port", cfg.Dashboard.HTTPPort,
		"websocket_path", cfg.Dashboard.WebsocketPath,
		"cors_origin", cfg.Dashboard.CORSOrigin,
		"forecaster_grpc", cfg.Forecaster.GrpcAddress,
		"collector_interval_sec", cfg.Collector.IntervalSec,
		"predictor_interval_sec", cfg.Predictor.IntervalSec,
		"predictor_enabled", pred != nil,
		"kubernetes_client_enabled", kubeClient != nil,
		"config_path", configPath,
	}
	if cfg.Discovery.Mode == config.DiscoveryModeStatic {
		startupFields = append(startupFields, "static_services", len(cfg.Discovery.StaticServices))
	}
	slog.Info("OptiPilot Controller starting", startupFields...)

	coll := collector.NewCollector(disc, st, collector.CollectorConfig{
		IntervalSec:     cfg.Collector.IntervalSec,
		PrometheusAddr:  cfg.Prometheus.Address,
		QueryTimeoutSec: cfg.Prometheus.QueryTimeoutSec,
	}, slog.Default())
	coll.SetMetricsHook(func(m models.ServiceMetrics) {
		events.Publish(dashboard.NewEvent(dashboard.EventTypeMetricsUpdate, dashboard.MetricsUpdateData{
			Service:   m.ServiceName,
			RPS:       m.RPS,
			CPU:       m.CPUPercent,
			Memory:    m.MemoryMB,
			Latency:   m.AvgLatencyMs,
			Timestamp: m.CollectedAt.UTC(),
		}))
	})

	dashServer, err := dashboard.NewServer(
		cfg.Dashboard,
		cfg.Prometheus.Address,
		st,
		disc,
		func() dashboard.PredictionReader {
			if pred == nil {
				return nil
			}
			return pred
		}(),
		act,
		fcClient,
		kubeClient,
		events,
		slog.Default(),
	)
	if err != nil {
		return err
	}

	// rootCtx is cancelled on signal so background goroutines wind down.
	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go coll.Run(rootCtx)
	if pred != nil {
		go pred.Run(rootCtx)
	}
	go act.RunWatcher(rootCtx, disc)
	go runPurgeLoop(rootCtx, st, cfg.Storage.RetentionHours)
	go func() {
		if err := dashServer.Run(rootCtx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("dashboard server failed", "error", err)
			cancel()
		}
	}()

	<-rootCtx.Done()
	slog.Info("shutting down", "reason", "signal received")
	events.Close()

	// Give in-flight operations a moment to bail out via context cancellation.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	<-shutdownCtx.Done()

	return nil
}

func newDiscovery(cfg *config.Config, kubeClient kube.Client) (discovery.ServiceDiscovery, error) {
	switch cfg.Discovery.Mode {
	case config.DiscoveryModeStatic:
		return discovery.NewStaticDiscovery(cfg.Discovery.StaticServices, cfg.Scaling.Mode), nil
	case config.DiscoveryModeKubernetes:
		return discovery.NewKubernetesDiscovery(
			cfg.Discovery.Kubernetes,
			cfg.Scaling.Mode,
			cfg.Scaling.DefaultMinReplicas,
			cfg.Scaling.DefaultMaxReplicas,
			kubeClient,
			slog.Default(),
		), nil
	default:
		return nil, fmt.Errorf("unsupported discovery mode: %q", cfg.Discovery.Mode)
	}
}

// runPurgeLoop deletes metrics older than retentionHours every purgeInterval.
// Cancels promptly when ctx is done.
func runPurgeLoop(ctx context.Context, st store.Store, retentionHours int) {
	ticker := time.NewTicker(purgeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := st.PurgeOldMetrics(ctx, retentionHours); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("purge old metrics failed", "error", err, "retention_hours", retentionHours)
				continue
			}
			slog.Debug("purged old metrics", "retention_hours", retentionHours)
		}
	}
}
