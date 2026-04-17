package dashboard

import (
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	EventTypeMetricsUpdate   = "metrics_update"
	EventTypePrediction      = "prediction"
	EventTypeScalingDecision = "scaling_decision"
	EventTypeModeChange      = "mode_change"
	EventTypeAlert           = "alert"
	EventTypeModelUpdate     = "model_update"
)

type Event struct {
	Type      string    `json:"type"`
	Data      any       `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

type MetricsUpdateData struct {
	Service   string    `json:"service"`
	RPS       float64   `json:"rps"`
	CPU       float64   `json:"cpu"`
	Memory    float64   `json:"memory"`
	Latency   float64   `json:"latency"`
	Timestamp time.Time `json:"timestamp"`
}

type PredictionData struct {
	Service      string  `json:"service"`
	P50          float64 `json:"p50"`
	P90          float64 `json:"p90"`
	Replicas     int32   `json:"replicas"`
	Mode         string  `json:"mode"`
	Confidence   float64 `json:"confidence"`
	ModelVersion string  `json:"model_version"`
}

type ScalingDecisionData struct {
	Service     string    `json:"service"`
	OldReplicas int32     `json:"old_replicas"`
	NewReplicas int32     `json:"new_replicas"`
	Reason      string    `json:"reason"`
	Executed    bool      `json:"executed"`
	Timestamp   time.Time `json:"timestamp"`
}

type ModeChangeData struct {
	Service     string `json:"service"`
	OldMode     string `json:"old_mode"`
	NewMode     string `json:"new_mode"`
	TriggeredBy string `json:"triggered_by"`
}

type AlertData struct {
	Service   string    `json:"service"`
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type ModelUpdateData struct {
	Service    string  `json:"service"`
	NewVersion string  `json:"new_version"`
	MAPE       float64 `json:"mape"`
	Promoted   bool    `json:"promoted"`
}

type EventBus struct {
	logger *slog.Logger

	mu     sync.RWMutex
	nextID uint64
	closed bool
	subs   map[uint64]*subscriber
}

type subscriber struct {
	types map[string]struct{}
	ch    chan Event
}

func NewEventBus(logger *slog.Logger) *EventBus {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventBus{
		logger: logger.With("component", "dashboard_event_bus"),
		subs:   make(map[uint64]*subscriber),
	}
}

func NewEvent(eventType string, data any) Event {
	return Event{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}
}

func (b *EventBus) Subscribe(eventTypes []string, buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	typesSet := make(map[string]struct{}, len(eventTypes))
	for _, t := range eventTypes {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		typesSet[t] = struct{}{}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, buffer)
	if b.closed {
		close(ch)
		return ch, func() {}
	}
	b.nextID++
	id := b.nextID
	b.subs[id] = &subscriber{
		types: typesSet,
		ch:    ch,
	}
	return ch, func() {
		b.removeSubscriber(id)
	}
}

func (b *EventBus) Publish(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}
	type target struct {
		id  uint64
		sub *subscriber
	}
	targets := make([]target, 0, len(b.subs))
	for id, sub := range b.subs {
		targets = append(targets, target{id: id, sub: sub})
	}
	b.mu.RUnlock()

	for _, t := range targets {
		if len(t.sub.types) > 0 {
			if _, ok := t.sub.types[evt.Type]; !ok {
				continue
			}
		}
		select {
		case t.sub.ch <- evt:
		default:
			b.logger.Warn("dropping slow event subscriber", "event_type", evt.Type, "subscriber_id", t.id)
			b.removeSubscriber(t.id)
		}
	}
}

func (b *EventBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, sub := range b.subs {
		close(sub.ch)
		delete(b.subs, id)
	}
}

func (b *EventBus) removeSubscriber(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sub, ok := b.subs[id]
	if !ok {
		return
	}
	close(sub.ch)
	delete(b.subs, id)
}
