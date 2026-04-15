package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const serviceName = "payment-service"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}

	m := NewMetrics(serviceName)
	h := newHandlers(m)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/payments/process", m.Wrap(h.process))
	mux.HandleFunc("GET /api/v1/payments/{id}/status", m.Wrap(h.status))
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /ready", h.ready)
	mux.HandleFunc("GET /metrics", m.MetricsHandler)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("OptiPilot | %s | listening on :%s", serviceName, port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Printf("OptiPilot | %s | shutting down", serviceName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
