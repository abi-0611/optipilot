package main

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	maxConnections = 1000
)

type Metrics struct {
	serviceName string

	activeConns int64

	handler         http.Handler
	activeGauge     prometheus.Gauge
	requestsTotal   *prometheus.CounterVec
	requestDuration prometheus.Histogram
}

func NewMetrics(name string) *Metrics {
	constLabels := prometheus.Labels{"service": name}
	registry := prometheus.NewRegistry()

	activeGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "http_active_connections",
		Help:        "Current number of active HTTP connections.",
		ConstLabels: constLabels,
	})

	requestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "http_requests_total",
		Help:        "Total number of HTTP requests.",
		ConstLabels: constLabels,
	}, []string{"code"})

	requestDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:        "http_request_duration_seconds",
		Help:        "HTTP request duration in seconds.",
		ConstLabels: constLabels,
		Buckets:     prometheus.DefBuckets,
	})

	registry.MustRegister(activeGauge, requestsTotal, requestDuration)

	return &Metrics{
		serviceName:     name,
		handler:         promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		activeGauge:     activeGauge,
		requestsTotal:   requestsTotal,
		requestDuration: requestDuration,
	}
}

func (m *Metrics) Active() int64 {
	return atomic.LoadInt64(&m.activeConns)
}

func (m *Metrics) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	m.handler.ServeHTTP(w, r)
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
		active := atomic.AddInt64(&m.activeConns, 1)
		m.activeGauge.Set(float64(active))
		defer func() {
			active := atomic.AddInt64(&m.activeConns, -1)
			m.activeGauge.Set(float64(active))
		}()

		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		h(sr, r)

		m.requestsTotal.WithLabelValues(strconv.Itoa(sr.status)).Inc()
		m.requestDuration.Observe(time.Since(start).Seconds())
	}
}
