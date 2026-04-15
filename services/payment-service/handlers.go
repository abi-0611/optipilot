package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

type Payment struct {
	ID            string    `json:"id"`
	TransactionID string    `json:"transaction_id"`
	OrderID       string    `json:"order_id"`
	Amount        float64   `json:"amount"`
	Currency      string    `json:"currency"`
	Method        string    `json:"method"`
	Status        string    `json:"status"`
	ProcessedAt   time.Time `json:"processed_at"`
}

type handlers struct {
	m        *Metrics
	mu       sync.Mutex
	payments map[string]Payment
}

func newHandlers(m *Metrics) *handlers {
	return &handlers{m: m, payments: make(map[string]Payment)}
}

func randID() string {
	a, b := rand.Uint64(), rand.Uint64()
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(a), uint16(a>>32), uint16(a>>48), uint16(b), b>>16)
}

func txnID() string {
	return fmt.Sprintf("txn_%016x", rand.Uint64())
}

func simLatency(active int64) time.Duration {
	var ms int
	switch {
	case active > 300:
		ms = 500 + rand.IntN(1001)
	case active > 100:
		ms = 100 + rand.IntN(301)
	default:
		ms = 20 + rand.IntN(41)
	}
	return time.Duration(ms) * time.Millisecond
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *handlers) process(w http.ResponseWriter, r *http.Request) {
	if rand.Float64() < 0.05 {
		time.Sleep(2 * time.Second)
		writeJSON(w, 504, map[string]string{
			"error":   "payment gateway timeout",
			"gateway": "acme-payments",
		})
		return
	}

	time.Sleep(simLatency(h.m.Active()))

	var body struct {
		OrderID  string  `json:"order_id"`
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
		Method   string  `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	if body.Currency == "" {
		body.Currency = "USD"
	}
	if body.Method == "" {
		body.Method = "card"
	}
	if body.Amount <= 0 {
		body.Amount = 99.99
	}

	status := "approved"
	if rand.Float64() < 0.03 {
		status = "declined"
	}

	p := Payment{
		ID:            randID(),
		TransactionID: txnID(),
		OrderID:       body.OrderID,
		Amount:        body.Amount,
		Currency:      body.Currency,
		Method:        body.Method,
		Status:        status,
		ProcessedAt:   time.Now().UTC(),
	}
	h.mu.Lock()
	h.payments[p.ID] = p
	h.mu.Unlock()

	code := 201
	if status == "declined" {
		code = 402
	}
	writeJSON(w, code, p)
}

func (h *handlers) status(w http.ResponseWriter, r *http.Request) {
	time.Sleep(simLatency(h.m.Active()))
	id := r.PathValue("id")
	h.mu.Lock()
	p, ok := h.payments[id]
	h.mu.Unlock()
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "payment not found", "id": id})
		return
	}
	writeJSON(w, 200, map[string]any{
		"id":             p.ID,
		"transaction_id": p.TransactionID,
		"status":         p.Status,
		"processed_at":   p.ProcessedAt,
	})
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
