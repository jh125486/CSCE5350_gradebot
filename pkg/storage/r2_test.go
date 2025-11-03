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

func skipIfNoR2(t *testing.T) {
	if os.Getenv("R2_ENDPOINT") == "" {
		t.Skip("Skipping R2 test: R2_ENDPOINT environment variable not set")
	}
}

func TestNewR2Storage(t *testing.T) {
	skipIfNoR2(t)
	t.Parallel()

	tests := []struct {
		name      string
		cfg       *storage.R2Config
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid_config",
			cfg: &storage.R2Config{
				Endpoint:        os.Getenv("R2_ENDPOINT"),
				Bucket:          "test-bucket-" + strconv.FormatInt(time.Now().Unix(), 10),
				AccessKeyID:     "test",
				SecretAccessKey: "test",
				UsePathStyle:    true,
			},
			wantError: false,
		},
		{
			name: "invalid_endpoint",
			cfg: &storage.R2Config{
				Endpoint:        "http://invalid-url:9999",
				Bucket:          "test-bucket",
				AccessKeyID:     "test",
				SecretAccessKey: "test",
				UsePathStyle:    true,
			},
			wantError: true,
			errorMsg:  "failed to ensure bucket exists",
		},
		{
			name: "virtual_hosted_style",
			cfg: &storage.R2Config{
				Endpoint:        os.Getenv("R2_ENDPOINT"),
				Bucket:          "test-vhost-" + strconv.FormatInt(time.Now().Unix(), 10),
				AccessKeyID:     "test",
				SecretAccessKey: "test",
				UsePathStyle:    false,
			},
			// Virtual hosting works with localhost but fails with custom hostnames like 'localstack'
			wantError: strings.Contains(os.Getenv("R2_ENDPOINT"), "localhost") == false,
			errorMsg:  "failed to ensure bucket exists",
		},
		{
			name: "empty_credentials",
			cfg: &storage.R2Config{
				Endpoint:        os.Getenv("R2_ENDPOINT"),
				Bucket:          "test-empty-creds-" + strconv.FormatInt(time.Now().Unix(), 10),
				AccessKeyID:     "",
				SecretAccessKey: "",
				UsePathStyle:    true,
			},
			wantError: true, // Empty credentials should fail
			errorMsg:  "static credentials are empty",
		},
		{
			name: "custom_region",
			cfg: &storage.R2Config{
				Endpoint:        os.Getenv("R2_ENDPOINT"),
				Region:          "eu-west-1",
				Bucket:          "test-custom-region-" + strconv.FormatInt(time.Now().Unix(), 10),
				AccessKeyID:     "test",
				SecretAccessKey: "test",
				UsePathStyle:    true,
			},
			wantError: false,
		},
		{
			name: "bucket_already_exists",
			cfg: &storage.R2Config{
				Endpoint:        os.Getenv("R2_ENDPOINT"),
				Bucket:          "test-existing-bucket-" + strconv.FormatInt(time.Now().Unix(), 10),
				AccessKeyID:     "test",
				SecretAccessKey: "test",
				UsePathStyle:    true,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.name == "valid_config" {
				skipIfNoR2(t)
			}

			s, err := storage.NewR2Storage(context.Background(), tt.cfg)

			if tt.wantError {
				require.Error(t, err)
				require.Nil(t, s)
				require.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				require.NotNil(t, s)

				// Special case: test bucket already exists path
				if tt.name == "bucket_already_exists" {
					// Create another storage instance with the same bucket
					s2, err2 := storage.NewR2Storage(context.Background(), tt.cfg)
					require.NoError(t, err2, "Second storage instance with same bucket should succeed")
					require.NotNil(t, s2)
				}
			}
		})
	}
}

func TestR2StorageSaveResult(t *testing.T) {
	skipIfNoR2(t)
	t.Parallel()

	tests := []struct {
		name         string
		submissionID string
		result       *proto.Result
		wantError    bool
		errorMsg     string
	}{
		{
			name:         "valid_result",
			submissionID: "test-123",
			result: &proto.Result{
				SubmissionId: "test-123",
				Timestamp:    time.Now().Format(time.RFC3339),
				Rubric: []*proto.RubricItem{
					{
						Name:    "Code Quality",
						Note:    "Good structure",
						Points:  10.0,
						Awarded: 8.5,
					},
				},
				IpAddress:   "127.0.0.1",
				GeoLocation: "Local/Unknown",
			},
			wantError: false,
		},
		{
			name:         "empty_submission_id",
			submissionID: "",
			result: &proto.Result{
				SubmissionId: "",
				Timestamp:    time.Now().Format(time.RFC3339),
				Rubric: []*proto.RubricItem{
					{Name: "Test", Points: 5.0, Awarded: 4.0, Note: "OK"},
				},
			},
			wantError: true,
			errorMsg:  "submission ID is required",
		},
		{
			name:         "nil_result",
			submissionID: "test-nil",
			result:       nil,
			wantError:    true,
			errorMsg:     "result cannot be nil",
		},
		{
			name:         "large_result",
			submissionID: "test-large",
			result: func() *proto.Result {
				rubricItems := make([]*proto.RubricItem, 50)
				for i := range 50 {
					rubricItems[i] = &proto.RubricItem{
						Name:    fmt.Sprintf("Item_%d", i),
						Note:    fmt.Sprintf("Note for item %d", i),
						Points:  float64(i + 1),
						Awarded: float64(i) * 0.8,
					}
				}
				return &proto.Result{
					SubmissionId: "test-large",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       rubricItems,
				}
			}(),
			wantError: false,
		},
		{
			name:         "result_with_special_characters",
			submissionID: "test-special-chars",
			result: &proto.Result{
				SubmissionId: "test-special-chars",
				Timestamp:    time.Now().Format(time.RFC3339),
				Rubric: []*proto.RubricItem{
					{
						Name:    "Test with Unicode: 测试 🚀 <script>alert('xss')</script>",
						Note:    "Special chars: \n\t\"quotes\" & 'apostrophes'",
						Points:  10.0,
						Awarded: 8.0,
					},
				},
				IpAddress:   "2001:db8::1",
				GeoLocation: "Test/Unicode-地点",
			},
			wantError: false,
		},
		{
			name:         "result_with_all_fields_populated",
			submissionID: "test-all-fields",
			result: &proto.Result{
				SubmissionId: "test-all-fields",
				Timestamp:    time.Now().Format(time.RFC3339),
				Rubric: []*proto.RubricItem{
					{
						Name:    "Comprehensive Test",
						Note:    "Testing all protobuf fields",
						Points:  100.0,
						Awarded: 95.5,
					},
				},
				IpAddress:   "192.168.1.100",
				GeoLocation: "TestCity/TestCountry",
			},
			wantError: false,
		},
		{
			name:         "result_with_zero_values",
			submissionID: "test-zeros",
			result: &proto.Result{
				SubmissionId: "test-zeros",
				Timestamp:    "",
				Rubric: []*proto.RubricItem{
					{
						Name:    "",
						Note:    "",
						Points:  0.0,
						Awarded: 0.0,
					},
				},
				IpAddress:   "",
				GeoLocation: "",
			},
			wantError: false,
		},
		{
			name:         "result_with_negative_values",
			submissionID: "test-negative",
			result: &proto.Result{
				SubmissionId: "test-negative",
				Timestamp:    time.Now().Format(time.RFC3339),
				Rubric: []*proto.RubricItem{
					{
						Name:    "Negative Test",
						Note:    "Testing negative values",
						Points:  -10.0,
						Awarded: -5.5,
					},
				},
			},
			wantError: false,
		},
		{
			name:         "result_with_extremely_large_data",
			submissionID: "test-huge-data",
			result: &proto.Result{
				SubmissionId: "test-huge-data",
				Timestamp:    time.Now().Format(time.RFC3339),
				Rubric: func() []*proto.RubricItem {
					// Create a massive rubric with many items
					items := make([]*proto.RubricItem, 100)
					for i := range 100 {
						items[i] = &proto.RubricItem{
							Name:    fmt.Sprintf("Massive Test Item #%d: %s", i, strings.Repeat("Long name ", 20)),
							Note:    fmt.Sprintf("Massive note for item %d: %s", i, strings.Repeat("This is a very detailed note that tests large data handling. ", 50)),
							Points:  float64(i + 1),
							Awarded: float64(i) * 0.85,
						}
					}
					return items
				}(),
				IpAddress:   strings.Repeat("192.168.1.255, ", 100),
				GeoLocation: strings.Repeat("Country/State/City/District/Street/Building/", 20),
			},
			wantError: false,
		},
		{
			name:         "result_with_unicode_edge_cases",
			submissionID: "test-unicode-edge",
			result: &proto.Result{
				SubmissionId: "test-unicode-edge",
				Timestamp:    time.Now().Format(time.RFC3339),
				Rubric: []*proto.RubricItem{
					{
						Name:    "Unicode Edge Cases: 🚀🌟✨💡🔥⚡🌈🎯 中文测试 العربية русский עברית 🇺🇸🇨🇳🇯🇵",
						Note:    "Testing various Unicode: \u0000\u0001\u0002 NULL bytes, \uFEFF BOM, \u200B ZWSP",
						Points:  42.42,
						Awarded: 39.99,
					},
				},
				IpAddress:   "2001:0db8:85a3:0000:0000:8a2e:0370:7334", // IPv6
				GeoLocation: "🌍Global/🌎International/🌏Worldwide",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &storage.R2Config{
				Endpoint:        os.Getenv("R2_ENDPOINT"),
				Bucket:          strings.ReplaceAll("test-save-"+tt.name+"-"+strconv.FormatInt(time.Now().UnixNano(), 36), "_", "-"),
				AccessKeyID:     "test",
				SecretAccessKey: "test",
				UsePathStyle:    true,
			}

			s, err := storage.NewR2Storage(context.Background(), cfg)
			require.NoError(t, err)

			err = s.SaveResult(context.Background(), tt.result)

			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestR2StorageLoadResult(t *testing.T) {
	skipIfNoR2(t)
	t.Parallel()

	cfg := &storage.R2Config{
		Endpoint:        os.Getenv("R2_ENDPOINT"),
		Bucket:          "test-load-result-" + strconv.FormatInt(time.Now().UnixNano(), 36),
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		UsePathStyle:    true,
	}

	s, err := storage.NewR2Storage(context.Background(), cfg)
	require.NoError(t, err)

	// First, save a test result
	testResult := &proto.Result{
		SubmissionId: "saved-test",
		Timestamp:    time.Now().Format(time.RFC3339),
		Rubric: []*proto.RubricItem{
			{
				Name:    "Test Item",
				Note:    "Test note",
				Points:  10.0,
				Awarded: 8.0,
			},
		},
		IpAddress:   "192.168.1.1",
		GeoLocation: "Test/Location",
	}

	err = s.SaveResult(context.Background(), testResult)
	require.NoError(t, err)

	tests := []struct {
		name         string
		submissionID string
		wantError    bool
		errorMsg     string
		validate     func(t *testing.T, result *proto.Result)
	}{
		{
			name:         "existing_result",
			submissionID: "saved-test",
			wantError:    false,
			validate: func(t *testing.T, result *proto.Result) {
				assert.Equal(t, "saved-test", result.SubmissionId)
				assert.Len(t, result.Rubric, 1)
				assert.Equal(t, "Test Item", result.Rubric[0].Name)
				assert.Equal(t, 8.0, result.Rubric[0].Awarded)
			},
		},
		{
			name:         "nonexistent_result",
			submissionID: "does-not-exist",
			wantError:    true,
			errorMsg:     "failed to load result from R2",
		},
		{
			name:         "empty_submission_id",
			submissionID: "",
			wantError:    true,
			errorMsg:     "failed to load result from R2",
		},
		{
			name:         "submission_id_with_special_chars",
			submissionID: "test/with/slashes",
			wantError:    true,
			errorMsg:     "failed to load result from R2",
		},
		{
			name:         "very_long_submission_id",
			submissionID: strings.Repeat("a", 300),
			wantError:    true,
			errorMsg:     "failed to load result from R2",
		},
		{
			name:         "submission_id_with_dots",
			submissionID: "test.with.dots",
			wantError:    true,
			errorMsg:     "failed to load result from R2",
		},
		{
			name:         "submission_id_with_spaces",
			submissionID: "test with spaces",
			wantError:    true,
			errorMsg:     "failed to load result from R2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := s.LoadResult(context.Background(), tt.submissionID)

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, result)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

func TestCalculatePaginationBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		page       int
		pageSize   int
		totalCount int
		wantStart  int
		wantEnd    int
	}{
		{
			name:       "first_page",
			page:       1,
			pageSize:   10,
			totalCount: 100,
			wantStart:  0,
			wantEnd:    10,
		},
		{
			name:       "middle_page",
			page:       5,
			pageSize:   10,
			totalCount: 100,
			wantStart:  40,
			wantEnd:    50,
		},
		{
			name:       "last_page_full",
			page:       10,
			pageSize:   10,
			totalCount: 100,
			wantStart:  90,
			wantEnd:    100,
		},
		{
			name:       "last_page_partial",
			page:       11,
			pageSize:   10,
			totalCount: 105,
			wantStart:  100,
			wantEnd:    105,
		},
		{
			name:       "page_beyond_total",
			page:       20,
			pageSize:   10,
			totalCount: 50,
			wantStart:  40,
			wantEnd:    50,
		},
		{
			name:       "empty_results",
			page:       1,
			pageSize:   10,
			totalCount: 0,
			wantStart:  0,
			wantEnd:    0,
		},
		{
			name:       "single_item",
			page:       1,
			pageSize:   10,
			totalCount: 1,
			wantStart:  0,
			wantEnd:    1,
		},
		{
			name:       "page_size_one",
			page:       1,
			pageSize:   1,
			totalCount: 10,
			wantStart:  0,
			wantEnd:    1,
		},
		{
			name:       "page_size_larger_than_total",
			page:       1,
			pageSize:   1000,
			totalCount: 50,
			wantStart:  0,
			wantEnd:    50,
		},
		{
			name:       "page_2_small_page_size",
			page:       2,
			pageSize:   5,
			totalCount: 12,
			wantStart:  5,
			wantEnd:    10,
		},
		{
			name:       "large_page_size",
			page:       1,
			pageSize:   1000,
			totalCount: 525,
			wantStart:  0,
			wantEnd:    525,
		},
		{
			name:       "exactly_at_page_boundary",
			page:       3,
			pageSize:   35,
			totalCount: 105,
			wantStart:  70,
			wantEnd:    105,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := storage.ListResultsParams{
				Page:     tt.page,
				PageSize: tt.pageSize,
			}
			start, end := params.CalculatePaginationBounds(tt.totalCount)
			assert.Equal(t, tt.wantStart, start, "Start index mismatch")
			assert.Equal(t, tt.wantEnd, end, "End index mismatch")
		})
	}
}

func TestR2StorageListResultsPaginated(t *testing.T) {
	skipIfNoR2(t)
	t.Parallel()

	ctx := context.Background()

	// Create unique bucket for this test
	cfg := &storage.R2Config{
		Endpoint:        os.Getenv("R2_ENDPOINT"),
		Bucket:          "test-list-" + strconv.FormatInt(time.Now().Unix(), 10),
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		UsePathStyle:    true,
	}

	store, err := storage.NewR2Storage(ctx, cfg)
	require.NoError(t, err)

	// Save multiple results
	for i := 1; i <= 5; i++ {
		result := &proto.Result{
			SubmissionId: fmt.Sprintf("test-sub-%d", i),
			Project:      "TestProject",
			Timestamp:    time.Now().Format(time.RFC3339),
			Rubric: []*proto.RubricItem{
				{Name: "Test", Points: 10, Awarded: float64(i), Note: fmt.Sprintf("Note %d", i)},
			},
		}
		err := store.SaveResult(ctx, result)
		require.NoError(t, err)
	}

	tests := []struct {
		name         string
		page         int
		pageSize     int
		wantCount    int
		wantTotalMin int
	}{
		{
			name:         "first_page",
			page:         1,
			pageSize:     2,
			wantCount:    2,
			wantTotalMin: 5,
		},
		{
			name:         "second_page",
			page:         2,
			pageSize:     2,
			wantCount:    2,
			wantTotalMin: 5,
		},
		{
			name:         "last_page",
			page:         3,
			pageSize:     2,
			wantCount:    1,
			wantTotalMin: 5,
		},
		{
			name:         "large_page_size",
			page:         1,
			pageSize:     100,
			wantCount:    5,
			wantTotalMin: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := storage.ListResultsParams{
				Page:     tt.page,
				PageSize: tt.pageSize,
			}

			results, totalCount, err := store.ListResultsPaginated(ctx, params)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, totalCount, tt.wantTotalMin, "Total count should be at least the minimum")
			assert.Equal(t, tt.wantCount, len(results), "Result count mismatch")
		})
	}
}
