package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	windowSeconds  = 60
	latencyBufSize = 1000
	maxConnections = 1000
)

type bucket struct {
	second int64
	count  int64
	errors int64
}

type Metrics struct {
	serviceName string
	startedAt   time.Time

	activeConns int64
	totalReqs   int64

	mu          sync.Mutex
	buckets     [windowSeconds]bucket
	latencies   [latencyBufSize]float64
	latencyIdx  int
	latencyFill int
}

func NewMetrics(name string) *Metrics {
	return &Metrics{serviceName: name, startedAt: time.Now()}
}

func (m *Metrics) Active() int64 {
	return atomic.LoadInt64(&m.activeConns)
}

func (m *Metrics) Record(latencyMs float64, isError bool) {
	atomic.AddInt64(&m.totalReqs, 1)
	now := time.Now().Unix()
	idx := now % windowSeconds
	m.mu.Lock()
	b := &m.buckets[idx]
	if b.second != now {
		b.second = now
		b.count = 0
		b.errors = 0
	}
	b.count++
	if isError {
		b.errors++
	}
	m.latencies[m.latencyIdx] = latencyMs
	m.latencyIdx = (m.latencyIdx + 1) % latencyBufSize
	if m.latencyFill < latencyBufSize {
		m.latencyFill++
	}
	m.mu.Unlock()
}

type snapshot struct {
	ServiceName       string  `json:"service_name"`
	RPS               float64 `json:"rps"`
	AvgLatencyMs      float64 `json:"avg_latency_ms"`
	P99LatencyMs      float64 `json:"p99_latency_ms"`
	ActiveConnections int64   `json:"active_connections"`
	CPUUsagePercent   float64 `json:"cpu_usage_percent"`
	MemoryUsageMB     float64 `json:"memory_usage_mb"`
	ErrorRate         float64 `json:"error_rate"`
	TotalRequests     int64   `json:"total_requests"`
	UptimeSeconds     int64   `json:"uptime_seconds"`
}

func (m *Metrics) Snapshot() snapshot {
	now := time.Now().Unix()
	m.mu.Lock()
	var reqs, errs int64
	for i := 0; i < windowSeconds; i++ {
		b := &m.buckets[i]
		if b.second != 0 && now-b.second < windowSeconds {
			reqs += b.count
			errs += b.errors
		}
	}
	lats := make([]float64, m.latencyFill)
	copy(lats, m.latencies[:m.latencyFill])
	m.mu.Unlock()

	var avg, p99 float64
	if n := len(lats); n > 0 {
		var sum float64
		for _, v := range lats {
			sum += v
		}
		avg = sum / float64(n)
		sort.Float64s(lats)
		pi := int(float64(n) * 0.99)
		if pi >= n {
			pi = n - 1
		}
		p99 = lats[pi]
	}

	active := atomic.LoadInt64(&m.activeConns)
	total := atomic.LoadInt64(&m.totalReqs)

	cpu := 10.0 + (float64(active)/float64(maxConnections))*80.0
	if cpu > 100 {
		cpu = 100
	}
	mem := 64.0 + (float64(total)/10000.0)*10.0
	if mem > 512 {
		mem = 512
	}
	errRate := 0.0
	if reqs > 0 {
		errRate = float64(errs) / float64(reqs)
	}

	return snapshot{
		ServiceName:       m.serviceName,
		RPS:               float64(reqs) / float64(windowSeconds),
		AvgLatencyMs:      avg,
		P99LatencyMs:      p99,
		ActiveConnections: active,
		CPUUsagePercent:   cpu,
		MemoryUsageMB:     mem,
		ErrorRate:         errRate,
		TotalRequests:     total,
		UptimeSeconds:     int64(time.Since(m.startedAt).Seconds()),
	}
}

func (m *Metrics) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m.Snapshot())
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (m *Metrics) Wrap(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&m.activeConns, 1)
		defer atomic.AddInt64(&m.activeConns, -1)

		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		h(sr, r)
		latMs := float64(time.Since(start).Microseconds()) / 1000.0
		m.Record(latMs, sr.status >= 500)
	}
}
