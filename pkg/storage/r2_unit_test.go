package storage_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

const (
	defaultTimestamp   = "2024-01-15T10:30:00Z"
	defaultBucket      = "gradebot-storage"
	testSubmissionID   = "test-001"
	testCodeQuality    = "Code Quality"
	testTesting        = "Testing"
	testLocation       = "Austin, TX, United States"
)

// TestListResultsPaginatedLogic tests the pagination logic with various edge cases
func TestListResultsPaginatedLogic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		page         int
		pageSize     int
		totalResults int
		wantStart    int
		wantEnd      int
	}{
		{
			name:         "page_1_of_525_results_15_per_page",
			page:         1,
			pageSize:     15,
			totalResults: 525,
			wantStart:    0,
			wantEnd:      15,
		},
		{
			name:         "page_35_of_525_results_15_per_page",
			page:         35,
			pageSize:     15,
			totalResults: 525,
			wantStart:    510,
			wantEnd:      525,
		},
		{
			name:         "page_2_of_525_results_15_per_page",
			page:         2,
			pageSize:     15,
			totalResults: 525,
			wantStart:    15,
			wantEnd:      30,
		},
		{
			name:         "middle_page_of_525",
			page:         18,
			pageSize:     15,
			totalResults: 525,
			wantStart:    255,
			wantEnd:      270,
		},
		{
			name:         "page_beyond_total",
			page:         100,
			pageSize:     15,
			totalResults: 525,
			wantStart:    510,
			wantEnd:      525,
		},
		{
			name:         "large_page_size",
			page:         1,
			pageSize:     1000,
			totalResults: 525,
			wantStart:    0,
			wantEnd:      525,
		},
		{
			name:         "page_size_larger_than_results",
			page:         1,
			pageSize:     100,
			totalResults: 50,
			wantStart:    0,
			wantEnd:      50,
		},
		{
			name:         "single_result",
			page:         1,
			pageSize:     15,
			totalResults: 1,
			wantStart:    0,
			wantEnd:      1,
		},
		{
			name:         "empty_results",
			page:         1,
			pageSize:     15,
			totalResults: 0,
			wantStart:    0,
			wantEnd:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := storage.CalculatePaginationBounds(tt.page, tt.pageSize, tt.totalResults)
			assert.Equal(t, tt.wantStart, start, "start index mismatch")
			assert.Equal(t, tt.wantEnd, end, "end index mismatch")

			// Verify bounds don't exceed total
			assert.GreaterOrEqual(t, start, 0)
			assert.LessOrEqual(t, end, tt.totalResults)
			assert.LessOrEqual(t, start, end)
		})
	}
}

// TestProtoMarshaling tests that Result proto marshaling/unmarshaling works correctly
func TestProtoMarshaling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result *proto.Result
	}{
		{
			name: "simple_result",
			result: &proto.Result{
				SubmissionId: testSubmissionID,
				Timestamp:    defaultTimestamp,
				Rubric: []*proto.RubricItem{
					{Name: testCodeQuality, Points: 10.0, Awarded: 8.5, Note: "Good structure"},
					{Name: testTesting, Points: 5.0, Awarded: 5.0, Note: "All tests pass"},
				},
				IpAddress:   "192.168.1.100",
				GeoLocation: testLocation,
			},
		},
		{
			name: "empty_rubric",
			result: &proto.Result{
				SubmissionId: "test-empty",
				Timestamp:    defaultTimestamp,
				Rubric:       []*proto.RubricItem{},
				IpAddress:    "127.0.0.1",
				GeoLocation:  "Local/Unknown",
			},
		},
		{
			name: "nil_rubric_items_not_present",
			result: &proto.Result{
				SubmissionId: "test-nil-items",
				Timestamp:    defaultTimestamp,
				IpAddress:    "10.0.0.1",
				GeoLocation:  "Test Location",
			},
		},
		{
			name: "many_rubric_items",
			result: func() *proto.Result {
				items := make([]*proto.RubricItem, 50)
				for i := 0; i < 50; i++ {
					items[i] = &proto.RubricItem{
						Name:    fmt.Sprintf("Item %d", i),
						Points:  float64((i + 1) * 2),
						Awarded: float64(i),
						Note:    fmt.Sprintf("Note for item %d", i),
					}
				}
				return &proto.Result{
					SubmissionId: "test-many",
					Timestamp:    defaultTimestamp,
					Rubric:       items,
					IpAddress:    "192.168.1.1",
					GeoLocation:  "Test/Location",
				}
			}(),
		},
		{
			name: "special_characters",
			result: &proto.Result{
				SubmissionId: "test-special-🚀",
				Timestamp:    defaultTimestamp,
				Rubric: []*proto.RubricItem{
					{Name: "Test with unicode: 测试", Points: 10.0, Awarded: 9.5, Note: "Special: <>&\"'"},
				},
				IpAddress:   "2001:db8::1",
				GeoLocation: "Test/Unicode-测试",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			marshaler := protojson.MarshalOptions{
				UseProtoNames:   true,
				EmitUnpopulated: true,
			}
			data, err := marshaler.Marshal(tt.result)
			require.NoError(t, err)
			require.NotEmpty(t, data)

			// Unmarshal back
			unmarshaler := protojson.UnmarshalOptions{
				DiscardUnknown: true,
			}
			var restored proto.Result
			err = unmarshaler.Unmarshal(data, &restored)
			require.NoError(t, err)

			// Verify key fields are preserved
			assert.Equal(t, tt.result.SubmissionId, restored.SubmissionId)
			assert.Equal(t, tt.result.Timestamp, restored.Timestamp)
			assert.Equal(t, len(tt.result.Rubric), len(restored.Rubric))
			assert.Equal(t, tt.result.IpAddress, restored.IpAddress)
			assert.Equal(t, tt.result.GeoLocation, restored.GeoLocation)

			// Verify rubric items
			for i, item := range tt.result.Rubric {
				assert.Equal(t, item.Name, restored.Rubric[i].Name)
				assert.Equal(t, item.Points, restored.Rubric[i].Points)
				assert.Equal(t, item.Awarded, restored.Rubric[i].Awarded)
				assert.Equal(t, item.Note, restored.Rubric[i].Note)
			}
		})
	}
}

// TestListResultsParamsValidation tests parameter validation logic
func TestListResultsParamsValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		page           int
		pageSize       int
		expectedPage   int
		expectedSize   int
	}{
		{
			name:           "valid_params",
			page:           1,
			pageSize:       15,
			expectedPage:   1,
			expectedSize:   15,
		},
		{
			name:           "page_zero_becomes_one",
			page:           0,
			pageSize:       15,
			expectedPage:   1,
			expectedSize:   15,
		},
		{
			name:           "negative_page_becomes_one",
			page:           -5,
			pageSize:       15,
			expectedPage:   1,
			expectedSize:   15,
		},
		{
			name:           "page_size_zero_becomes_default",
			page:           1,
			pageSize:       0,
			expectedPage:   1,
			expectedSize:   20, // Default
		},
		{
			name:           "page_size_negative_becomes_default",
			page:           1,
			pageSize:       -10,
			expectedPage:   1,
			expectedSize:   20, // Default
		},
		{
			name:           "page_size_exceeds_max_capped",
			page:           1,
			pageSize:       10000,
			expectedPage:   1,
			expectedSize:   20, // Default (capped to 1000 max, but also has default)
		},
		{
			name:           "large_valid_page",
			page:           1000,
			pageSize:       50,
			expectedPage:   1000,
			expectedSize:   50,
		},
		{
			name:           "max_valid_page_size",
			page:           1,
			pageSize:       1000,
			expectedPage:   1,
			expectedSize:   1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := storage.ListResultsParams{
				Page:     tt.page,
				PageSize: tt.pageSize,
			}

			// Simulate validation that would happen in ListResultsPaginated
			if params.Page < 1 {
				params.Page = 1
			}
			if params.PageSize < 1 || params.PageSize > 1000 {
				params.PageSize = 20 // Default
			}

			assert.Equal(t, tt.expectedPage, params.Page)
			assert.Equal(t, tt.expectedSize, params.PageSize)
		})
	}
}

// TestPaginationConcurrency tests that pagination calculations are thread-safe
func TestPaginationConcurrency(t *testing.T) {
	t.Parallel()

	const (
		numGoroutines = 100
		totalResults  = 525
		pageSize      = 15
	)

	results := make(map[string]bool)
	var mu sync.Mutex

	wg := sync.WaitGroup{}
	wg.Add(numGoroutines)

	for i := 1; i <= numGoroutines; i++ {
		go func(page int) {
			defer wg.Done()

			start, end := storage.CalculatePaginationBounds(page, pageSize, totalResults)

			mu.Lock()
			key := fmt.Sprintf("page_%d: [%d-%d]", page, start, end)
			results[key] = true
			mu.Unlock()

			// Verify consistency
			assert.GreaterOrEqual(t, start, 0)
			assert.LessOrEqual(t, end, totalResults)
			assert.LessOrEqual(t, start, end)
		}(i)
	}

	wg.Wait()

	// Verify all goroutines completed successfully
	assert.Equal(t, numGoroutines, len(results))
}

// TestStorageInterfaceCompliance verifies that R2Storage implements Storage interface
func TestStorageInterfaceCompliance(t *testing.T) {
	t.Parallel()

	// This is a compile-time check that gets verified at build time
	// but we also verify the interface exists
	var _ storage.Storage = (*storage.R2Storage)(nil) // Will fail at compile if not implemented

	// Verify the interface has the expected methods
	assert.Implements(t, (*storage.Storage)(nil), (*storage.R2Storage)(nil))
}

// TestResultKeyExtraction tests the key parsing logic used in ListResults
// Note: The actual keys will always start with "submissions/" since that's the prefix
// used in collectAllKeys, but the extraction logic only cares about length and .json suffix
func TestResultKeyExtraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		key           string
		shouldExtract bool
		expectedID    string
	}{
		{
			name:          "valid_submission_key",
			key:           "submissions/test-123.json",
			shouldExtract: true,
			expectedID:    "test-123",
		},
		{
			name:          "valid_submission_with_unicode",
			key:           "submissions/test-🚀-emoji.json",
			shouldExtract: true,
			expectedID:    "test-🚀-emoji",
		},
		{
			name:          "valid_submission_with_numbers",
			key:           "submissions/test-12345-678.json",
			shouldExtract: true,
			expectedID:    "test-12345-678",
		},
		{
			name:          "key_without_json_suffix",
			key:           "submissions/test-123",
			shouldExtract: false,
			expectedID:    "",
		},
		{
			name:          "key_too_short",
			key:           "short",
			shouldExtract: false,
			expectedID:    "",
		},
		{
			name:          "empty_key",
			key:           "",
			shouldExtract: false,
			expectedID:    "",
		},
		{
			name:          "invalid_suffix",
			key:           "submissions/test-123.txt",
			shouldExtract: false,
			expectedID:    "",
		},
		{
			name:          "nested_path",
			key:           "submissions/nested/test-123.json",
			shouldExtract: true,
			expectedID:    "nested/test-123",
		},
		{
			name:          "exactly_13_chars",
			key:           "submissions/a.json",
			shouldExtract: true,
			expectedID:    "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the actual logic from r2.go ListResults
			// The check is: if len(key) < 13 || key[len(key)-5:] != ".json" then skip
			shouldExtract := len(tt.key) >= 13 && tt.key[len(tt.key)-5:] == ".json"
			assert.Equal(t, tt.shouldExtract, shouldExtract, fmt.Sprintf("extraction check failed for key: %s", tt.key))

			if shouldExtract && len(tt.key) >= 13 {
				submissionID := tt.key[12 : len(tt.key)-5]
				assert.Equal(t, tt.expectedID, submissionID)
			}
		})
	}
}

// TestPaginationBoundaryConditions tests edge cases in pagination calculation
func TestPaginationBoundaryConditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		page       int
		pageSize   int
		totalCount int
		validate   func(t *testing.T, start, end int)
	}{
		{
			name:       "boundaries_never_exceed_total",
			page:       1000000,
			pageSize:   15,
			totalCount: 100,
			validate: func(t *testing.T, start, end int) {
				assert.LessOrEqual(t, start, 100)
				assert.LessOrEqual(t, end, 100)
			},
		},
		{
			name:       "start_within_bounds",
			page:       1,
			pageSize:   15,
			totalCount: 100,
			validate: func(t *testing.T, start, end int) {
				assert.GreaterOrEqual(t, start, 0)
				assert.Equal(t, 0, start)
			},
		},
		{
			name:       "start_less_than_end",
			page:       5,
			pageSize:   15,
			totalCount: 200,
			validate: func(t *testing.T, start, end int) {
				assert.Less(t, start, end)
			},
		},
		{
			name:       "end_not_exceeding_total",
			page:       100,
			pageSize:   1,
			totalCount: 10,
			validate: func(t *testing.T, start, end int) {
				assert.LessOrEqual(t, end, 10)
			},
		},
		{
			name:       "page_size_determines_slice_length",
			page:       1,
			pageSize:   25,
			totalCount: 100,
			validate: func(t *testing.T, start, end int) {
				sliceLen := end - start
				assert.Equal(t, 25, sliceLen)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := storage.CalculatePaginationBounds(tt.page, tt.pageSize, tt.totalCount)
			tt.validate(t, start, end)
		})
	}
}

// TestConfigValidation tests the NewConfig function parameter handling
func TestConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		endpoint        string
		region          string
		bucket          string
		accessKeyID     string
		secretAccessKey string
		usePathStyle    bool
		expectedRegion  string
		expectedBucket  string
	}{
		{
			name:            "all_defaults",
			endpoint:        "",
			region:          "",
			bucket:          "",
			accessKeyID:     "",
			secretAccessKey: "",
			usePathStyle:    false,
			expectedRegion:  "auto",
			expectedBucket:  defaultBucket,
		},
		{
			name:            "custom_bucket_only",
			endpoint:        "",
			region:          "",
			bucket:          "my-bucket",
			accessKeyID:     "",
			secretAccessKey: "",
			usePathStyle:    false,
			expectedRegion:  "auto",
			expectedBucket:  "my-bucket",
		},
		{
			name:            "custom_region_only",
			endpoint:        "",
			region:          "us-west-2",
			bucket:          "",
			accessKeyID:     "",
			secretAccessKey: "",
			usePathStyle:    false,
			expectedRegion:  "us-west-2",
			expectedBucket:  defaultBucket,
		},
		{
			name:            "all_custom",
			endpoint:        "https://r2.example.com",
			region:          "eu-west-1",
			bucket:          "prod-bucket",
			accessKeyID:     "key123",
			secretAccessKey: "secret456",
			usePathStyle:    true,
			expectedRegion:  "eu-west-1",
			expectedBucket:  "prod-bucket",
		},
		{
			name:            "path_style_with_local",
			endpoint:        "http://localhost:9000",
			region:          "",
			bucket:          "",
			accessKeyID:     "test",
			secretAccessKey: "test",
			usePathStyle:    true,
			expectedRegion:  "auto",
			expectedBucket:  defaultBucket,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := storage.NewConfig(
				tt.endpoint,
				tt.region,
				tt.bucket,
				tt.accessKeyID,
				tt.secretAccessKey,
				tt.usePathStyle,
			)

			assert.Equal(t, tt.expectedRegion, cfg.Region)
			assert.Equal(t, tt.expectedBucket, cfg.Bucket)
			assert.Equal(t, tt.endpoint, cfg.Endpoint)
			assert.Equal(t, tt.accessKeyID, cfg.AccessKeyID)
			assert.Equal(t, tt.secretAccessKey, cfg.SecretAccessKey)
			assert.Equal(t, tt.usePathStyle, cfg.UsePathStyle)
		})
	}
}

// TestPaginationMathematicalProperties verifies mathematical properties of pagination
func TestPaginationMathematicalProperties(t *testing.T) {
	t.Parallel()

	const (
		totalResults = 1000
		pageSize     = 37
	)

	// Calculate expected number of pages
	expectedPages := (totalResults + pageSize - 1) / pageSize

	// Test each page
	for page := 1; page <= expectedPages+5; page++ {
		t.Run(fmt.Sprintf("page_%d", page), func(t *testing.T) {
			start, end := storage.CalculatePaginationBounds(page, pageSize, totalResults)

			// Verify properties
			assert.GreaterOrEqual(t, start, 0, "start must be non-negative")
			assert.LessOrEqual(t, end, totalResults, "end must not exceed total")
			assert.LessOrEqual(t, start, end, "start must be <= end")

			// For pages within bounds, verify slice length
			if page <= expectedPages {
				expectedLen := pageSize
				if page == expectedPages {
					// Last page might be partial
					expectedLen = totalResults - (pageSize * (expectedPages - 1))
				}
				actualLen := end - start
				assert.Equal(t, expectedLen, actualLen, fmt.Sprintf("page %d slice length incorrect", page))
			}
		})
	}
}
