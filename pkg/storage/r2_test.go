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

func skipIfNoEndpoint(t *testing.T) {
	if os.Getenv("R2_ENDPOINT") == "" {
		t.Skip("Skipping R2 test: R2_ENDPOINT environment variable not set")
	}
}

func TestNewConfig(t *testing.T) {
	skipIfNoEndpoint(t)
	t.Parallel()

	tests := []struct {
		name            string
		endpoint        string
		region          string
		bucket          string
		accessKeyID     string
		secretAccessKey string
		usePathStyle    bool
		expectedBucket  string
		expectedRegion  string
	}{
		{
			name:            "full_config",
			endpoint:        "http://localhost:4566",
			region:          "us-east-1",
			bucket:          "test-bucket",
			accessKeyID:     "test-key",
			secretAccessKey: "test-secret",
			usePathStyle:    true,
			expectedBucket:  "test-bucket",
			expectedRegion:  "us-east-1",
		},
		{
			name:            "empty_bucket_uses_default",
			endpoint:        "http://localhost:4566",
			region:          "us-west-2",
			bucket:          "",
			accessKeyID:     "test-key",
			secretAccessKey: "test-secret",
			usePathStyle:    false,
			expectedBucket:  "gradebot-storage",
			expectedRegion:  "us-west-2",
		},
		{
			name:            "empty_region_uses_default",
			endpoint:        "https://r2.example.com",
			region:          "",
			bucket:          "my-bucket",
			accessKeyID:     "prod-key",
			secretAccessKey: "prod-secret",
			usePathStyle:    false,
			expectedBucket:  "my-bucket",
			expectedRegion:  "auto",
		},
		{
			name:            "minimal_config",
			endpoint:        "",
			region:          "",
			bucket:          "",
			accessKeyID:     "",
			secretAccessKey: "",
			usePathStyle:    false,
			expectedBucket:  "gradebot-storage",
			expectedRegion:  "auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := storage.NewConfig(tt.endpoint, tt.region, tt.bucket, tt.accessKeyID, tt.secretAccessKey, tt.usePathStyle)

			assert.Equal(t, tt.endpoint, cfg.Endpoint)
			assert.Equal(t, tt.expectedRegion, cfg.Region)
			assert.Equal(t, tt.expectedBucket, cfg.Bucket)
			assert.Equal(t, tt.accessKeyID, cfg.AccessKeyID)
			assert.Equal(t, tt.secretAccessKey, cfg.SecretAccessKey)
			assert.Equal(t, tt.usePathStyle, cfg.UsePathStyle)
		})
	}
}

func TestNewR2Storage(t *testing.T) {
	skipIfNoEndpoint(t)
	t.Parallel()

	tests := []struct {
		name      string
		cfg       *storage.Config
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid_config",
			cfg: &storage.Config{
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
			cfg: &storage.Config{
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
			cfg: &storage.Config{
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
			cfg: &storage.Config{
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
			cfg: &storage.Config{
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
			cfg: &storage.Config{
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
				skipIfNoEndpoint(t)
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

func TestR2Storage_SaveResult(t *testing.T) {
	skipIfNoEndpoint(t)
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
			wantError: false,
		},
		{
			name:         "nil_result",
			submissionID: "test-nil",
			result:       nil,
			wantError:    false, // Should not panic
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

			cfg := &storage.Config{
				Endpoint:        os.Getenv("R2_ENDPOINT"),
				Bucket:          strings.ReplaceAll("test-save-"+tt.name+"-"+strconv.FormatInt(time.Now().UnixNano(), 36), "_", "-"),
				AccessKeyID:     "test",
				SecretAccessKey: "test",
				UsePathStyle:    true,
			}

			s, err := storage.NewR2Storage(context.Background(), cfg)
			require.NoError(t, err)

			err = s.SaveResult(context.Background(), tt.submissionID, tt.result)

			if tt.wantError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestR2Storage_LoadResult(t *testing.T) {
	skipIfNoEndpoint(t)
	t.Parallel()

	cfg := &storage.Config{
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

	err = s.SaveResult(context.Background(), testResult.SubmissionId, testResult)
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

func TestR2Storage_ListResults(t *testing.T) {
	skipIfNoEndpoint(t)
	t.Parallel()

	tests := []struct {
		name         string
		setupFunc    func(context.Context, storage.Storage) error
		expectedKeys []string
		minCount     int
		maxCount     int
	}{
		{
			name:         "empty_bucket",
			setupFunc:    nil,
			expectedKeys: []string{},
			minCount:     0,
			maxCount:     0,
		},
		{
			name: "single_result",
			setupFunc: func(ctx context.Context, s storage.Storage) error {
				return s.SaveResult(ctx, "single-test", &proto.Result{
					SubmissionId: "single-test",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*proto.RubricItem{
						{Name: "Test", Points: 5.0, Awarded: 4.0},
					},
				})
			},
			expectedKeys: []string{"single-test"},
			minCount:     1,
			maxCount:     1,
		},
		{
			name: "multiple_results",
			setupFunc: func(ctx context.Context, s storage.Storage) error {
				results := []string{"multi-test-1", "multi-test-2", "multi-test-3"}
				for _, id := range results {
					err := s.SaveResult(ctx, id, &proto.Result{
						SubmissionId: id,
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric: []*proto.RubricItem{
							{Name: "Test", Points: 5.0, Awarded: 4.0},
						},
					})
					if err != nil {
						return err
					}
				}
				return nil
			},
			expectedKeys: []string{"multi-test-1", "multi-test-2", "multi-test-3"},
			minCount:     3,
			maxCount:     3,
		},
		{
			name: "results_with_special_characters",
			setupFunc: func(ctx context.Context, s storage.Storage) error {
				specialIds := []string{
					"test-with-unicode-测试",
					"test-with-emoji-🚀",
					"test-with-numbers-123456",
				}
				for _, id := range specialIds {
					err := s.SaveResult(ctx, id, &proto.Result{
						SubmissionId: id,
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric: []*proto.RubricItem{
							{Name: "Special Test", Points: 10.0, Awarded: 9.0},
						},
					})
					if err != nil {
						return err
					}
				}
				return nil
			},
			expectedKeys: []string{"test-with-unicode-测试", "test-with-emoji-🚀", "test-with-numbers-123456"},
			minCount:     3,
			maxCount:     3,
		},
		{
			name: "mixed_results_with_empty_rubrics",
			setupFunc: func(ctx context.Context, s storage.Storage) error {
				// Save results with different rubric states
				testCases := []struct {
					id     string
					rubric []*proto.RubricItem
				}{
					{"empty-rubric", []*proto.RubricItem{}},
					{"single-item", []*proto.RubricItem{{Name: "Test", Points: 5.0, Awarded: 3.0}}},
					{"multiple-items", []*proto.RubricItem{
						{Name: "Test1", Points: 5.0, Awarded: 4.0},
						{Name: "Test2", Points: 10.0, Awarded: 8.0},
					}},
				}

				for _, tc := range testCases {
					err := s.SaveResult(ctx, tc.id, &proto.Result{
						SubmissionId: tc.id,
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       tc.rubric,
					})
					if err != nil {
						return err
					}
				}
				return nil
			},
			expectedKeys: []string{"empty-rubric", "single-item", "multiple-items"},
			minCount:     3,
			maxCount:     3,
		},
		{
			name: "results_with_edge_case_submission_ids",
			setupFunc: func(ctx context.Context, s storage.Storage) error {
				// Test edge cases for submission IDs that might affect key parsing
				edgeIds := []string{
					"a",                     // Very short
					"test-with-dashes",      // Dashes
					"test_with_underscores", // Underscores
					"test123numbers",        // Numbers
					"TestWithCamelCase",     // Mixed case
				}

				for _, id := range edgeIds {
					err := s.SaveResult(ctx, id, &proto.Result{
						SubmissionId: id,
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric: []*proto.RubricItem{
							{Name: "Edge Test", Points: 5.0, Awarded: 4.0},
						},
					})
					if err != nil {
						return err
					}
				}
				return nil
			},
			expectedKeys: []string{"a", "test-with-dashes", "test_with_underscores", "test123numbers", "TestWithCamelCase"},
			minCount:     5,
			maxCount:     5,
		},
		{
			name: "large_number_of_results",
			setupFunc: func(ctx context.Context, s storage.Storage) error {
				// Create many results to test pagination and performance
				for i := range 25 {
					id := fmt.Sprintf("bulk-test-%03d", i)
					err := s.SaveResult(ctx, id, &proto.Result{
						SubmissionId: id,
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric: []*proto.RubricItem{
							{Name: fmt.Sprintf("Bulk Test %d", i), Points: 10.0, Awarded: float64(i % 10)},
						},
					})
					if err != nil {
						return err
					}
				}
				return nil
			},
			expectedKeys: func() []string {
				var keys []string
				for i := range 25 {
					keys = append(keys, fmt.Sprintf("bulk-test-%03d", i))
				}
				return keys
			}(),
			minCount: 25,
			maxCount: 25,
		},
		{
			name: "results_with_very_short_ids",
			setupFunc: func(ctx context.Context, s storage.Storage) error {
				// Test edge cases for very short IDs that might affect key parsing
				shortIds := []string{"1", "22", "333"}

				for _, id := range shortIds {
					err := s.SaveResult(ctx, id, &proto.Result{
						SubmissionId: id,
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric: []*proto.RubricItem{
							{Name: "Short ID Test", Points: 5.0, Awarded: 4.0},
						},
					})
					if err != nil {
						return err
					}
				}
				return nil
			},
			expectedKeys: []string{"1", "22", "333"},
			minCount:     3,
			maxCount:     3,
		},
		{
			name: "results_with_extremely_large_data",
			setupFunc: func(ctx context.Context, s storage.Storage) error {
				// Create a result with very large data that might test marshal limits
				largeNote := strings.Repeat("This is a very long note that tests the marshaling and storage limits. ", 1000)
				largeName := strings.Repeat("VeryLongTestName", 50)

				err := s.SaveResult(ctx, "large-data-test", &proto.Result{
					SubmissionId: "large-data-test",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*proto.RubricItem{
						{
							Name:    largeName,
							Note:    largeNote,
							Points:  100.0,
							Awarded: 95.5,
						},
					},
					IpAddress:   strings.Repeat("192.168.1.1, ", 100), // Very long IP list
					GeoLocation: strings.Repeat("Very/Long/Location/Path/", 50),
				})
				return err
			},
			expectedKeys: []string{"large-data-test"},
			minCount:     1,
			maxCount:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a unique bucket for each test case
			cfg := &storage.Config{
				Endpoint:        os.Getenv("R2_ENDPOINT"),
				Bucket:          strings.ReplaceAll("test-list-"+tt.name+"-"+strconv.FormatInt(time.Now().UnixNano(), 36), "_", "-"),
				AccessKeyID:     "test",
				SecretAccessKey: "test",
				UsePathStyle:    true,
			}

			s, err := storage.NewR2Storage(context.Background(), cfg)
			require.NoError(t, err)

			ctx := context.Background()

			if tt.setupFunc != nil {
				err := tt.setupFunc(ctx, s)
				require.NoError(t, err)
			}

			results, err := s.ListResults(ctx)
			assert.NoError(t, err)
			assert.NotNil(t, results)

			// Check count is within expected range
			assert.GreaterOrEqual(t, len(results), tt.minCount)
			assert.LessOrEqual(t, len(results), tt.maxCount)

			// Check all expected keys are present
			for _, key := range tt.expectedKeys {
				assert.Contains(t, results, key)
				assert.Equal(t, key, results[key].SubmissionId)
			}
		})
	}
}
