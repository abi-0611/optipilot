package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/optipilot/controller/internal/forecaster"
	"github.com/optipilot/controller/internal/models"
)

func (s *Server) registerRESTRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/services", s.handleGetServices)
	mux.HandleFunc("GET /api/services/{name}/metrics", s.handleGetServiceMetrics)
	mux.HandleFunc("GET /api/services/{name}/predictions", s.handleGetServicePredictions)
	mux.HandleFunc("GET /api/services/{name}/decisions", s.handleGetServiceDecisions)
	mux.HandleFunc("GET /api/services/{name}/model", s.handleGetServiceModel)
	mux.HandleFunc("POST /api/services/{name}/mode", s.handlePostServiceMode)
	mux.HandleFunc("POST /api/services/{name}/retrain", s.handlePostServiceRetrain)
	mux.HandleFunc("POST /api/services/{name}/pause", s.handlePostServicePause)
	mux.HandleFunc("POST /api/kill-switch", s.handlePostKillSwitch)
	mux.HandleFunc("GET /api/audit", s.handleGetAudit)
	mux.HandleFunc("GET /api/system/status", s.handleGetSystemStatus)
	mux.HandleFunc("POST /api/recommendations/{id}/approve", s.handlePostApproveRecommendation)
	mux.HandleFunc("POST /api/recommendations/{id}/reject", s.handlePostRejectRecommendation)
}

type serviceSummary struct {
	Name            string                     `json:"name"`
	Namespace       string                     `json:"namespace"`
	MinReplicas     int32                      `json:"min_replicas"`
	MaxReplicas     int32                      `json:"max_replicas"`
	CurrentReplicas int32                      `json:"current_replicas"`
	Mode            string                     `json:"mode"`
	Paused          bool                       `json:"paused"`
	LastMetrics     *models.ServiceMetrics     `json:"last_metrics,omitempty"`
	LastPrediction  *predictionResponsePayload `json:"last_prediction,omitempty"`
	ModelStatus     *models.ModelStatus        `json:"model_status,omitempty"`
}

type predictionResponsePayload struct {
	ServiceName         string    `json:"service_name"`
	RpsP50              float64   `json:"rps_p50"`
	RpsP90              float64   `json:"rps_p90"`
	RecommendedReplicas int32     `json:"recommended_replicas"`
	ScalingMode         string    `json:"scaling_mode"`
	ConfidenceScore     float64   `json:"confidence_score"`
	Reason              string    `json:"reason"`
	ModelVersion        string    `json:"model_version"`
	Timestamp           time.Time `json:"timestamp"`
}

func (s *Server) handleGetServices(w http.ResponseWriter, r *http.Request) {
	services, err := s.discovery.Discover(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	latestMetrics, err := s.store.GetAllLatestMetrics(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	modelStatuses, err := s.store.GetAllModelStatuses(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	resp := make([]serviceSummary, 0, len(services))
	for _, target := range services {
		summary := serviceSummary{
			Name:            target.Name,
			Namespace:       target.Namespace,
			MinReplicas:     target.MinReplicas,
			MaxReplicas:     target.MaxReplicas,
			CurrentReplicas: 0,
			Mode:            s.actuator.GetEffectiveServiceMode(target),
			Paused:          s.actuator.ServicePaused(target.Name),
			LastMetrics:     latestMetrics[target.Name],
			ModelStatus:     modelStatuses[target.Name],
		}
		if s.kube != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			replicas, err := s.currentReplicas(ctx, target)
			cancel()
			if err == nil {
				summary.CurrentReplicas = replicas
			}
		}
		if s.predictor != nil {
			if pred := s.predictor.GetLatestPrediction(target.Name); pred != nil {
				summary.LastPrediction = predictionToPayload(pred)
			}
		}
		resp = append(resp, summary)
	}

	writeJSON(w, http.StatusOK, map[string]any{"services": resp})
}

func (s *Server) handleGetServiceMetrics(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	minutes := parsePositiveInt(r.URL.Query().Get("minutes"), 60, 1, 24*60)
	metrics, err := s.store.GetRecentMetrics(r.Context(), name, minutes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"service": name, "metrics": metrics})
}

func (s *Server) handleGetServicePredictions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 100, 1, 5000)
	var predictions []predictionResponsePayload

	if s.predictor != nil {
		rows := s.predictor.GetPredictionHistory(name, limit)
		predictions = make([]predictionResponsePayload, 0, len(rows))
		for i := range rows {
			predictions = append(predictions, *predictionToPayload(&rows[i]))
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"service": name, "predictions": predictions})
}

func (s *Server) handleGetServiceDecisions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 100, 1, 5000)
	decisions, err := s.store.GetServiceDecisions(r.Context(), name, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"service": name, "decisions": decisions})
}

func (s *Server) handleGetServiceModel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	status, err := s.store.GetModelStatus(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status == nil && s.forecaster != nil {
		fresh, err := s.forecaster.GetModelStatus(r.Context(), name)
		if err == nil && fresh != nil {
			status = fresh
			_ = s.store.UpsertModelStatus(r.Context(), fresh)
		}
	}
	if status == nil {
		writeError(w, http.StatusNotFound, "model status not found")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handlePostServiceMode(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	target, err := s.lookupService(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	var req struct {
		Mode        string `json:"mode"`
		TriggeredBy string `json:"triggered_by"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Mode = strings.TrimSpace(req.Mode)
	if req.Mode == "" {
		writeError(w, http.StatusBadRequest, "mode is required")
		return
	}
	oldMode := s.actuator.GetEffectiveServiceMode(target)
	if err := s.actuator.SetServiceMode(name, req.Mode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	triggeredBy := strings.TrimSpace(req.TriggeredBy)
	if triggeredBy == "" {
		triggeredBy = "api"
	}
	s.events.Publish(NewEvent(EventTypeModeChange, ModeChangeData{
		Service:     name,
		OldMode:     oldMode,
		NewMode:     req.Mode,
		TriggeredBy: triggeredBy,
	}))
	writeJSON(w, http.StatusOK, map[string]any{
		"service": name,
		"mode":    req.Mode,
	})
}

func (s *Server) handlePostServiceRetrain(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.forecaster == nil {
		writeError(w, http.StatusServiceUnavailable, "forecaster unavailable")
		return
	}
	result, err := s.forecaster.TriggerRetrain(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if result.Success {
		s.events.Publish(NewEvent(EventTypeModelUpdate, ModelUpdateData{
			Service:    name,
			NewVersion: result.NewModelVersion,
			MAPE:       result.NewMAPE,
			Promoted:   true,
		}))
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handlePostServicePause(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req struct {
		Paused *bool `json:"paused"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	paused := true
	if req.Paused != nil {
		paused = *req.Paused
	}
	s.actuator.SetServicePaused(name, paused)
	if paused {
		s.events.Publish(NewEvent(EventTypeAlert, AlertData{
			Service:   name,
			Severity:  "warning",
			Message:   "service paused by operator",
			Timestamp: time.Now().UTC(),
		}))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": name,
		"paused":  paused,
	})
}

func (s *Server) handlePostKillSwitch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	s.actuator.SetGlobalKillSwitch(enabled)
	if enabled {
		s.events.Publish(NewEvent(EventTypeAlert, AlertData{
			Service:   "*",
			Severity:  "critical",
			Message:   "global kill switch enabled",
			Timestamp: time.Now().UTC(),
		}))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": enabled,
	})
}

func (s *Server) handleGetAudit(w http.ResponseWriter, r *http.Request) {
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 200, 1, 10000)
	decisions, err := s.store.GetRecentDecisions(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": decisions})
}

func (s *Server) handleGetSystemStatus(w http.ResponseWriter, r *http.Request) {
	metricsCount, err := s.store.GetMetricsCount(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	forecasterConnected := false
	forecasterErr := ""
	if s.forecaster != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		_, err := s.forecaster.GetAllServicesStatus(ctx)
		cancel()
		if err == nil {
			forecasterConnected = true
		} else {
			forecasterErr = err.Error()
		}
	} else {
		forecasterErr = "forecaster unavailable"
	}

	prometheusConnected := false
	prometheusErr := ""
	if s.prometheusAddr != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.prometheusAddr+"/-/healthy", nil)
		resp, err := s.httpClient.Do(req)
		if err == nil && resp != nil {
			prometheusConnected = resp.StatusCode == http.StatusOK
			_ = resp.Body.Close()
		}
		if err != nil {
			prometheusErr = err.Error()
		}
		cancel()
	} else {
		prometheusErr = "prometheus address not configured"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"controller": map[string]any{
			"healthy":            true,
			"uptime_sec":         int64(time.Since(s.startedAt).Seconds()),
			"metrics_row_count":  metricsCount,
			"global_kill_switch": s.actuator.GlobalKillSwitchEnabled(),
		},
		"forecaster": map[string]any{
			"connected": forecasterConnected,
			"error":     forecasterErr,
		},
		"prometheus": map[string]any{
			"connected": prometheusConnected,
			"error":     prometheusErr,
		},
		"websocket": map[string]any{
			"connections": s.wsHub.connectionCount(),
		},
	})
}

func (s *Server) handlePostApproveRecommendation(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	decision, err := s.actuator.ApproveRecommendation(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.events.Publish(NewEvent(EventTypeScalingDecision, ScalingDecisionData{
		Service:     decision.ServiceName,
		OldReplicas: decision.OldReplicas,
		NewReplicas: decision.NewReplicas,
		Reason:      decision.Reason,
		Executed:    decision.Executed,
		Timestamp:   time.Now().UTC(),
	}))
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) handlePostRejectRecommendation(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	decision, err := s.actuator.RejectRecommendation(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *Server) lookupService(ctx context.Context, name string) (models.ServiceTarget, error) {
	services, err := s.discovery.Discover(ctx)
	if err != nil {
		return models.ServiceTarget{}, err
	}
	var hit *models.ServiceTarget
	for i := range services {
		if services[i].Name == name {
			if hit != nil {
				return models.ServiceTarget{}, fmt.Errorf("service name %q is ambiguous across namespaces", name)
			}
			hit = &services[i]
		}
	}
	if hit == nil {
		return models.ServiceTarget{}, fmt.Errorf("service %q not found", name)
	}
	return *hit, nil
}

func (s *Server) currentReplicas(ctx context.Context, target models.ServiceTarget) (int32, error) {
	if s.kube == nil {
		return 0, fmt.Errorf("kubernetes client unavailable")
	}
	dep, err := s.kube.GetDeployment(ctx, target.Namespace, target.Name)
	if err != nil {
		return 0, err
	}
	if dep.Spec.Replicas == nil {
		return 1, nil
	}
	return *dep.Spec.Replicas, nil
}

func predictionToPayload(p *forecaster.PredictionResponse) *predictionResponsePayload {
	if p == nil {
		return nil
	}
	return &predictionResponsePayload{
		ServiceName:         p.ServiceName,
		RpsP50:              p.RpsP50,
		RpsP90:              p.RpsP90,
		RecommendedReplicas: p.RecommendedReplicas,
		ScalingMode:         p.ScalingMode,
		ConfidenceScore:     p.ConfidenceScore,
		Reason:              p.Reason,
		ModelVersion:        p.ModelVersion,
		Timestamp:           p.Timestamp,
	}
}

func parsePositiveInt(raw string, defaultVal int, min int, max int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultVal
	}
	if n < min {
		n = min
	}
	if n > max {
		n = max
	}
	return n
}

func parsePathInt64(raw string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid id %q", raw)
	}
	return n, nil
}

func decodeJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
