// Package forecaster provides a gRPC client for the OptiPilot forecaster
// service. All methods accept context and return domain types (internal/models)
// so callers never depend on proto types directly.
package forecaster

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/optipilot/controller/internal/models"
	optipilotv1 "github.com/optipilot/proto/gen/go/optipilot/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is the interface the rest of the controller uses. It exposes only
// the methods actually needed by the predictor and future actuator.
type Client interface {
	GetPrediction(ctx context.Context, req *PredictionRequest) (*PredictionResponse, error)
	GetModelStatus(ctx context.Context, serviceName string) (*models.ModelStatus, error)
	IngestMetrics(ctx context.Context, metrics []models.ServiceMetrics) (int32, error)
	GetAllServicesStatus(ctx context.Context) ([]models.ModelStatus, error)
	TriggerRetrain(ctx context.Context, serviceName string) (*RetrainResult, error)
	Close() error
}

// PredictionRequest is the controller-side request type for GetPrediction.
type PredictionRequest struct {
	ServiceName string
	RecentRPS   []float64
	Timestamp   time.Time
}

// PredictionResponse is the controller-side response from GetPrediction.
type PredictionResponse struct {
	ServiceName         string
	RpsP50              float64
	RpsP90              float64
	RecommendedReplicas int32
	ScalingMode         string // "PREDICTIVE" | "CONSERVATIVE" | "REACTIVE"
	ConfidenceScore     float64
	Reason              string
	ModelVersion        string
	Timestamp           time.Time
}

// RetrainResult is the controller-side result from TriggerRetrain.
type RetrainResult struct {
	Success         bool
	NewModelVersion string
	NewMAPE         float64
	Message         string
}

// grpcClient is the live implementation backed by a real gRPC connection.
type grpcClient struct {
	conn    *grpc.ClientConn
	stub    optipilotv1.OptiPilotServiceClient
	timeout time.Duration
	logger  *slog.Logger
}

// NewClient dials the forecaster and returns a ready Client. The connection
// is lazy (non-blocking) — if the forecaster is not running, errors will
// surface on the first RPC call, not here. This lets the controller start
// cleanly even when the forecaster is temporarily unavailable.
func NewClient(address string, timeout time.Duration, logger *slog.Logger) (Client, error) {
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("forecaster: dial %s: %w", address, err)
	}
	return &grpcClient{
		conn:    conn,
		stub:    optipilotv1.NewOptiPilotServiceClient(conn),
		timeout: timeout,
		logger:  logger,
	}, nil
}

func (c *grpcClient) GetPrediction(ctx context.Context, req *PredictionRequest) (*PredictionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.stub.GetPrediction(ctx, &optipilotv1.GetPredictionRequest{
		ServiceName: req.ServiceName,
		RecentRps:   req.RecentRPS,
		Timestamp:   timestampToProto(req.Timestamp),
	})
	if err != nil {
		return nil, fmt.Errorf("forecaster.GetPrediction: %w", err)
	}
	return &PredictionResponse{
		ServiceName:         resp.GetServiceName(),
		RpsP50:              resp.GetRpsP50(),
		RpsP90:              resp.GetRpsP90(),
		RecommendedReplicas: resp.GetRecommendedReplicas(),
		ScalingMode:         scalingModeFromProto(resp.GetScalingMode()),
		ConfidenceScore:     resp.GetConfidenceScore(),
		Reason:              resp.GetReason(),
		ModelVersion:        resp.GetModelVersion(),
		Timestamp:           time.Now(),
	}, nil
}

func (c *grpcClient) GetModelStatus(ctx context.Context, serviceName string) (*models.ModelStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.stub.GetModelStatus(ctx, &optipilotv1.GetModelStatusRequest{
		ServiceName: serviceName,
	})
	if err != nil {
		return nil, fmt.Errorf("forecaster.GetModelStatus: %w", err)
	}
	return modelStatusFromProto(resp), nil
}

// IngestMetrics sends a batch of metrics to the forecaster. It sends in a
// single RPC call — callers are responsible for chunking large batches.
func (c *grpcClient) IngestMetrics(ctx context.Context, metrics []models.ServiceMetrics) (int32, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	proto := make([]*optipilotv1.ServiceMetric, len(metrics))
	for i, m := range metrics {
		proto[i] = metricToProto(m)
	}

	resp, err := c.stub.IngestMetrics(ctx, &optipilotv1.IngestMetricsRequest{
		Metrics: proto,
	})
	if err != nil {
		return 0, fmt.Errorf("forecaster.IngestMetrics: %w", err)
	}
	return resp.GetAcceptedCount(), nil
}

func (c *grpcClient) GetAllServicesStatus(ctx context.Context) ([]models.ModelStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.stub.GetAllServicesStatus(ctx, &optipilotv1.AllServicesStatusRequest{})
	if err != nil {
		return nil, fmt.Errorf("forecaster.GetAllServicesStatus: %w", err)
	}
	out := make([]models.ModelStatus, 0, len(resp.GetServices()))
	for _, s := range resp.GetServices() {
		out = append(out, *modelStatusFromProto(s))
	}
	return out, nil
}

func (c *grpcClient) TriggerRetrain(ctx context.Context, serviceName string) (*RetrainResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.stub.TriggerRetrain(ctx, &optipilotv1.TriggerRetrainRequest{
		ServiceName: serviceName,
	})
	if err != nil {
		return nil, fmt.Errorf("forecaster.TriggerRetrain: %w", err)
	}
	return &RetrainResult{
		Success:         resp.GetSuccess(),
		NewModelVersion: resp.GetNewModelVersion(),
		NewMAPE:         resp.GetNewMape(),
		Message:         resp.GetMessage(),
	}, nil
}

func (c *grpcClient) Close() error {
	return c.conn.Close()
}
