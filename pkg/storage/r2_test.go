package storage_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

const (
	testEndpoint = "https://example.com"
	testBucket   = "test-bucket"
)

func skipIfNoEndpoint(t *testing.T) {
	t.Helper()
	if os.Getenv("R2_ENDPOINT") == "" {
		t.Skip("Skipping R2 test: R2_ENDPOINT environment variable not set")
	}
}

func createTestStorage(t *testing.T) *storage.R2Storage {
	t.Helper()
	skipIfNoEndpoint(t)

	// Clean test name to create valid bucket name (lowercase, alphanumeric, hyphens only)
	bucketName := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(t.Name(), "/", "-"), "_", "-"))
	bucketName = "test-" + bucketName + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	cfg := &storage.Config{
		Endpoint:        os.Getenv("R2_ENDPOINT"),
		Bucket:          bucketName,
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		UsePathStyle:    true,
	}

	s, err := storage.NewR2Storage(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, s)

	return s
}

// TestNewConfig tests the configuration constructor
func TestNewConfig(t *testing.T) {
	type args struct {
		endpoint        string
		region          string
		bucket          string
		accessKeyID     string
		secretAccessKey string
		usePathStyle    bool
	}
	tests := []struct {
		name           string
		args           args
		expectedRegion string
		expectedBucket string
	}{
		{
			name: "valid_config",
			args: args{
				endpoint:        testEndpoint,
				region:          "us-east-1",
				bucket:          testBucket,
				accessKeyID:     "key123",
				secretAccessKey: "secret456",
				usePathStyle:    true,
			},
			expectedRegion: "us-east-1",
			expectedBucket: testBucket,
		},
		{
			name: "empty_bucket_uses_default",
			args: args{
				endpoint:        testEndpoint,
				region:          "us-west-2",
				bucket:          "",
				accessKeyID:     "key123",
				secretAccessKey: "secret456",
				usePathStyle:    false,
			},
			expectedRegion: "us-west-2",
			expectedBucket: "gradebot-storage",
		},
		{
			name: "empty_region_uses_default",
			args: args{
				endpoint:        testEndpoint,
				region:          "",
				bucket:          "my-bucket",
				accessKeyID:     "key123",
				secretAccessKey: "secret456",
				usePathStyle:    false,
			},
			expectedRegion: "auto",
			expectedBucket: "my-bucket",
		},
		{
			name: "empty_values_use_defaults",
			args: args{
				endpoint:        "",
				region:          "",
				bucket:          "",
				accessKeyID:     "",
				secretAccessKey: "",
				usePathStyle:    false,
			},
			expectedRegion: "auto",
			expectedBucket: "gradebot-storage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := storage.NewConfig(tt.args.endpoint, tt.args.region, tt.args.bucket, tt.args.accessKeyID, tt.args.secretAccessKey, tt.args.usePathStyle)
			assert.NotNil(t, cfg)
			assert.Equal(t, tt.args.endpoint, cfg.Endpoint)
			assert.Equal(t, tt.expectedRegion, cfg.Region)
			assert.Equal(t, tt.expectedBucket, cfg.Bucket)
			assert.Equal(t, tt.args.accessKeyID, cfg.AccessKeyID)
			assert.Equal(t, tt.args.secretAccessKey, cfg.SecretAccessKey)
			assert.Equal(t, tt.args.usePathStyle, cfg.UsePathStyle)
		})
	}
}

// TestNewR2Storage tests storage initialization
func TestNewR2Storage(t *testing.T) {
	skipIfNoEndpoint(t)

	type args struct {
		cfg *storage.Config
	}
	tests := []struct {
		name      string
		args      args
		wantError bool
	}{
		{
			name: "valid_config",
			args: args{
				cfg: &storage.Config{
					Endpoint:        os.Getenv("R2_ENDPOINT"),
					Bucket:          strings.ReplaceAll("test-newstorage-"+strconv.FormatInt(time.Now().UnixNano(), 36), "_", "-"),
					AccessKeyID:     "test",
					SecretAccessKey: "test",
					UsePathStyle:    true,
				},
			},
			wantError: false,
		},
		{
			name: "missing_endpoint",
			args: args{
				cfg: &storage.Config{
					Bucket:          testBucket,
					AccessKeyID:     "test",
					SecretAccessKey: "test",
				},
			},
			wantError: true,
		},
		{
			name: "empty_credentials",
			args: args{
				cfg: &storage.Config{
					Endpoint:        os.Getenv("R2_ENDPOINT"),
					Bucket:          strings.ReplaceAll("test-empty-creds-"+strconv.FormatInt(time.Now().UnixNano(), 36), "_", "-"),
					AccessKeyID:     "",
					SecretAccessKey: "",
					UsePathStyle:    true,
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := storage.NewR2Storage(context.Background(), tt.args.cfg)
			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, s)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, s)
			}
		})
	}
}

// TestSaveResult tests saving results to storage
func TestSaveResult(t *testing.T) {
	s := createTestStorage(t)
	ctx := context.Background()

	type args struct {
		submissionID string
		result       *proto.Result
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		setup   func(t *testing.T)
		verify  func(t *testing.T)
	}{
		{
			name: "valid_result",
			args: args{
				submissionID: "test-001",
				result: &proto.Result{
					SubmissionId: "test-001",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*proto.RubricItem{
						{Name: "Test Item", Points: 10.0, Awarded: 8.0, Note: "Good work"},
					},
					IpAddress:   "192.168.1.1",
					GeoLocation: "Test Location",
				},
			},
			wantErr: false,
		},
		{
			name: "empty_submission_id",
			args: args{
				submissionID: "",
				result: &proto.Result{
					SubmissionId: "",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*proto.RubricItem{},
				},
			},
			wantErr: false,
		},
		{
			name: "nil_result",
			args: args{
				submissionID: "test-002",
				result:       nil,
			},
			wantErr: false,
		},
		{
			name: "empty_rubric",
			args: args{
				submissionID: "test-003",
				result: &proto.Result{
					SubmissionId: "test-003",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*proto.RubricItem{},
				},
			},
			wantErr: false,
		},
		{
			name: "large_result",
			args: args{
				submissionID: "test-004",
				result: &proto.Result{
					SubmissionId: "test-004",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*proto.RubricItem{
						{Name: "Item 1", Points: 100.0, Awarded: 95.0, Note: strings.Repeat("x", 10000)},
						{Name: "Item 2", Points: 50.0, Awarded: 45.0, Note: strings.Repeat("y", 10000)},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "overwrite_existing",
			args: args{
				submissionID: "overwrite-test",
				result: &proto.Result{
					SubmissionId: "overwrite-test",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*proto.RubricItem{
						{Name: "Second", Points: 20.0, Awarded: 15.0, Note: "Second version"},
					},
				},
			},
			setup: func(t *testing.T) {
				firstResult := &proto.Result{
					SubmissionId: "overwrite-test",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*proto.RubricItem{
						{Name: "First", Points: 10.0, Awarded: 5.0, Note: "First version"},
					},
				}
				err := s.SaveResult(ctx, "overwrite-test", firstResult)
				require.NoError(t, err)
				time.Sleep(100 * time.Millisecond)
			},
			verify: func(t *testing.T) {
				loaded, err := s.LoadResult(ctx, "overwrite-test")
				require.NoError(t, err)
				assert.Equal(t, "Second", loaded.Rubric[0].Name)
				assert.Equal(t, 20.0, loaded.Rubric[0].Points)
			},
			wantErr: false,
		},
		{
			name: "special_chars_hyphens",
			args: args{
				submissionID: "test-123-abc",
				result: &proto.Result{
					SubmissionId: "test-123-abc",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*proto.RubricItem{{Name: "Test", Points: 10, Awarded: 8}},
				},
			},
			wantErr: false,
		},
		{
			name: "special_chars_underscores",
			args: args{
				submissionID: "test_123_abc",
				result: &proto.Result{
					SubmissionId: "test_123_abc",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*proto.RubricItem{{Name: "Test", Points: 10, Awarded: 8}},
				},
			},
			wantErr: false,
		},
		{
			name: "special_chars_dots",
			args: args{
				submissionID: "test.123.abc",
				result: &proto.Result{
					SubmissionId: "test.123.abc",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*proto.RubricItem{{Name: "Test", Points: 10, Awarded: 8}},
				},
			},
			wantErr: false,
		},
		{
			name: "long_submission_id",
			args: args{
				submissionID: strings.Repeat("a", 200),
				result: &proto.Result{
					SubmissionId: strings.Repeat("a", 200),
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*proto.RubricItem{{Name: "Test", Points: 10, Awarded: 8}},
				},
			},
			wantErr: false,
		},
		{
			name: "concurrent_saves",
			args: args{
				submissionID: "concurrent-1",
				result: &proto.Result{
					SubmissionId: "concurrent-1",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*proto.RubricItem{{Name: "Test", Points: 10, Awarded: 8}},
				},
			},
			setup: func(t *testing.T) {
				done := make(chan bool, 9)
				errors := make(chan error, 9)
				for i := 2; i <= 10; i++ {
					go func(index int) {
						result := &proto.Result{
							SubmissionId: fmt.Sprintf("concurrent-%d", index),
							Timestamp:    time.Now().Format(time.RFC3339),
							Rubric:       []*proto.RubricItem{{Name: "Test", Points: 10, Awarded: 8}},
						}
						if err := s.SaveResult(ctx, fmt.Sprintf("concurrent-%d", index), result); err != nil {
							errors <- err
						}
						done <- true
					}(i)
				}
				for i := 2; i <= 10; i++ {
					<-done
				}
				close(errors)
				for err := range errors {
					require.NoError(t, err)
				}
			},
			verify: func(t *testing.T) {
				params := storage.ListResultsParams{Page: 1, PageSize: 100}
				results, totalCount, err := s.ListResultsPaginated(ctx, params)
				assert.NoError(t, err)
				assert.GreaterOrEqual(t, totalCount, 10)
				assert.GreaterOrEqual(t, len(results), 10)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}

			err := s.SaveResult(ctx, tt.args.submissionID, tt.args.result)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.verify != nil {
				tt.verify(t)
			}
		})
	}
}

// TestLoadResult tests loading results from storage
func TestLoadResult(t *testing.T) {
	s := createTestStorage(t)
	ctx := context.Background()

	// Pre-save test results
	testSubmissionID := "test-load-001"
	expectedResult := &proto.Result{
		SubmissionId: testSubmissionID,
		Timestamp:    time.Now().Format(time.RFC3339),
		Rubric: []*proto.RubricItem{
			{Name: "Test Item", Points: 10.0, Awarded: 8.0, Note: "Good"},
		},
		IpAddress:   "10.0.0.1",
		GeoLocation: "Test/Location",
	}
	err := s.SaveResult(ctx, testSubmissionID, expectedResult)
	require.NoError(t, err)

	// Save roundtrip test result
	roundtripID := "roundtrip-test"
	roundtripResult := &proto.Result{
		SubmissionId: roundtripID,
		Timestamp:    time.Now().Format(time.RFC3339),
		Rubric: []*proto.RubricItem{
			{Name: "Correctness", Points: 50.0, Awarded: 45.0, Note: "Almost perfect"},
			{Name: "Style", Points: 30.0, Awarded: 28.0, Note: "Good formatting"},
			{Name: "Tests", Points: 20.0, Awarded: 20.0, Note: "Excellent coverage"},
		},
		IpAddress:   "203.0.113.42",
		GeoLocation: "San Francisco, CA, USA",
	}
	err = s.SaveResult(ctx, roundtripID, roundtripResult)
	require.NoError(t, err)

	// Save special character test results
	specialIDs := []string{"test-123-abc", "test_123_abc", "test.123.abc", strings.Repeat("a", 200)}
	for _, id := range specialIDs {
		result := &proto.Result{
			SubmissionId: id,
			Timestamp:    time.Now().Format(time.RFC3339),
			Rubric:       []*proto.RubricItem{{Name: "Test", Points: 10, Awarded: 8}},
		}
		err = s.SaveResult(ctx, id, result)
		require.NoError(t, err)
	}

	type args struct {
		submissionID string
	}
	tests := []struct {
		name         string
		args         args
		wantErr      bool
		verifyResult func(t *testing.T, result *proto.Result)
	}{
		{
			name: "existing_result",
			args: args{
				submissionID: testSubmissionID,
			},
			wantErr: false,
			verifyResult: func(t *testing.T, result *proto.Result) {
				assert.Equal(t, expectedResult.SubmissionId, result.SubmissionId)
				assert.Equal(t, expectedResult.IpAddress, result.IpAddress)
				assert.Equal(t, expectedResult.GeoLocation, result.GeoLocation)
				assert.Len(t, result.Rubric, len(expectedResult.Rubric))
			},
		},
		{
			name: "non_existent_result",
			args: args{
				submissionID: "does-not-exist",
			},
			wantErr: true,
		},
		{
			name: "empty_submission_id",
			args: args{
				submissionID: "",
			},
			wantErr: true,
		},
		{
			name: "roundtrip_integrity",
			args: args{
				submissionID: roundtripID,
			},
			wantErr: false,
			verifyResult: func(t *testing.T, result *proto.Result) {
				assert.Equal(t, roundtripResult.SubmissionId, result.SubmissionId)
				assert.Equal(t, roundtripResult.Timestamp, result.Timestamp)
				assert.Equal(t, roundtripResult.IpAddress, result.IpAddress)
				assert.Equal(t, roundtripResult.GeoLocation, result.GeoLocation)
				require.Len(t, result.Rubric, len(roundtripResult.Rubric))
				for i, item := range roundtripResult.Rubric {
					assert.Equal(t, item.Name, result.Rubric[i].Name)
					assert.Equal(t, item.Points, result.Rubric[i].Points)
					assert.Equal(t, item.Awarded, result.Rubric[i].Awarded)
					assert.Equal(t, item.Note, result.Rubric[i].Note)
				}
			},
		},
		{
			name: "special_chars_hyphens",
			args: args{
				submissionID: "test-123-abc",
			},
			wantErr: false,
			verifyResult: func(t *testing.T, result *proto.Result) {
				assert.Equal(t, "test-123-abc", result.SubmissionId)
			},
		},
		{
			name: "special_chars_underscores",
			args: args{
				submissionID: "test_123_abc",
			},
			wantErr: false,
			verifyResult: func(t *testing.T, result *proto.Result) {
				assert.Equal(t, "test_123_abc", result.SubmissionId)
			},
		},
		{
			name: "special_chars_dots",
			args: args{
				submissionID: "test.123.abc",
			},
			wantErr: false,
			verifyResult: func(t *testing.T, result *proto.Result) {
				assert.Equal(t, "test.123.abc", result.SubmissionId)
			},
		},
		{
			name: "long_submission_id",
			args: args{
				submissionID: strings.Repeat("a", 200),
			},
			wantErr: false,
			verifyResult: func(t *testing.T, result *proto.Result) {
				assert.Equal(t, strings.Repeat("a", 200), result.SubmissionId)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := s.LoadResult(ctx, tt.args.submissionID)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.verifyResult != nil {
					tt.verifyResult(t, result)
				}
			}
		})
	}
}

// TestListResultsPaginated tests paginated listing of results
func TestListResultsPaginated(t *testing.T) {
	s := createTestStorage(t)
	ctx := context.Background()

	// Create 25 test results
	for i := range 25 {
		submissionID := fmt.Sprintf("paginated-test-%03d", i)
		result := &proto.Result{
			SubmissionId: submissionID,
			Timestamp:    time.Now().Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			Rubric: []*proto.RubricItem{
				{Name: "Item", Points: 100.0, Awarded: float64(50 + i), Note: "Test"},
			},
		}
		err := s.SaveResult(ctx, submissionID, result)
		require.NoError(t, err)
	}

	type args struct {
		page     int
		pageSize int
	}
	tests := []struct {
		name             string
		args             args
		wantErr          bool
		expectedCount    int
		expectedTotalMin int
		expectedTotalMax int
	}{
		{
			name:             "first_page",
			args:             args{page: 1, pageSize: 10},
			wantErr:          false,
			expectedCount:    10,
			expectedTotalMin: 25,
			expectedTotalMax: 25,
		},
		{
			name:             "middle_page",
			args:             args{page: 2, pageSize: 10},
			wantErr:          false,
			expectedCount:    10,
			expectedTotalMin: 25,
			expectedTotalMax: 25,
		},
		{
			name:             "last_page_partial",
			args:             args{page: 3, pageSize: 10},
			wantErr:          false,
			expectedCount:    5,
			expectedTotalMin: 25,
			expectedTotalMax: 25,
		},
		{
			name:             "page_beyond_range",
			args:             args{page: 100, pageSize: 10},
			wantErr:          false,
			expectedCount:    10, // Should clamp to last page
			expectedTotalMin: 25,
			expectedTotalMax: 25,
		},
		{
			name:             "negative_page",
			args:             args{page: -1, pageSize: 10},
			wantErr:          false,
			expectedCount:    10, // Should default to page 1
			expectedTotalMin: 25,
			expectedTotalMax: 25,
		},
		{
			name:             "zero_page",
			args:             args{page: 0, pageSize: 10},
			wantErr:          false,
			expectedCount:    10, // Should default to page 1
			expectedTotalMin: 25,
			expectedTotalMax: 25,
		},
		{
			name:             "large_page_size",
			args:             args{page: 1, pageSize: 100},
			wantErr:          false,
			expectedCount:    25, // All results
			expectedTotalMin: 25,
			expectedTotalMax: 25,
		},
		{
			name:             "page_size_one",
			args:             args{page: 1, pageSize: 1},
			wantErr:          false,
			expectedCount:    1,
			expectedTotalMin: 25,
			expectedTotalMax: 25,
		},
		{
			name:             "negative_page_size",
			args:             args{page: 1, pageSize: -5},
			wantErr:          false,
			expectedCount:    20, // Should default to 20
			expectedTotalMin: 25,
			expectedTotalMax: 25,
		},
		{
			name:             "zero_page_size",
			args:             args{page: 1, pageSize: 0},
			wantErr:          false,
			expectedCount:    20, // Should default to 20
			expectedTotalMin: 25,
			expectedTotalMax: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := storage.ListResultsParams{
				Page:     tt.args.page,
				PageSize: tt.args.pageSize,
			}

			results, totalCount, err := s.ListResultsPaginated(ctx, params)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, results)
				assert.Equal(t, tt.expectedCount, len(results), "Expected %d results, got %d", tt.expectedCount, len(results))
				assert.GreaterOrEqual(t, totalCount, tt.expectedTotalMin)
				assert.LessOrEqual(t, totalCount, tt.expectedTotalMax)
			}
		})
	}

	// Test empty list with fresh storage
	t.Run("empty_list", func(t *testing.T) {
		emptyStorage := createTestStorage(t)
		params := storage.ListResultsParams{Page: 1, PageSize: 10}
		results, totalCount, err := emptyStorage.ListResultsPaginated(ctx, params)
		assert.NoError(t, err)
		assert.NotNil(t, results)
		assert.Equal(t, 0, len(results))
		assert.Equal(t, 0, totalCount)
	})
}
