package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/lib/pq"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
)

// SQLStorage implements Storage using SQL database (PostgreSQL)
type SQLStorage struct {
	db *sql.DB
}

// NewSQLStorage creates a new SQL storage instance from a DATABASE_URL.
// It uses the pq driver's NewConnector for proper driver-specific connection handling.
func NewSQLStorage(ctx context.Context, connStr string) (*SQLStorage, error) {
	if connStr == "" {
		return nil, fmt.Errorf("database connection string is required")
	}
	host, dbname := parseConnConfig(connStr)
	l := contextlog.From(ctx).With(
		slog.String("host", host),
		slog.String("database", dbname),
	)

	// Use pq's driver-specific connection helper for proper DSN handling
	connector, err := pq.NewConnector(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create connector: %w", err)
	}

	db := sql.OpenDB(connector)

	// Configure connection pool for small instances
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			l.ErrorContext(ctx, "failed to close database after ping error", slog.Any("error", closeErr))
		}
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	storage := &SQLStorage{db: db}

	// Ensure table exists
	if err := storage.ensureTableExists(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ensure table exists: %w", err)
	}

	l.InfoContext(ctx, "Connected to SQL database")

	return storage, nil
}

// parseConnConfig extracts host and database name from a connection config string.
// Supports both URL format (returns key=value pairs) and DSN format (user=... host=... dbname=...).
func parseConnConfig(configStr string) (host, dbname string) {
	// Try URL format first (postgresql://...)
	config, err := pq.ParseURL(configStr)
	if err != nil {
		// For DSN format (user=... host=... dbname=...), parse manually
		config = configStr
	}

	// Parse key=value pairs from the config string
	parts := make(map[string]string)
	for pair := range strings.FieldsSeq(config) {
		if key, val, found := strings.Cut(pair, "="); found {
			parts[key] = val
		}
	}

	return parts["host"], parts["dbname"]
}

// ensureTableExists creates the submissions table if it doesn't exist
func (s *SQLStorage) ensureTableExists(ctx context.Context) error {
	queries := []string{
		`
		CREATE TABLE IF NOT EXISTS submissions (
			submission_id TEXT PRIMARY KEY,
			project TEXT,
			score FLOAT8,
			result JSONB NOT NULL,
			uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
		`,
		`CREATE INDEX IF NOT EXISTS idx_project ON submissions(project)`,
		`CREATE INDEX IF NOT EXISTS idx_uploaded_at ON submissions(uploaded_at)`,
		`CREATE INDEX IF NOT EXISTS idx_score ON submissions(score)`,
	}

	for _, query := range queries {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute schema query: %w", err)
		}
	}

	contextlog.From(ctx).InfoContext(ctx, "Ensured submissions table exists")

	return nil
}

// SaveResult saves a rubric result to the database
func (s *SQLStorage) SaveResult(ctx context.Context, result *proto.Result) error {
	start := time.Now()

	if result == nil {
		return fmt.Errorf("result cannot be nil")
	}
	if result.SubmissionId == "" {
		return fmt.Errorf("result.SubmissionId cannot be empty")
	}

	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}
	data, err := marshaler.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	// Calculate score from rubric items
	scoreVal := 0.0
	for _, item := range result.Rubric {
		scoreVal += item.Awarded
	}

	const q = `
		INSERT INTO submissions (submission_id, project, score, result, uploaded_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (submission_id) 
		DO UPDATE SET 
			project = EXCLUDED.project,
			score = EXCLUDED.score,
			result = EXCLUDED.result,
			uploaded_at = EXCLUDED.uploaded_at
	`

	// Store NULL if score is not a valid number
	var scoreToStore any
	if math.IsNaN(scoreVal) || math.IsInf(scoreVal, 0) {
		scoreToStore = nil
	} else {
		scoreToStore = scoreVal
	}

	if _, err = s.db.ExecContext(ctx, q, result.SubmissionId, result.Project, scoreToStore, string(data), time.Now()); err != nil {
		return fmt.Errorf("failed to save result to database: %w", err)
	}

	contextlog.From(ctx).InfoContext(ctx, "Saved rubric result",
		slog.String("submission_id", result.SubmissionId),
		slog.String("project", result.Project),
		slog.Float64("score", scoreVal),
		slog.Duration("duration", time.Since(start)),
	)

	return nil
}

// LoadResult loads a rubric result from the database
func (s *SQLStorage) LoadResult(ctx context.Context, submissionID string) (*proto.Result, error) {
	start := time.Now()

	const q = `SELECT result FROM submissions WHERE submission_id = $1`

	var resultJSON []byte
	if err := s.db.QueryRowContext(ctx, q, submissionID).Scan(&resultJSON); errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("submission not found: %s", submissionID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to load result from database: %w", err)
	}

	var result proto.Result
	unmarshaler := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	if err := unmarshaler.Unmarshal(resultJSON, &result); err != nil {
		return nil, fmt.Errorf("failed to decode result: %w", err)
	}

	contextlog.From(ctx).InfoContext(ctx, "Loaded rubric result",
		slog.String("submission_id", submissionID),
		slog.Duration("duration", time.Since(start)),
	)

	return &result, nil
}

// ListResultsPaginated loads rubric results with pagination.
// Page numbers start at 1.
func (s *SQLStorage) ListResultsPaginated(
	ctx context.Context,
	params ListResultsParams,
) (results map[string]*proto.Result, totalCount int, err error) {
	start := time.Now()

	// Validate and normalize parameters
	params = params.Validate()

	// Get total count
	const countQ = `SELECT COUNT(*) FROM submissions`
	if err = s.db.QueryRowContext(ctx, countQ).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("failed to count submissions: %w", err)
	}

	// If no results, return empty map
	if totalCount == 0 {
		return make(map[string]*proto.Result), 0, nil
	}

	// Calculate offset
	offset := (params.Page - 1) * params.PageSize

	// Fetch paginated results, ordered by uploaded_at DESC (newest first)
	const q = `
		SELECT submission_id, result 
		FROM submissions 
		ORDER BY uploaded_at DESC 
		LIMIT $1 OFFSET $2
	`

	rows, err := s.db.QueryContext(ctx, q, params.PageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query submissions: %w", err)
	}
	defer rows.Close()

	results = make(map[string]*proto.Result)
	unmarshaler := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	logger := contextlog.From(ctx)

	for rows.Next() {
		var submissionID string
		var resultJSON []byte
		if err := rows.Scan(&submissionID, &resultJSON); err != nil {
			logger.WarnContext(ctx, "Failed to scan row", slog.Any("error", err))
			continue
		}

		var result proto.Result
		if err := unmarshaler.Unmarshal(resultJSON, &result); err != nil {
			logger.WarnContext(ctx, "Failed to unmarshal result",
				slog.String("submission_id", submissionID),
				slog.Any("error", err),
			)
			continue
		}

		results[submissionID] = &result
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	contextlog.From(ctx).InfoContext(ctx, "Listed paginated rubric results",
		slog.Int("page", params.Page),
		slog.Int("page_size", params.PageSize),
		slog.Int("total_count", totalCount),
		slog.Int("returned", len(results)),
		slog.Duration("duration", time.Since(start)),
	)

	return results, totalCount, nil
}

// Close closes the database connection
func (s *SQLStorage) Close() error {
	return s.db.Close()
}
