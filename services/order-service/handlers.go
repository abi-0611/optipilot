package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

type OrderItem struct {
	ProductID string  `json:"product_id"`
	Quantity  int     `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
}

type Order struct {
	ID         string      `json:"id"`
	CustomerID string      `json:"customer_id"`
	Items      []OrderItem `json:"items"`
	Total      float64     `json:"total"`
	Status     string      `json:"status"`
	CreatedAt  time.Time   `json:"created_at"`
}

type handlers struct {
	m      *Metrics
	mu     sync.Mutex
	orders map[string]Order
	recent []string
}

func newHandlers(m *Metrics) *handlers {
	return &handlers{m: m, orders: make(map[string]Order)}
}

func randID() string {
	a, b := rand.Uint64(), rand.Uint64()
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(a), uint16(a>>32), uint16(a>>48), uint16(b), b>>16)
}

func simLatency(active int64) time.Duration {
	var ms int
	switch {
	case active > 400:
		ms = 400 + rand.IntN(801)
	case active > 150:
		ms = 80 + rand.IntN(171)
	default:
		ms = 10 + rand.IntN(21)
	}
	if rand.Float64() < 0.02 {
		ms += 200 + rand.IntN(301)
	}
	return time.Duration(ms) * time.Millisecond
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *handlers) createOrder(w http.ResponseWriter, r *http.Request) {
	time.Sleep(simLatency(h.m.Active()))
	var body struct {
		CustomerID string      `json:"customer_id"`
		Items      []OrderItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	if body.CustomerID == "" {
		body.CustomerID = "cust-" + randID()[:8]
	}
	if len(body.Items) == 0 {
		body.Items = []OrderItem{{ProductID: "p-001", Quantity: 1, UnitPrice: 249.99}}
	}
	var total float64
	for _, it := range body.Items {
		total += it.UnitPrice * float64(it.Quantity)
	}
	order := Order{
		ID:         randID(),
		CustomerID: body.CustomerID,
		Items:      body.Items,
		Total:      total,
		Status:     "confirmed",
		CreatedAt:  time.Now().UTC(),
	}
	h.mu.Lock()
	h.orders[order.ID] = order
	h.recent = append([]string{order.ID}, h.recent...)
	if len(h.recent) > 100 {
		h.recent = h.recent[:100]
	}
	h.mu.Unlock()
	writeJSON(w, 201, order)
}

func (h *handlers) getOrder(w http.ResponseWriter, r *http.Request) {
	time.Sleep(simLatency(h.m.Active()))
	id := r.PathValue("id")
	h.mu.Lock()
	o, ok := h.orders[id]
	h.mu.Unlock()
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "order not found", "id": id})
		return
	}
	writeJSON(w, 200, o)
}

func (h *handlers) listOrders(w http.ResponseWriter, r *http.Request) {
	time.Sleep(simLatency(h.m.Active()))
	h.mu.Lock()
	out := make([]Order, 0, len(h.recent))
	for _, id := range h.recent {
		if o, ok := h.orders[id]; ok {
			out = append(out, o)
		}
	}
	h.mu.Unlock()
	writeJSON(w, 200, map[string]any{"orders": out, "count": len(out)})
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
