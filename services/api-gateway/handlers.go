package main

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

type Product struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Category string  `json:"category"`
	Stock    int     `json:"stock"`
}

var products = []Product{
	{"p-001", "Wireless Noise-Cancelling Headphones", 249.99, "electronics", 42},
	{"p-002", "Ergonomic Mesh Office Chair", 389.00, "furniture", 17},
	{"p-003", "Stainless Steel French Press", 34.50, "kitchen", 128},
	{"p-004", "Merino Wool Running Socks (3-pack)", 28.75, "apparel", 256},
	{"p-005", "Smart LED Desk Lamp", 79.99, "electronics", 64},
}

type handlers struct{ m *Metrics }

func simLatency(active int64) time.Duration {
	var ms int
	switch {
	case active > 500:
		ms = 300 + rand.IntN(501)
	case active > 200:
		ms = 50 + rand.IntN(151)
	default:
		ms = 5 + rand.IntN(11)
	}
	return time.Duration(ms) * time.Millisecond
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *handlers) listProducts(w http.ResponseWriter, r *http.Request) {
	time.Sleep(simLatency(h.m.Active()))
	writeJSON(w, 200, products)
}

func (h *handlers) getProduct(w http.ResponseWriter, r *http.Request) {
	time.Sleep(simLatency(h.m.Active()))
	id := r.PathValue("id")
	for _, p := range products {
		if p.ID == id {
			writeJSON(w, 200, p)
			return
		}
	}
	writeJSON(w, 404, map[string]string{"error": "product not found", "id": id})
}

func (h *handlers) search(w http.ResponseWriter, r *http.Request) {
	time.Sleep(simLatency(h.m.Active()))
	q := strings.ToLower(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, 400, map[string]string{"error": "missing query param 'q'"})
		return
	}
	matches := make([]Product, 0)
	for _, p := range products {
		if strings.Contains(strings.ToLower(p.Name), q) || strings.Contains(strings.ToLower(p.Category), q) {
			matches = append(matches, p)
		}
	}
	writeJSON(w, 200, map[string]any{"query": q, "results": matches, "count": len(matches)})
}

func (h *handlers) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "healthy", "service": serviceName})
}

func (h *handlers) ready(w http.ResponseWriter, r *http.Request) {
	if h.m.Active() > int64(float64(maxConnections)*0.9) {
		writeJSON(w, 503, map[string]string{"status": "overloaded", "service": serviceName})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready", "service": serviceName})
}
