package forecaster

import (
	"time"

	"github.com/optipilot/controller/internal/models"
	optipilotv1 "github.com/optipilot/proto/gen/go/optipilot/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// scalingModeFromProto maps the proto enum to the canonical string used
// throughout the controller ("PREDICTIVE", "CONSERVATIVE", "REACTIVE").
func scalingModeFromProto(m optipilotv1.ScalingMode) string {
	switch m {
	case optipilotv1.ScalingMode_SCALING_MODE_PREDICTIVE:
		return "PREDICTIVE"
	case optipilotv1.ScalingMode_SCALING_MODE_CONSERVATIVE:
		return "CONSERVATIVE"
	default:
		return "REACTIVE"
	}
}

// metricToProto converts one internal ServiceMetrics row to the proto wire
// type sent to the forecaster via IngestMetrics. Note: proto has p99 only
// (no p95 field), matching the Python forecaster's ServiceMetric dataclass.
func metricToProto(m models.ServiceMetrics) *optipilotv1.ServiceMetric {
	return &optipilotv1.ServiceMetric{
		ServiceName:       m.ServiceName,
		Rps:               m.RPS,
		AvgLatencyMs:      m.AvgLatencyMs,
		P99LatencyMs:      m.P99LatencyMs,
		ActiveConnections: m.ActiveConns,
		CpuUsagePercent:   m.CPUPercent,
		MemoryUsageMb:     m.MemoryMB,
		ErrorRate:         m.ErrorRate,
		Timestamp:         timestampToProto(m.CollectedAt),
	}
}

// modelStatusFromProto converts a GetModelStatusResponse to the internal
// ModelStatus type stored in the controller's SQLite db.
func modelStatusFromProto(r *optipilotv1.GetModelStatusResponse) *models.ModelStatus {
	return &models.ModelStatus{
		ServiceName:        r.GetServiceName(),
		ModelVersion:       r.GetModelVersion(),
		CurrentMAPE:        r.GetCurrentMape(),
		ScalingMode:        scalingModeFromProto(r.GetScalingMode()),
		LastTrainedAt:      timestampFromProto(r.GetLastTrainedAt()),
		LastRecalibratedAt: timestampFromProto(r.GetLastRecalibratedAt()),
		TrainingDataPoints: r.GetTrainingDataPoints(),
		UpdatedAt:          time.Now(),
	}
}

func timestampToProto(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t)
}

func timestampFromProto(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
