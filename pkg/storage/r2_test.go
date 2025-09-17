package storage

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
)

func TestR2Storage_LocalStack(t *testing.T) {
	// Skip if LocalStack is not available
	if os.Getenv("SKIP_LOCALSTACK_TESTS") == "true" {
		t.Skip("Skipping LocalStack tests")
	}

	// Configure for LocalStack
	endpoint := os.Getenv("R2_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:4566"
	}
	cfg := &Config{
		Endpoint:        endpoint,
		Bucket:          "test-gradebot-bucket",
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		UsePathStyle:    true,
	}

	storage, err := NewR2Storage(t.Context(), cfg)
	require.NoError(t, err)
	defer storage.Close()

	ctx := context.Background()

	// Create a test result
	testResult := &proto.Result{
		SubmissionId: "test-submission-123",
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
	}

	// Test SaveResult
	err = storage.SaveResult(ctx, testResult.SubmissionId, testResult)
	assert.NoError(t, err)

	// Test LoadResult
	loadedResult, err := storage.LoadResult(ctx, testResult.SubmissionId)
	assert.NoError(t, err)
	assert.Equal(t, testResult.SubmissionId, loadedResult.SubmissionId)
	assert.Equal(t, testResult.Rubric[0].Name, loadedResult.Rubric[0].Name)
	assert.Equal(t, testResult.Rubric[0].Awarded, loadedResult.Rubric[0].Awarded)

	// Test ListResults
	results, err := storage.ListResults(ctx)
	assert.NoError(t, err)
	assert.Contains(t, results, testResult.SubmissionId)
	assert.Equal(t, testResult.SubmissionId, results[testResult.SubmissionId].SubmissionId)
}

func TestR2Storage_ProductionConfig(t *testing.T) {
	// Test configuration loading from environment
	t.Run("LocalStack config", func(t *testing.T) {
		os.Setenv("R2_ENDPOINT", "http://localhost:4566")
		os.Setenv("R2_BUCKET", "test-bucket")
		os.Setenv("AWS_ACCESS_KEY_ID", "test-key")
		os.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
		os.Setenv("USE_PATH_STYLE", "true")
		defer func() {
			os.Unsetenv("R2_ENDPOINT")
			os.Unsetenv("R2_BUCKET")
			os.Unsetenv("AWS_ACCESS_KEY_ID")
			os.Unsetenv("AWS_SECRET_ACCESS_KEY")
			os.Unsetenv("USE_PATH_STYLE")
		}()

		cfg := NewConfig()
		assert.True(t, cfg.UsePathStyle)
		assert.Equal(t, "http://localhost:4566", cfg.Endpoint)
		assert.Equal(t, "test-bucket", cfg.Bucket)
		assert.Equal(t, "test-key", cfg.AccessKeyID)
		assert.Equal(t, "test-secret", cfg.SecretAccessKey)
	})

	t.Run("Production config", func(t *testing.T) {
		os.Setenv("R2_ENDPOINT", "https://test.r2.cloudflarestorage.com")
		os.Setenv("AWS_REGION", "auto")
		os.Setenv("R2_BUCKET", "prod-bucket")
		os.Setenv("AWS_ACCESS_KEY_ID", "prod-key")
		os.Setenv("AWS_SECRET_ACCESS_KEY", "prod-secret")
		// Don't set USE_PATH_STYLE - should default to false
		defer func() {
			os.Unsetenv("R2_ENDPOINT")
			os.Unsetenv("AWS_REGION")
			os.Unsetenv("R2_BUCKET")
			os.Unsetenv("AWS_ACCESS_KEY_ID")
			os.Unsetenv("AWS_SECRET_ACCESS_KEY")
		}()

		cfg := NewConfig()
		assert.False(t, cfg.UsePathStyle)
		assert.Equal(t, "https://test.r2.cloudflarestorage.com", cfg.Endpoint)
		assert.Equal(t, "auto", cfg.Region)
		assert.Equal(t, "prod-bucket", cfg.Bucket)
		assert.Equal(t, "prod-key", cfg.AccessKeyID)
		assert.Equal(t, "prod-secret", cfg.SecretAccessKey)
	})
}

func TestR2Storage_ErrorHandling(t *testing.T) {
	// Test with invalid endpoint
	cfg := &Config{
		Endpoint:        "http://invalid-url:9999",
		Bucket:          "test-bucket",
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		UsePathStyle:    true,
	}

	// Now that we create buckets in NewR2Storage, this should fail
	storage, err := NewR2Storage(t.Context(), cfg)
	require.Error(t, err) // Should fail during bucket creation
	require.Nil(t, storage)
	require.Contains(t, err.Error(), "failed to ensure bucket exists")
}

func TestR2Storage_BucketCreation(t *testing.T) {
	// Skip if LocalStack is not available
	if os.Getenv("SKIP_LOCALSTACK_TESTS") == "true" {
		t.Skip("Skipping LocalStack tests")
	}

	// Configure for LocalStack
	endpoint := os.Getenv("R2_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:4566"
	}
	cfg := &Config{
		Endpoint:        endpoint,
		Bucket:          "test-new-bucket-" + strconv.FormatInt(time.Now().Unix(), 10), // Use unique name
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		UsePathStyle:    true,
	}

	storage, err := NewR2Storage(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, storage)

	// Test that we can save to the newly created bucket
	testResult := &proto.Result{
		SubmissionId: "bucket-creation-test",
		Timestamp:    time.Now().Format(time.RFC3339),
		Rubric: []*proto.RubricItem{
			{Name: "Test", Points: 10.0, Awarded: 8.0, Note: "Good"},
		},
	}

	err = storage.SaveResult(t.Context(), testResult.SubmissionId, testResult)
	require.NoError(t, err)

	// Test that we can load from the newly created bucket
	loaded, err := storage.LoadResult(t.Context(), testResult.SubmissionId)
	require.NoError(t, err)
	require.Equal(t, testResult.SubmissionId, loaded.SubmissionId)
}
