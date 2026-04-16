package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/optipilot/controller/internal/models"

	// Pure-Go SQLite driver (no CGO). Registers the "sqlite" driver on import.
	_ "modernc.org/sqlite"
)

// SQLiteStore is the production implementation of Store. It is safe for
// concurrent use: *sql.DB manages its own connection pool, and prepared
// statements are goroutine-safe.
type SQLiteStore struct {
	db *sql.DB

	// Hot-path prepared statements. Prepared once on construction and
	// closed on Close(). Less-frequent queries are built ad-hoc.
	stmtInsertMetric        *sql.Stmt
	stmtSelectLatestMetric  *sql.Stmt
	stmtSelectRecentMetrics *sql.Stmt
	stmtInsertDecision      *sql.Stmt
	stmtUpsertModel         *sql.Stmt
	stmtSelectModel         *sql.Stmt
}

// NewSQLiteStore opens (creates if missing) the database at dbPath, applies
// the schema, prepares hot-path statements, and returns a ready Store. The
// connection string enables WAL for concurrent readers and busy_timeout to
// avoid spurious "database is locked" errors under contention.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}

	// Verify the connection is actually usable before we start preparing
	// statements — sql.Open is lazy.
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", dbPath, err)
	}

	if err := applySchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	s := &SQLiteStore{db: db}
	if err := s.prepareStatements(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("prepare statements: %w", err)
	}
	return s, nil
}

const schemaDDL = `
CREATE TABLE IF NOT EXISTS service_metrics (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    service_name    TEXT    NOT NULL,
    rps             REAL    NOT NULL,
    avg_latency_ms  REAL    NOT NULL,
    p95_latency_ms  REAL    NOT NULL,
    p99_latency_ms  REAL    NOT NULL,
    active_conns    INTEGER NOT NULL,
    cpu_percent     REAL    NOT NULL,
    memory_mb       REAL    NOT NULL,
    error_rate      REAL    NOT NULL,
    collected_at    DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_metrics_service_time
    ON service_metrics (service_name, collected_at);

CREATE TABLE IF NOT EXISTS scaling_decisions (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    service_name     TEXT    NOT NULL,
    old_replicas     INTEGER NOT NULL,
    new_replicas     INTEGER NOT NULL,
    scaling_mode     TEXT    NOT NULL,
    model_version    TEXT    NOT NULL,
    reason           TEXT    NOT NULL,
    rps_p50          REAL    NOT NULL,
    rps_p90          REAL    NOT NULL,
    confidence_score REAL    NOT NULL,
    executed         BOOLEAN NOT NULL,
    created_at       DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_decisions_service
    ON scaling_decisions (service_name, created_at);

CREATE TABLE IF NOT EXISTS model_status (
    service_name          TEXT PRIMARY KEY,
    model_version         TEXT NOT NULL,
    current_mape          REAL NOT NULL,
    scaling_mode          TEXT NOT NULL,
    last_trained_at       DATETIME NOT NULL,
    last_recalibrated_at  DATETIME NOT NULL,
    training_data_points  INTEGER NOT NULL,
    updated_at            DATETIME NOT NULL
);
`

func applySchema(db *sql.DB) error {
	_, err := db.ExecContext(context.Background(), schemaDDL)
	return err
}

func (s *SQLiteStore) prepareStatements() error {
	var err error

	if s.stmtInsertMetric, err = s.db.Prepare(`
        INSERT INTO service_metrics
            (service_name, rps, avg_latency_ms, p95_latency_ms, p99_latency_ms,
             active_conns, cpu_percent, memory_mb, error_rate, collected_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`); err != nil {
		return fmt.Errorf("insert_metric: %w", err)
	}

	if s.stmtSelectLatestMetric, err = s.db.Prepare(`
        SELECT service_name, rps, avg_latency_ms, p95_latency_ms, p99_latency_ms,
               active_conns, cpu_percent, memory_mb, error_rate, collected_at
        FROM service_metrics
        WHERE service_name = ?
        ORDER BY collected_at DESC
        LIMIT 1`); err != nil {
		return fmt.Errorf("select_latest_metric: %w", err)
	}

	if s.stmtSelectRecentMetrics, err = s.db.Prepare(`
        SELECT service_name, rps, avg_latency_ms, p95_latency_ms, p99_latency_ms,
               active_conns, cpu_percent, memory_mb, error_rate, collected_at
        FROM service_metrics
        WHERE service_name = ? AND collected_at >= ?
        ORDER BY collected_at ASC`); err != nil {
		return fmt.Errorf("select_recent_metrics: %w", err)
	}

	if s.stmtInsertDecision, err = s.db.Prepare(`
        INSERT INTO scaling_decisions
            (service_name, old_replicas, new_replicas, scaling_mode, model_version,
             reason, rps_p50, rps_p90, confidence_score, executed, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`); err != nil {
		return fmt.Errorf("insert_decision: %w", err)
	}

	// INSERT ... ON CONFLICT lets us upsert by primary key (service_name).
	if s.stmtUpsertModel, err = s.db.Prepare(`
        INSERT INTO model_status
            (service_name, model_version, current_mape, scaling_mode,
             last_trained_at, last_recalibrated_at, training_data_points, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(service_name) DO UPDATE SET
            model_version        = excluded.model_version,
            current_mape         = excluded.current_mape,
            scaling_mode         = excluded.scaling_mode,
            last_trained_at      = excluded.last_trained_at,
            last_recalibrated_at = excluded.last_recalibrated_at,
            training_data_points = excluded.training_data_points,
            updated_at           = excluded.updated_at`); err != nil {
		return fmt.Errorf("upsert_model: %w", err)
	}

	if s.stmtSelectModel, err = s.db.Prepare(`
        SELECT service_name, model_version, current_mape, scaling_mode,
               last_trained_at, last_recalibrated_at, training_data_points, updated_at
        FROM model_status
        WHERE service_name = ?`); err != nil {
		return fmt.Errorf("select_model: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

func (s *SQLiteStore) SaveMetrics(ctx context.Context, m *models.ServiceMetrics) error {
	if m == nil {
		return errors.New("nil metrics")
	}
	_, err := s.stmtInsertMetric.ExecContext(ctx,
		m.ServiceName, m.RPS, m.AvgLatencyMs, m.P95LatencyMs, m.P99LatencyMs,
		m.ActiveConns, m.CPUPercent, m.MemoryMB, m.ErrorRate, m.CollectedAt.UTC())
	if err != nil {
		return fmt.Errorf("save metrics for %q: %w", m.ServiceName, err)
	}
	return nil
}

// SaveMetricsBatch writes all rows inside a single transaction so partial
// failures roll back cleanly. The per-row statement is prepared against the
// transaction (not reused from s.stmtInsertMetric) to avoid the lock that
// tx.Stmt would otherwise impose.
func (s *SQLiteStore) SaveMetricsBatch(ctx context.Context, metrics []models.ServiceMetrics) error {
	if len(metrics) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// Deferred Rollback is a no-op once Commit succeeds.
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO service_metrics
            (service_name, rps, avg_latency_ms, p95_latency_ms, p99_latency_ms,
             active_conns, cpu_percent, memory_mb, error_rate, collected_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare batch insert: %w", err)
	}
	defer stmt.Close()

	for i := range metrics {
		m := &metrics[i]
		if _, err := stmt.ExecContext(ctx,
			m.ServiceName, m.RPS, m.AvgLatencyMs, m.P95LatencyMs, m.P99LatencyMs,
			m.ActiveConns, m.CPUPercent, m.MemoryMB, m.ErrorRate, m.CollectedAt.UTC()); err != nil {
			return fmt.Errorf("batch insert row %d (%s): %w", i, m.ServiceName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRecentMetrics(ctx context.Context, serviceName string, minutes int) ([]models.ServiceMetrics, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
	rows, err := s.stmtSelectRecentMetrics.QueryContext(ctx, serviceName, cutoff)
	if err != nil {
		return nil, fmt.Errorf("query recent metrics for %q: %w", serviceName, err)
	}
	defer rows.Close()

	var out []models.ServiceMetrics
	for rows.Next() {
		m, err := scanMetric(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent metrics: %w", err)
	}
	return out, nil
}

func (s *SQLiteStore) GetLatestMetrics(ctx context.Context, serviceName string) (*models.ServiceMetrics, error) {
	row := s.stmtSelectLatestMetric.QueryRowContext(ctx, serviceName)
	m, err := scanMetric(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan latest metric for %q: %w", serviceName, err)
	}
	return &m, nil
}

// GetAllLatestMetrics uses a self-join against (service_name, MAX(collected_at))
// to pluck the newest row for each service in a single round-trip.
func (s *SQLiteStore) GetAllLatestMetrics(ctx context.Context) (map[string]*models.ServiceMetrics, error) {
	const q = `
        SELECT m.service_name, m.rps, m.avg_latency_ms, m.p95_latency_ms,
               m.p99_latency_ms, m.active_conns, m.cpu_percent, m.memory_mb,
               m.error_rate, m.collected_at
        FROM service_metrics m
        INNER JOIN (
            SELECT service_name, MAX(collected_at) AS max_t
            FROM service_metrics
            GROUP BY service_name
        ) latest
          ON latest.service_name = m.service_name
         AND latest.max_t        = m.collected_at`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query all-latest metrics: %w", err)
	}
	defer rows.Close()

	out := make(map[string]*models.ServiceMetrics)
	for rows.Next() {
		m, err := scanMetric(rows)
		if err != nil {
			return nil, err
		}
		// Defensive copy for the map.
		mCopy := m
		out[m.ServiceName] = &mCopy
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate all-latest metrics: %w", err)
	}
	return out, nil
}

// rowScanner is the common surface of *sql.Row and *sql.Rows so we can share
// scan code between single-row and multi-row paths.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanMetric(r rowScanner) (models.ServiceMetrics, error) {
	var m models.ServiceMetrics
	err := r.Scan(
		&m.ServiceName, &m.RPS, &m.AvgLatencyMs, &m.P95LatencyMs, &m.P99LatencyMs,
		&m.ActiveConns, &m.CPUPercent, &m.MemoryMB, &m.ErrorRate, &m.CollectedAt,
	)
	return m, err
}

// ---------------------------------------------------------------------------
// Scaling decisions
// ---------------------------------------------------------------------------

func (s *SQLiteStore) SaveScalingDecision(ctx context.Context, d *models.ScalingDecision) error {
	if d == nil {
		return errors.New("nil decision")
	}
	res, err := s.stmtInsertDecision.ExecContext(ctx,
		d.ServiceName, d.OldReplicas, d.NewReplicas, d.ScalingMode, d.ModelVersion,
		d.Reason, d.RpsP50, d.RpsP90, d.ConfidenceScore, d.Executed, d.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("save decision for %q: %w", d.ServiceName, err)
	}
	if id, err := res.LastInsertId(); err == nil {
		d.ID = id
	}
	return nil
}

func (s *SQLiteStore) GetRecentDecisions(ctx context.Context, limit int) ([]models.ScalingDecision, error) {
	const q = `
        SELECT id, service_name, old_replicas, new_replicas, scaling_mode,
               model_version, reason, rps_p50, rps_p90, confidence_score,
               executed, created_at
        FROM scaling_decisions
        ORDER BY created_at DESC
        LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent decisions: %w", err)
	}
	defer rows.Close()
	return scanDecisions(rows)
}

func (s *SQLiteStore) GetServiceDecisions(ctx context.Context, serviceName string, limit int) ([]models.ScalingDecision, error) {
	const q = `
        SELECT id, service_name, old_replicas, new_replicas, scaling_mode,
               model_version, reason, rps_p50, rps_p90, confidence_score,
               executed, created_at
        FROM scaling_decisions
        WHERE service_name = ?
        ORDER BY created_at DESC
        LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, serviceName, limit)
	if err != nil {
		return nil, fmt.Errorf("query decisions for %q: %w", serviceName, err)
	}
	defer rows.Close()
	return scanDecisions(rows)
}

func scanDecisions(rows *sql.Rows) ([]models.ScalingDecision, error) {
	var out []models.ScalingDecision
	for rows.Next() {
		var d models.ScalingDecision
		if err := rows.Scan(
			&d.ID, &d.ServiceName, &d.OldReplicas, &d.NewReplicas, &d.ScalingMode,
			&d.ModelVersion, &d.Reason, &d.RpsP50, &d.RpsP90, &d.ConfidenceScore,
			&d.Executed, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan decision: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate decisions: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Model status
// ---------------------------------------------------------------------------

func (s *SQLiteStore) UpsertModelStatus(ctx context.Context, m *models.ModelStatus) error {
	if m == nil {
		return errors.New("nil model status")
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = time.Now().UTC()
	}
	_, err := s.stmtUpsertModel.ExecContext(ctx,
		m.ServiceName, m.ModelVersion, m.CurrentMAPE, m.ScalingMode,
		m.LastTrainedAt.UTC(), m.LastRecalibratedAt.UTC(), m.TrainingDataPoints,
		m.UpdatedAt.UTC())
	if err != nil {
		return fmt.Errorf("upsert model status for %q: %w", m.ServiceName, err)
	}
	return nil
}

func (s *SQLiteStore) GetModelStatus(ctx context.Context, serviceName string) (*models.ModelStatus, error) {
	row := s.stmtSelectModel.QueryRowContext(ctx, serviceName)
	var m models.ModelStatus
	err := row.Scan(
		&m.ServiceName, &m.ModelVersion, &m.CurrentMAPE, &m.ScalingMode,
		&m.LastTrainedAt, &m.LastRecalibratedAt, &m.TrainingDataPoints, &m.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan model status for %q: %w", serviceName, err)
	}
	return &m, nil
}

func (s *SQLiteStore) GetAllModelStatuses(ctx context.Context) (map[string]*models.ModelStatus, error) {
	const q = `
        SELECT service_name, model_version, current_mape, scaling_mode,
               last_trained_at, last_recalibrated_at, training_data_points, updated_at
        FROM model_status`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query model statuses: %w", err)
	}
	defer rows.Close()

	out := make(map[string]*models.ModelStatus)
	for rows.Next() {
		var m models.ModelStatus
		if err := rows.Scan(
			&m.ServiceName, &m.ModelVersion, &m.CurrentMAPE, &m.ScalingMode,
			&m.LastTrainedAt, &m.LastRecalibratedAt, &m.TrainingDataPoints, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan model status: %w", err)
		}
		mCopy := m
		out[m.ServiceName] = &mCopy
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model statuses: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Maintenance
// ---------------------------------------------------------------------------

func (s *SQLiteStore) PurgeOldMetrics(ctx context.Context, olderThanHours int) error {
	cutoff := time.Now().UTC().Add(-time.Duration(olderThanHours) * time.Hour)
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM service_metrics WHERE collected_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("purge old metrics: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetMetricsCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_metrics`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count metrics: %w", err)
	}
	return n, nil
}

// Close releases prepared statements and the underlying *sql.DB. Errors from
// statement closes are joined so the caller sees the full picture.
func (s *SQLiteStore) Close() error {
	var errs []error
	for _, st := range []*sql.Stmt{
		s.stmtInsertMetric, s.stmtSelectLatestMetric, s.stmtSelectRecentMetrics,
		s.stmtInsertDecision, s.stmtUpsertModel, s.stmtSelectModel,
	} {
		if st == nil {
			continue
		}
		if err := st.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.db.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("close store: %w", errors.Join(errs...))
	}
	return nil
}
