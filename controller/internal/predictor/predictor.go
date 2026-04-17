// Package predictor runs the prediction loop: every interval it forwards
// recent metrics to the forecaster and fetches scaling recommendations for
// each discovered service. Recommendations are logged, cached, and then handed
// off to the actuator when one is wired.
package predictor

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/optipilot/controller/internal/discovery"
	"github.com/optipilot/controller/internal/forecaster"
	"github.com/optipilot/controller/internal/models"
	"github.com/optipilot/controller/internal/store"
)

// PredictorConfig holds tunables extracted from the top-level config.
type PredictorConfig struct {
	IntervalSec     int // how often to tick (default 60)
	LookbackMinutes int // how far back to pull from store (default 30)
	MinDataPoints   int // skip service if fewer metrics in window (default 10)
	IngestBatchSize int // max metrics per IngestMetrics call (default 500)
}

// Predictor owns the forward-metrics + predict loop.
type Predictor struct {
	discovery discovery.ServiceDiscovery
	store     store.Store
	client    forecaster.Client
	cfg       PredictorConfig
	handler   RecommendationHandler

	// cache is the latest PredictionResponse per service, accessible to
	// external callers (e.g. a future HTTP API) without blocking the loop.
	cache   map[string]*forecaster.PredictionResponse
	history map[string][]forecaster.PredictionResponse
	cacheMu sync.RWMutex

	historyLimit     int
	predictionUpdate func(models.ServiceTarget, *forecaster.PredictionResponse)

	logger *slog.Logger
}

// RecommendationHandler consumes one prediction and decides whether to apply it.
// Actuator implements this in autonomous/recommend/shadow flows.
type RecommendationHandler interface {
	HandlePrediction(ctx context.Context, target models.ServiceTarget, pred *forecaster.PredictionResponse) (action string, reason string, err error)
}

// New constructs a Predictor. cfg values of 0 are replaced with sensible defaults.
func New(
	disc discovery.ServiceDiscovery,
	st store.Store,
	client forecaster.Client,
	cfg PredictorConfig,
	logger *slog.Logger,
) *Predictor {
	if cfg.IntervalSec <= 0 {
		cfg.IntervalSec = 60
	}
	if cfg.LookbackMinutes <= 0 {
		cfg.LookbackMinutes = 30
	}
	if cfg.MinDataPoints <= 0 {
		cfg.MinDataPoints = 10
	}
	if cfg.IngestBatchSize <= 0 {
		cfg.IngestBatchSize = 500
	}
	return &Predictor{
		discovery: disc,
		store:     st,
		client:    client,
		cfg:       cfg,
		cache:     make(map[string]*forecaster.PredictionResponse),
		history:   make(map[string][]forecaster.PredictionResponse),
		// Keeps enough data for dashboard inspection while staying bounded.
		historyLimit: 1000,
		logger:       logger,
	}
}

// SetRecommendationHandler wires an optional action handler (actuator).
func (p *Predictor) SetRecommendationHandler(handler RecommendationHandler) {
	p.handler = handler
}

// SetPredictionHook sets an optional callback invoked after each successful
// prediction retrieval.
func (p *Predictor) SetPredictionHook(hook func(models.ServiceTarget, *forecaster.PredictionResponse)) {
	p.predictionUpdate = hook
}

// Run blocks until ctx is cancelled, ticking every IntervalSec.
// It does one immediate metrics-forward pass on startup so the forecaster
// begins receiving data right away, then waits for the first full interval
// before requesting predictions.
func (p *Predictor) Run(ctx context.Context) {
	p.logger.Info("predictor starting",
		"interval_sec", p.cfg.IntervalSec,
		"lookback_min", p.cfg.LookbackMinutes,
	)

	// Forward any existing metrics immediately.
	p.forwardMetrics(ctx)

	ticker := time.NewTicker(time.Duration(p.cfg.IntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("predictor stopping")
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

// GetLatestPrediction returns the most recent prediction for a service, or
// nil if none has been produced yet.
func (p *Predictor) GetLatestPrediction(service string) *forecaster.PredictionResponse {
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()
	return p.cache[service]
}

// GetAllPredictions returns a shallow copy of the prediction cache.
func (p *Predictor) GetAllPredictions() map[string]*forecaster.PredictionResponse {
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()
	out := make(map[string]*forecaster.PredictionResponse, len(p.cache))
	for k, v := range p.cache {
		out[k] = v
	}
	return out
}

// GetPredictionHistory returns up to `limit` most recent predictions (newest
// first) for one service.
func (p *Predictor) GetPredictionHistory(service string, limit int) []forecaster.PredictionResponse {
	if limit <= 0 {
		limit = 100
	}
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()
	h := p.history[service]
	if len(h) == 0 {
		return nil
	}
	if limit > len(h) {
		limit = len(h)
	}
	out := make([]forecaster.PredictionResponse, 0, limit)
	for i := len(h) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, h[i])
	}
	return out
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (p *Predictor) tick(ctx context.Context) {
	start := time.Now()
	p.forwardMetrics(ctx)
	p.predictAll(ctx)
	p.logger.Info("predictor tick complete",
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

// forwardMetrics collects recent metrics for all services and sends them to
// the forecaster. This is fire-and-forget: errors are logged, not fatal.
func (p *Predictor) forwardMetrics(ctx context.Context) {
	services, err := p.discovery.Discover(ctx)
	if err != nil {
		p.logger.Warn("predictor: discover services failed", "error", err)
		return
	}

	var batch []models.ServiceMetrics
	for _, svc := range services {
		rows, err := p.store.GetRecentMetrics(ctx, svc.Name, p.cfg.LookbackMinutes)
		if err != nil {
			p.logger.Warn("predictor: fetch metrics failed",
				"service", svc.Name, "error", err)
			continue
		}
		batch = append(batch, rows...)
	}

	if len(batch) == 0 {
		p.logger.Info("predictor: no metrics to forward")
		return
	}

	// Send in chunks so we don't exceed message size limits.
	total := 0
	for i := 0; i < len(batch); i += p.cfg.IngestBatchSize {
		end := i + p.cfg.IngestBatchSize
		if end > len(batch) {
			end = len(batch)
		}
		accepted, err := p.client.IngestMetrics(ctx, batch[i:end])
		if err != nil {
			p.logger.Warn("predictor: IngestMetrics failed",
				"error", err, "chunk_size", end-i)
			continue
		}
		total += int(accepted)
	}
	p.logger.Info("predictor: metrics forwarded",
		"count", len(batch), "accepted", total)
}

// predictAll fires one GetPrediction goroutine per service and waits for all
// to finish, then logs a summary.
func (p *Predictor) predictAll(ctx context.Context) {
	services, err := p.discovery.Discover(ctx)
	if err != nil {
		p.logger.Warn("predictor: discover services failed", "error", err)
		return
	}

	var (
		wg      sync.WaitGroup
		success atomic.Int32
		skipped atomic.Int32
		failed  atomic.Int32
	)

	for _, svc := range services {
		svc := svc // capture
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch p.predictOne(ctx, svc) {
			case predictResultSuccess:
				success.Add(1)
			case predictResultSkipped:
				skipped.Add(1)
			case predictResultFailed:
				failed.Add(1)
			}
		}()
	}

	wg.Wait()
	p.logger.Info("predictor: predictions complete",
		"total", len(services),
		"success", success.Load(),
		"skipped", skipped.Load(),
		"failed", failed.Load(),
	)
}

type predictResult int

const (
	predictResultSuccess predictResult = iota
	predictResultSkipped
	predictResultFailed
)

func (p *Predictor) predictOne(ctx context.Context, svc models.ServiceTarget) predictResult {
	rows, err := p.store.GetRecentMetrics(ctx, svc.Name, p.cfg.LookbackMinutes)
	if err != nil {
		p.logger.Warn("predictor: fetch metrics failed",
			"service", svc.Name, "error", err)
		return predictResultFailed
	}

	if len(rows) < p.cfg.MinDataPoints {
		p.logger.Info("predictor: skipping service — insufficient data",
			"service", svc.Name,
			"have", len(rows),
			"need", p.cfg.MinDataPoints,
		)
		return predictResultSkipped
	}

	// Extract RPS values in chronological order for the request.
	recentRPS := make([]float64, len(rows))
	for i, r := range rows {
		recentRPS[i] = r.RPS
	}

	resp, err := p.client.GetPrediction(ctx, &forecaster.PredictionRequest{
		ServiceName: svc.Name,
		RecentRPS:   recentRPS,
		Timestamp:   time.Now(),
	})
	if err != nil {
		p.logger.Warn("predictor: GetPrediction failed",
			"service", svc.Name, "error", err)
		return predictResultFailed
	}

	// Log the recommendation prominently — this is the key signal operators watch.
	p.logger.Info("RECOMMENDATION",
		"service", resp.ServiceName,
		"rps_p50", resp.RpsP50,
		"rps_p90", resp.RpsP90,
		"recommended_replicas", resp.RecommendedReplicas,
		"mode", resp.ScalingMode,
		"confidence", resp.ConfidenceScore,
		"model_version", resp.ModelVersion,
		"reason", resp.Reason,
	)

	// Update in-memory cache.
	p.cacheMu.Lock()
	p.cache[svc.Name] = resp
	p.history[svc.Name] = append(p.history[svc.Name], *resp)
	if len(p.history[svc.Name]) > p.historyLimit {
		p.history[svc.Name] = p.history[svc.Name][len(p.history[svc.Name])-p.historyLimit:]
	}
	p.cacheMu.Unlock()

	if p.predictionUpdate != nil {
		p.predictionUpdate(svc, resp)
	}

	if p.handler != nil {
		action, reason, err := p.handler.HandlePrediction(ctx, svc, resp)
		if err != nil {
			p.logger.Warn("predictor: actuator handling failed",
				"service", svc.Name,
				"error", err,
			)
			return predictResultFailed
		}
		p.logger.Info("predictor: recommendation handled",
			"service", svc.Name,
			"action", action,
			"reason", reason,
		)
		return predictResultSuccess
	}

	// Persist as an audit record when no actuator is configured.
	decision := &models.ScalingDecision{
		ServiceName:     resp.ServiceName,
		OldReplicas:     0, // unknown until actuator queries k8s
		NewReplicas:     resp.RecommendedReplicas,
		ScalingMode:     resp.ScalingMode,
		ModelVersion:    resp.ModelVersion,
		Reason:          resp.Reason,
		RpsP50:          resp.RpsP50,
		RpsP90:          resp.RpsP90,
		ConfidenceScore: resp.ConfidenceScore,
		Executed:        false,
		CreatedAt:       time.Now(),
	}
	if err := p.store.SaveScalingDecision(ctx, decision); err != nil {
		p.logger.Warn("predictor: save scaling decision failed",
			"service", svc.Name, "error", err)
	}

	return predictResultSuccess
}
