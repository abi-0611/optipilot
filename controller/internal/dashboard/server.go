package dashboard

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/optipilot/controller/internal/config"
	"github.com/optipilot/controller/internal/forecaster"
	"github.com/optipilot/controller/internal/models"
	"github.com/optipilot/controller/internal/store"
)

//go:embed static/*
var embeddedStatic embed.FS

type DiscoveryReader interface {
	Discover(ctx context.Context) ([]models.ServiceTarget, error)
}

type PredictionReader interface {
	GetLatestPrediction(service string) *forecaster.PredictionResponse
	GetAllPredictions() map[string]*forecaster.PredictionResponse
	GetPredictionHistory(service string, limit int) []forecaster.PredictionResponse
}

type ActuatorControl interface {
	SetServiceMode(serviceName, mode string) error
	GetEffectiveServiceMode(target models.ServiceTarget) string
	SetGlobalKillSwitch(enabled bool)
	GlobalKillSwitchEnabled() bool
	SetServicePaused(serviceName string, paused bool)
	ServicePaused(serviceName string) bool
	ApproveRecommendation(ctx context.Context, id int64) (*models.ScalingDecision, error)
	RejectRecommendation(ctx context.Context, id int64) (*models.ScalingDecision, error)
}

type ForecasterControl interface {
	GetModelStatus(ctx context.Context, serviceName string) (*models.ModelStatus, error)
	GetAllServicesStatus(ctx context.Context) ([]models.ModelStatus, error)
	TriggerRetrain(ctx context.Context, serviceName string) (*forecaster.RetrainResult, error)
}

type KubeReader interface {
	GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error)
}

type Server struct {
	cfg            config.DashboardConfig
	prometheusAddr string

	store      store.Store
	discovery  DiscoveryReader
	predictor  PredictionReader
	actuator   ActuatorControl
	forecaster ForecasterControl
	kube       KubeReader
	events     *EventBus

	logger     *slog.Logger
	httpServer *http.Server
	wsHub      *wsHub
	httpClient *http.Client
	staticFS   fs.FS
	startedAt  time.Time
}

func NewServer(
	cfg config.DashboardConfig,
	prometheusAddr string,
	st store.Store,
	disc DiscoveryReader,
	pred PredictionReader,
	act ActuatorControl,
	fc ForecasterControl,
	kubeClient KubeReader,
	events *EventBus,
	logger *slog.Logger,
) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if events == nil {
		events = NewEventBus(logger)
	}
	if st == nil || disc == nil || act == nil {
		return nil, errors.New("dashboard server requires store, discovery, and actuator")
	}

	staticFS, err := resolveStaticFS(cfg.StaticDir)
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg:            cfg,
		prometheusAddr: strings.TrimRight(prometheusAddr, "/"),
		store:          st,
		discovery:      disc,
		predictor:      pred,
		actuator:       act,
		forecaster:     fc,
		kube:           kubeClient,
		events:         events,
		logger:         logger.With("component", "dashboard"),
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		staticFS:  staticFS,
		startedAt: time.Now().UTC(),
	}
	s.wsHub = newWSHub(events, cfg.CORSOrigin, s.logger)

	router := s.newRouter()
	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s, nil
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("dashboard server listening",
			"addr", s.httpServer.Addr,
			"websocket_path", s.cfg.WebsocketPath,
			"cors_origin", s.cfg.CORSOrigin,
		)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.wsHub.closeAll()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("dashboard shutdown: %w", err)
		}
		return nil
	}
}

func (s *Server) newRouter() http.Handler {
	mux := http.NewServeMux()
	s.registerRESTRoutes(mux)
	s.registerWebsocketRoute(mux)
	mux.Handle("/", s.staticHandler())
	return s.recoveryMiddleware(s.corsMiddleware(mux))
}

func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("dashboard panic recovered",
					"panic", rec,
					"path", r.URL.Path,
					"method", r.Method,
					"stack", string(debug.Stack()),
				)
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "internal server error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	allowOrigin := strings.TrimSpace(s.cfg.CORSOrigin)
	if allowOrigin == "" {
		allowOrigin = "*"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func resolveStaticFS(staticDir string) (fs.FS, error) {
	staticDir = strings.TrimSpace(staticDir)
	if staticDir != "" {
		info, err := os.Stat(staticDir)
		if err == nil && info.IsDir() {
			return os.DirFS(staticDir), nil
		}
	}
	sub, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return nil, fmt.Errorf("load embedded static files: %w", err)
	}
	return sub, nil
}

func (s *Server) staticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == s.cfg.WebsocketPath {
			http.NotFound(w, r)
			return
		}

		targetPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if targetPath == "" || targetPath == "." {
			targetPath = "index.html"
		}
		if _, err := fs.Stat(s.staticFS, targetPath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		clone := *r
		clone.URL = newURLWithPath(r, "/index.html")
		fileServer.ServeHTTP(w, &clone)
	})
}

func newURLWithPath(r *http.Request, p string) *url.URL {
	u := *r.URL
	u.Path = p
	return &u
}
