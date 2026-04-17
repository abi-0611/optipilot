// Package collector queries Prometheus for runtime metrics of each discovered
// service and persists them to the store. It runs as a long-lived goroutine
// on a configurable tick interval and maintains an in-memory cache of the
// latest observation per service for zero-latency reads by other subsystems.
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/optipilot/controller/internal/discovery"
	"github.com/optipilot/controller/internal/models"
	"github.com/optipilot/controller/internal/store"
)

// CollectorConfig holds the knobs extracted from the top-level config so the
// collector package doesn't depend on the config package directly.
type CollectorConfig struct {
	IntervalSec     int
	PrometheusAddr  string
	QueryTimeoutSec int
}

// promClient abstracts Prometheus so the collector can be tested without a
// real Prometheus server.
type promClient interface {
	Query(ctx context.Context, query string, ts time.Time) (float64, error)
}

// Collector ties discovery, Prometheus, and the store together into a
// single collection loop.
type Collector struct {
	discovery discovery.ServiceDiscovery
	store     store.Store
	prom      promClient
	cfg       CollectorConfig

	cache         map[string]*models.ServiceMetrics
	cacheMu       sync.RWMutex
	metricsUpdate func(models.ServiceMetrics)

	logger *slog.Logger
}

func NewCollector(
	disc discovery.ServiceDiscovery,
	st store.Store,
	cfg CollectorConfig,
	logger *slog.Logger,
) *Collector {
	return &Collector{
		discovery: disc,
		store:     st,
		prom:      newPrometheusHTTPClient(cfg.PrometheusAddr, cfg.QueryTimeoutSec),
		cfg:       cfg,
		cache:     make(map[string]*models.ServiceMetrics),
		logger:    logger,
	}
}

// Run collects immediately on start and then every IntervalSec until ctx is
// cancelled. Intended to be called as a goroutine: go c.Run(ctx).
func (c *Collector) Run(ctx context.Context) {
	interval := time.Duration(c.cfg.IntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.collectAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectAll(ctx)
		}
	}
}

// ---------------------------------------------------------------------------
// Cache accessors — read by other subsystems (gRPC server, HTTP dashboard).
// ---------------------------------------------------------------------------

func (c *Collector) GetCachedMetrics(service string) *models.ServiceMetrics {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	m, ok := c.cache[service]
	if !ok {
		return nil
	}
	cp := *m
	return &cp
}

func (c *Collector) GetAllCachedMetrics() map[string]*models.ServiceMetrics {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	out := make(map[string]*models.ServiceMetrics, len(c.cache))
	for k, v := range c.cache {
		cp := *v
		out[k] = &cp
	}
	return out
}

func (c *Collector) GetMonitoredServices() []string {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	out := make([]string, 0, len(c.cache))
	for k := range c.cache {
		out = append(out, k)
	}
	return out
}

// SetMetricsHook sets an optional callback invoked after each successful
// metrics row is persisted and cached.
func (c *Collector) SetMetricsHook(hook func(models.ServiceMetrics)) {
	c.metricsUpdate = hook
}

// ---------------------------------------------------------------------------
// Core collection logic
// ---------------------------------------------------------------------------

// collectResult carries a successful observation or an error back from a
// per-service goroutine.
type collectResult struct {
	metrics models.ServiceMetrics
	err     error
}

func (c *Collector) collectAll(ctx context.Context) {
	start := time.Now()

	services, err := c.discovery.Discover(ctx)
	if err != nil {
		c.logger.Error("discovery failed during collection", "error", err)
		return
	}
	if len(services) == 0 {
		c.logger.Warn("no services discovered, skipping collection")
		return
	}

	results := make(chan collectResult, len(services))
	var wg sync.WaitGroup

	for _, svc := range services {
		wg.Add(1)
		go func(target models.ServiceTarget) {
			defer wg.Done()
			m, err := c.collectService(ctx, target)
			results <- collectResult{metrics: m, err: err}
		}(svc)
	}

	// Close the channel once every goroutine finishes.
	go func() {
		wg.Wait()
		close(results)
	}()

	var (
		batch     []models.ServiceMetrics
		succeeded int
		failed    int
	)
	for r := range results {
		if r.err != nil {
			failed++
			continue
		}
		batch = append(batch, r.metrics)
		succeeded++
	}

	// Persist the batch and refresh the cache.
	if len(batch) > 0 {
		if err := c.store.SaveMetricsBatch(ctx, batch); err != nil {
			c.logger.Error("save metrics batch failed", "error", err)
		}

		c.cacheMu.Lock()
		for i := range batch {
			m := batch[i]
			c.cache[m.ServiceName] = &m
		}
		c.cacheMu.Unlock()

		if c.metricsUpdate != nil {
			for i := range batch {
				c.metricsUpdate(batch[i])
			}
		}
	}

	elapsed := time.Since(start)
	c.logger.Info("metrics collected",
		"services", len(services),
		"success", succeeded,
		"failed", failed,
		"duration", elapsed.Round(time.Millisecond),
	)
	for i := range batch {
		m := &batch[i]
		c.logger.Info("  "+m.ServiceName,
			"rps", fmt.Sprintf("%.1f", m.RPS),
			"cpu", fmt.Sprintf("%.1f%%", m.CPUPercent),
			"mem", fmt.Sprintf("%.0fMB", m.MemoryMB),
			"p99", fmt.Sprintf("%.0fms", m.P99LatencyMs),
		)
	}
}

// ---------------------------------------------------------------------------
// Per-service PromQL queries
// ---------------------------------------------------------------------------

// TODO: These PromQL templates assume standard metric names (http_requests_total,
// container_cpu_usage_seconds_total, etc.). In a future iteration, make the
// query templates configurable per-service or globally so users can adapt to
// their actual Prometheus metric naming conventions.

type metricQuery struct {
	name  string
	tmpl  string // %s is replaced with the service name
	apply func(m *models.ServiceMetrics, val float64)
}

var defaultQueries = []metricQuery{
	{
		name:  "rps",
		tmpl:  `rate(http_requests_total{service="%s"}[1m])`,
		apply: func(m *models.ServiceMetrics, v float64) { m.RPS = v },
	},
	{
		name:  "avg_latency_ms",
		tmpl:  `rate(http_request_duration_seconds_sum{service="%s"}[1m]) / rate(http_request_duration_seconds_count{service="%s"}[1m]) * 1000`,
		apply: func(m *models.ServiceMetrics, v float64) { m.AvgLatencyMs = v },
	},
	{
		name:  "p95_latency_ms",
		tmpl:  `histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{service="%s"}[1m])) * 1000`,
		apply: func(m *models.ServiceMetrics, v float64) { m.P95LatencyMs = v },
	},
	{
		name:  "p99_latency_ms",
		tmpl:  `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket{service="%s"}[1m])) * 1000`,
		apply: func(m *models.ServiceMetrics, v float64) { m.P99LatencyMs = v },
	},
	{
		name:  "cpu_percent",
		tmpl:  `rate(container_cpu_usage_seconds_total{pod=~"%s.*"}[1m]) * 100`,
		apply: func(m *models.ServiceMetrics, v float64) { m.CPUPercent = v },
	},
	{
		name:  "memory_mb",
		tmpl:  `container_memory_working_set_bytes{pod=~"%s.*"} / 1024 / 1024`,
		apply: func(m *models.ServiceMetrics, v float64) { m.MemoryMB = v },
	},
	{
		name:  "error_rate",
		tmpl:  `rate(http_requests_total{service="%s",code=~"5.."}[1m]) / rate(http_requests_total{service="%s"}[1m])`,
		apply: func(m *models.ServiceMetrics, v float64) { m.ErrorRate = v },
	},
}

func buildQuery(tmpl, serviceName string) string {
	// Count how many %s placeholders exist in the template.
	n := 0
	for i := 0; i < len(tmpl)-1; i++ {
		if tmpl[i] == '%' && tmpl[i+1] == 's' {
			n++
		}
	}
	args := make([]any, n)
	for i := range args {
		args[i] = serviceName
	}
	return fmt.Sprintf(tmpl, args...)
}

// collectService runs all PromQL queries for a single service. Individual
// query failures are logged as warnings and the metric is left at 0 — we
// never fail the whole service because one metric is missing.
func (c *Collector) collectService(ctx context.Context, target models.ServiceTarget) (models.ServiceMetrics, error) {
	now := time.Now()
	m := models.ServiceMetrics{
		ServiceName: target.Name,
		CollectedAt: now,
	}

	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.QueryTimeoutSec)*time.Second)
	defer cancel()

	var queryErrors int
	for _, q := range defaultQueries {
		promql := buildQuery(q.tmpl, target.Name)
		val, err := c.prom.Query(queryCtx, promql, now)
		if err != nil {
			c.logger.Warn("prometheus query failed",
				"service", target.Name,
				"metric", q.name,
				"error", err,
			)
			queryErrors++
			continue
		}
		if math.IsNaN(val) || math.IsInf(val, 0) {
			val = 0
		}
		q.apply(&m, val)
	}

	if queryErrors == len(defaultQueries) {
		return m, fmt.Errorf("all %d queries failed for service %q", queryErrors, target.Name)
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Prometheus HTTP client
// ---------------------------------------------------------------------------

type prometheusHTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

func newPrometheusHTTPClient(addr string, timeoutSec int) *prometheusHTTPClient {
	return &prometheusHTTPClient{
		baseURL: addr,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
	}
}

// promResponse is the minimal structure of a Prometheus /api/v1/query response
// needed to extract a single scalar from an instant vector.
type promResponse struct {
	Status string   `json:"status"`
	Error  string   `json:"error"`
	Data   promData `json:"data"`
}

type promData struct {
	ResultType string       `json:"resultType"`
	Result     []promResult `json:"result"`
}

type promResult struct {
	Value [2]json.RawMessage `json:"value"`
}

func (p *prometheusHTTPClient) Query(ctx context.Context, query string, ts time.Time) (float64, error) {
	u := fmt.Sprintf("%s/api/v1/query", p.baseURL)
	params := url.Values{
		"query": {query},
		"time":  {fmt.Sprintf("%d", ts.Unix())},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("prometheus request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus returned HTTP %d: %s", resp.StatusCode, truncate(body, 200))
	}

	var pr promResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	if pr.Status != "success" {
		return 0, fmt.Errorf("prometheus error: %s", pr.Error)
	}

	if len(pr.Data.Result) == 0 {
		return 0, fmt.Errorf("empty result for query")
	}

	// value[1] is a JSON string containing the float, e.g. "1.234".
	var valStr string
	if err := json.Unmarshal(pr.Data.Result[0].Value[1], &valStr); err != nil {
		return 0, fmt.Errorf("decode value: %w", err)
	}

	var val float64
	if _, err := fmt.Sscanf(valStr, "%f", &val); err != nil {
		return 0, fmt.Errorf("parse value %q: %w", valStr, err)
	}

	return val, nil
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
