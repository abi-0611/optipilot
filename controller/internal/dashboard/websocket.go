package dashboard

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type wsHub struct {
	upgrader websocket.Upgrader
	logger   *slog.Logger

	mu    sync.Mutex
	conns map[*websocket.Conn]struct{}
}

func newWSHub(events *EventBus, corsOrigin string, logger *slog.Logger) *wsHub {
	if logger == nil {
		logger = slog.Default()
	}
	origin := strings.TrimSpace(corsOrigin)
	return &wsHub{
		logger: logger.With("component", "dashboard_websocket"),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				headerOrigin := strings.TrimSpace(r.Header.Get("Origin"))
				if headerOrigin == "" || origin == "" || origin == "*" {
					return true
				}
				return strings.EqualFold(headerOrigin, origin)
			},
		},
		conns: make(map[*websocket.Conn]struct{}),
	}
}

func (h *wsHub) register(conn *websocket.Conn) {
	h.mu.Lock()
	h.conns[conn] = struct{}{}
	h.mu.Unlock()
}

func (h *wsHub) unregister(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.conns, conn)
	h.mu.Unlock()
}

func (h *wsHub) connectionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

func (h *wsHub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.conns {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "controller shutdown"),
			time.Now().Add(2*time.Second),
		)
		_ = conn.Close()
		delete(h.conns, conn)
	}
}

func (s *Server) registerWebsocketRoute(mux *http.ServeMux) {
	mux.HandleFunc("GET "+s.cfg.WebsocketPath, s.handleWebsocket)
}

func (s *Server) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.wsHub.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("websocket upgrade failed", "error", err)
		return
	}
	s.wsHub.register(conn)
	defer func() {
		s.wsHub.unregister(conn)
		_ = conn.Close()
	}()

	eventTypes := parseEventTypes(r.URL.Query().Get("types"))
	subCh, unsubscribe := s.events.Subscribe(eventTypes, 64)
	defer unsubscribe()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-readDone:
			return
		case evt, ok := <-subCh:
			if !ok {
				s.logger.Warn("websocket client dropped due to slow consumption")
				return
			}
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(evt); err != nil {
				return
			}
		}
	}
}

func parseEventTypes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
