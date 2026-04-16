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

	"github.com/optipilot/controller/internal/collector"
	"github.com/optipilot/controller/internal/config"
	"github.com/optipilot/controller/internal/discovery"
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

	// Initialize service discovery based on configured mode.
	disc, err := newDiscovery(cfg)
	if err != nil {
		return err
	}
	defer disc.Stop()

	startupFields := []any{
		"discovery_mode", cfg.Discovery.Mode,
		"scaling_mode", cfg.Scaling.Mode,
		"prometheus_address", cfg.Prometheus.Address,
		"db_path", cfg.Storage.DBPath,
		"http_port", cfg.Server.HTTPPort,
		"forecaster_grpc", cfg.Forecaster.GrpcAddress,
		"collector_interval_sec", cfg.Collector.IntervalSec,
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

	// rootCtx is cancelled on signal so background goroutines wind down.
	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go coll.Run(rootCtx)
	go runPurgeLoop(rootCtx, st, cfg.Storage.RetentionHours)

	<-rootCtx.Done()
	slog.Info("shutting down", "reason", "signal received")

	// Give in-flight operations a moment to bail out via context cancellation.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	<-shutdownCtx.Done()

	return nil
}

func newDiscovery(cfg *config.Config) (discovery.ServiceDiscovery, error) {
	switch cfg.Discovery.Mode {
	case config.DiscoveryModeStatic:
		return discovery.NewStaticDiscovery(cfg.Discovery.StaticServices, cfg.Scaling.Mode), nil
	case config.DiscoveryModeKubernetes:
		return discovery.NewKubernetesDiscovery(cfg.Discovery.Kubernetes), nil
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
