package storage_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

func TestNewSQLStorage(t *testing.T) {
	type args struct {
		databaseURL string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		setup   func(t *testing.T)
	}{
		{
			name:    "empty database URL",
			args:    args{databaseURL: ""},
			wantErr: true,
		},
		{
			name: "valid database URL",
			args: args{databaseURL: os.Getenv("DATABASE_URL")},
			setup: func(t *testing.T) {
				skipIfNoSQL(t)
			},
			wantErr: false,
		},
		{
			name:    "invalid database URL",
			args:    args{databaseURL: "postgres://invalid:invalid@invalid:5432/invalid"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}

			ctx := context.Background()
			s, err := storage.NewSQLStorage(ctx, tt.args.databaseURL)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, s)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, s)
				if s != nil {
					defer s.Close()
				}
			}
		})
	}
}

func TestSQLStorage_SaveResult(t *testing.T) {
	skipIfNoSQL(t)

	type args struct {
		result *proto.Result
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "valid save",
			args: args{
				result: &proto.Result{
					SubmissionId: "test-001",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*proto.RubricItem{
						{Name: "Test Item", Points: 10, Awarded: 10},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "overwrite existing",
			args: args{
				result: &proto.Result{
					SubmissionId: "test-002",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*proto.RubricItem{
						{Name: "Updated Item", Points: 20, Awarded: 15},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty submission_id",
			args: args{
				result: &proto.Result{
					SubmissionId: "",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*proto.RubricItem{
						{Name: "Test Item", Points: 10, Awarded: 10},
					},
				},
			},
			wantErr: true,
		},
	}

	ctx := context.Background()
	store := createTestSQLStorage(t)
	defer store.Close()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.SaveResult(ctx, tt.args.result)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSQLStorage_LoadResult(t *testing.T) {
	skipIfNoSQL(t)

	ctx := context.Background()
	store := createTestSQLStorage(t)
	defer store.Close()

	// Setup: Save a result first
	testResult := &proto.Result{
		SubmissionId: "test-load-001",
		Timestamp:    time.Now().Format(time.RFC3339),
		Rubric: []*proto.RubricItem{
			{Name: "Test Item", Points: 10, Awarded: 8},
		},
		IpAddress:   "127.0.0.1",
		GeoLocation: "Test Location",
	}
	err := store.SaveResult(ctx, testResult)
	require.NoError(t, err)

	type args struct {
		submissionID string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		verify  func(t *testing.T, result *proto.Result)
	}{
		{
			name:    "valid load",
			args:    args{submissionID: "test-load-001"},
			wantErr: false,
			verify: func(t *testing.T, result *proto.Result) {
				assert.Equal(t, "test-load-001", result.SubmissionId)
				assert.Equal(t, "127.0.0.1", result.IpAddress)
				assert.Equal(t, "Test Location", result.GeoLocation)
				assert.Len(t, result.Rubric, 1)
				assert.Equal(t, "Test Item", result.Rubric[0].Name)
				assert.Equal(t, 10.0, result.Rubric[0].Points)
				assert.Equal(t, 8.0, result.Rubric[0].Awarded)
			},
		},
		{
			name:    "non-existent submission",
			args:    args{submissionID: "non-existent"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := store.LoadResult(ctx, tt.args.submissionID)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.verify != nil {
					tt.verify(t, result)
				}
			}
		})
	}
}

func TestSQLStorage_ListResultsPaginated(t *testing.T) {
	skipIfNoSQL(t)

	ctx := context.Background()
	store := createTestSQLStorage(t)
	defer store.Close()

	// Setup: Clear and create test data
	clearTestData(t, store)
	createTestData(t, store, 25) // Create 25 test submissions

	type args struct {
		params storage.ListResultsParams
	}
	tests := []struct {
		name      string
		args      args
		wantCount int
		wantTotal int
		wantErr   bool
	}{
		{
			name: "first page default size",
			args: args{
				params: storage.ListResultsParams{Page: 1, PageSize: 20},
			},
			wantCount: 20,
			wantTotal: 25,
			wantErr:   false,
		},
		{
			name: "second page default size",
			args: args{
				params: storage.ListResultsParams{Page: 2, PageSize: 20},
			},
			wantCount: 5,
			wantTotal: 25,
			wantErr:   false,
		},
		{
			name: "custom page size",
			args: args{
				params: storage.ListResultsParams{Page: 1, PageSize: 10},
			},
			wantCount: 10,
			wantTotal: 25,
			wantErr:   false,
		},
		{
			name: "invalid page defaults to 1",
			args: args{
				params: storage.ListResultsParams{Page: 0, PageSize: 10},
			},
			wantCount: 10,
			wantTotal: 25,
			wantErr:   false,
		},
		{
			name: "page size too large defaults to max",
			args: args{
				params: storage.ListResultsParams{Page: 1, PageSize: 200},
			},
			wantCount: 25,
			wantTotal: 25,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, totalCount, err := store.ListResultsPaginated(ctx, tt.args.params)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantTotal, totalCount)
				assert.Len(t, results, tt.wantCount)
			}
		})
	}
}

func skipIfNoSQL(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
}

func TestSQLStorage_ListResultsPaginated_EmptyDatabase(t *testing.T) {
	skipIfNoSQL(t)

	ctx := context.Background()
	store := createTestSQLStorage(t)
	defer store.Close()

	// Clear all data
	clearTestData(t, store)

	results, totalCount, err := store.ListResultsPaginated(ctx, storage.ListResultsParams{
		Page:     1,
		PageSize: 20,
	})

	assert.NoError(t, err)
	assert.Equal(t, 0, totalCount)
	assert.Empty(t, results)
}

// Helper functions

func createTestSQLStorage(t *testing.T) *storage.SQLStorage {
	t.Helper()
	ctx := context.Background()
	store, err := storage.NewSQLStorage(ctx, os.Getenv("DATABASE_URL"))
	require.NoError(t, err)
	return store
}

func clearTestData(t *testing.T, store *storage.SQLStorage) {
	t.Helper()

	// Connect directly to clear data
	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	require.NoError(t, err)
	defer db.Close()

	_, err = db.ExecContext(t.Context(), "TRUNCATE TABLE submissions")
	require.NoError(t, err)
}

func createTestData(t *testing.T, store *storage.SQLStorage, count int) {
	t.Helper()
	ctx := context.Background()

	for i := range count {
		result := &proto.Result{
			SubmissionId: fmt.Sprintf("test-%03d", i),
			Timestamp:    time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
			Rubric: []*proto.RubricItem{
				{Name: fmt.Sprintf("Item %d", i), Points: 10, Awarded: float64(i % 11)},
			},
		}
		err := store.SaveResult(ctx, result)
		require.NoError(t, err)
	}
}

func TestSQLStorageErrorScenarios(t *testing.T) {
	skipIfNoSQL(t)

	t.Run("save_with_invalid_proto_data", func(t *testing.T) {
		store := createTestSQLStorage(t)
		defer store.Close()

		// Test with extremely large data that might cause issues
		result := &proto.Result{
			SubmissionId: "test-large",
			Timestamp:    time.Now().Format(time.RFC3339),
			Rubric:       make([]*proto.RubricItem, 1000),
		}

		for i := range result.Rubric {
			result.Rubric[i] = &proto.RubricItem{
				Name:    fmt.Sprintf("Item %d", i),
				Points:  10,
				Awarded: float64(i % 11),
				Note:    strings.Repeat("x", 100),
			}
		}

		err := store.SaveResult(t.Context(), result)
		assert.NoError(t, err) // Should handle large data
	})

	t.Run("list_with_negative_page", func(t *testing.T) {
		store := createTestSQLStorage(t)
		defer store.Close()

		params := storage.ListResultsParams{
			Page:     -1,
			PageSize: 10,
		}

		results, totalCount, err := store.ListResultsPaginated(t.Context(), params)
		assert.NoError(t, err)
		assert.NotNil(t, results)
		assert.GreaterOrEqual(t, totalCount, 0)
	})

	t.Run("list_with_zero_page_size", func(t *testing.T) {
		store := createTestSQLStorage(t)
		defer store.Close()

		params := storage.ListResultsParams{
			Page:     1,
			PageSize: 0,
		}

		results, totalCount, err := store.ListResultsPaginated(t.Context(), params)
		assert.NoError(t, err)
		assert.NotNil(t, results)
		assert.GreaterOrEqual(t, totalCount, 0)
	})
}
