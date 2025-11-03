package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
)

// SQLStorage implements Storage using SQL database (PostgreSQL)
type SQLStorage struct {
	db *sql.DB
}

// NewSQLStorage creates a new SQL storage instance from a DATABASE_URL
func NewSQLStorage(ctx context.Context, databaseURL string) (*SQLStorage, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for small instances
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			contextlog.From(ctx).Error("failed to close database after ping error", "error", closeErr)
		}
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	storage := &SQLStorage{db: db}

	// Ensure table exists
	if err := storage.ensureTableExists(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ensure table exists: %w", err)
	}

	contextlog.From(ctx).Info("Connected to SQL database")

	return storage, nil
}

// ensureTableExists creates the submissions table if it doesn't exist
func (s *SQLStorage) ensureTableExists(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS submissions (
			submission_id TEXT PRIMARY KEY,
			project TEXT,
			result JSONB NOT NULL,
			uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_project ON submissions(project);
		CREATE INDEX IF NOT EXISTS idx_uploaded_at ON submissions(uploaded_at);
	`

	_, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	contextlog.From(ctx).Info("Ensured submissions table exists")
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

	const q = `
		INSERT INTO submissions (submission_id, project, result, uploaded_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (submission_id) 
		DO UPDATE SET 
			project = EXCLUDED.project,
			result = EXCLUDED.result,
			uploaded_at = EXCLUDED.uploaded_at
	`

	if _, err = s.db.ExecContext(ctx, q, result.SubmissionId, result.Project, string(data), time.Now()); err != nil {
		return fmt.Errorf("failed to save result to database: %w", err)
	}

	contextlog.From(ctx).Info("Saved rubric result",
		slog.String("submission_id", result.SubmissionId),
		slog.String("project", result.Project),
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

	contextlog.From(ctx).Info("Loaded rubric result",
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
			logger.Warn("Failed to scan row", "error", err)
			continue
		}

		var result proto.Result
		if err := unmarshaler.Unmarshal(resultJSON, &result); err != nil {
			logger.Warn("Failed to unmarshal result", "submission_id", submissionID, "error", err)
			continue
		}

		results[submissionID] = &result
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating rows: %w", err)
	}

	logger.Info("Listed paginated rubric results",
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
