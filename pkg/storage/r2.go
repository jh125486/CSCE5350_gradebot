package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
)

const (
	// DefaultPageSize is the default number of results per page when not specified
	DefaultPageSize = 20
	// MaxPageSize is the maximum allowed results per page
	MaxPageSize = 100
)

// ListResultsParams holds pagination parameters for ListResults
type ListResultsParams struct {
	Page     int // 1-indexed page number
	PageSize int // Number of results per page
}

// Storage defines the interface for persistent storage of rubric results
type Storage interface {
	SaveResult(ctx context.Context, submissionID string, result *proto.Result) error
	LoadResult(ctx context.Context, submissionID string) (*proto.Result, error)
	ListResultsPaginated(ctx context.Context, params ListResultsParams) (map[string]*proto.Result, int, error)
}

// Config holds storage configuration
type Config struct {
	// For production R2/Cloudflare
	Endpoint string
	Region   string
	Bucket   string

	// Credentials
	AccessKeyID     string
	SecretAccessKey string

	// Addressing style
	UsePathStyle bool

	// MaxConcurrentFetches limits parallel fetches in ListResultsPaginated.
	// Default is 30 if not set or <= 0.
	MaxConcurrentFetches int
}

// NewConfig creates storage config from provided parameters
func NewConfig(endpoint, region, bucket, accessKeyID, secretAccessKey string, usePathStyle bool) *Config {
	if bucket == "" {
		bucket = "gradebot-storage" // Default bucket name
	}
	if region == "" {
		region = "auto" // Default region
	}
	return &Config{
		Endpoint:        endpoint,
		Region:          region,
		Bucket:          bucket,
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		UsePathStyle:    usePathStyle,
	}
}

// R2Storage implements Storage using Cloudflare R2 (S3-compatible)
type R2Storage struct {
	client               *s3.Client
	bucket               string
	maxConcurrentFetches int
}

// NewR2Storage creates a new R2 storage instance
func NewR2Storage(ctx context.Context, cfg *Config) (*R2Storage, error) {
	// Determine region based on addressing style
	region := cfg.Region
	if cfg.UsePathStyle {
		// LocalStack typically uses us-east-1
		region = "us-east-1"
	}

	if cfg.UsePathStyle {
		slog.Info("Using path-style addressing for storage", "endpoint", cfg.Endpoint, "region", region)
	} else {
		slog.Info("Using virtual-hosted addressing for storage", "endpoint", cfg.Endpoint, "region", region)
	}

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.UsePathStyle
		o.BaseEndpoint = &cfg.Endpoint
	})

	maxConcurrentFetches := cfg.MaxConcurrentFetches
	if maxConcurrentFetches <= 0 {
		maxConcurrentFetches = 30 // Default concurrency limit
	}

	storage := &R2Storage{
		client:               client,
		bucket:               cfg.Bucket,
		maxConcurrentFetches: maxConcurrentFetches,
	}

	// Ensure bucket exists, create if it doesn't
	if err := storage.ensureBucketExists(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

	return storage, nil
}

// SaveResult saves a rubric result to storage
func (r *R2Storage) SaveResult(ctx context.Context, submissionID string, result *proto.Result) error {
	start := time.Now()
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}
	data, err := marshaler.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	key := fmt.Sprintf("submissions/%s.json", submissionID)

	_, err = r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &r.bucket,
		Key:         &key,
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to save result to R2: %w", err)
	}

	slog.Info("Saved rubric result",
		slog.String("submission_id", submissionID),
		slog.String("bucket", r.bucket),
		slog.Duration("duration", time.Since(start)),
	)

	return nil
}

// LoadResult loads a rubric result from storage
func (r *R2Storage) LoadResult(ctx context.Context, submissionID string) (*proto.Result, error) {
	start := time.Now()
	key := fmt.Sprintf("submissions/%s.json", submissionID)

	resp, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &r.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load result from R2: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result proto.Result
	unmarshaler := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	if err := unmarshaler.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to decode result: %w", err)
	}
	slog.Info("Loaded rubric result",
		slog.String("submission_id", submissionID),
		slog.String("bucket", r.bucket),
		slog.Duration("duration", time.Since(start)),
	)

	return &result, nil
}

// ListResultsPaginated loads rubric results with pagination, only fetching the requested page.
// If the requested page exceeds available pages, returns the last page. Page numbers start at 1.
func (r *R2Storage) ListResultsPaginated(
	ctx context.Context,
	params ListResultsParams,
) (results map[string]*proto.Result, totalCount int, err error) {
	start := time.Now()

	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > MaxPageSize {
		params.PageSize = DefaultPageSize
	}

	// Collect all keys from storage
	allKeys, err := r.collectAllKeys(ctx)
	if err != nil {
		return nil, 0, err
	}

	totalCount = len(allKeys)

	// Calculate pagination boundaries
	startIdx, endIdx := calculatePaginationBounds(params.Page, params.PageSize, totalCount)

	// Fetch results for this page in parallel
	pageKeys := allKeys[startIdx:endIdx]
	results = r.loadResultsParallel(ctx, pageKeys)

	slog.Info("Listed paginated rubric results",
		slog.Int("page", params.Page),
		slog.Int("page_size", params.PageSize),
		slog.Int("total_count", totalCount),
		slog.Int("returned", len(results)),
		slog.String("bucket", r.bucket),
		slog.Duration("duration", time.Since(start)),
	)

	return results, totalCount, nil
}

func calculatePaginationBounds(page, pageSize, totalCount int) (startIdx, endIdx int) {
	// Handle empty results
	if totalCount == 0 {
		return 0, 0
	}

	startIdx = (page - 1) * pageSize
	endIdx = startIdx + pageSize

	// If requested page is beyond available pages, clamp to valid range
	if startIdx >= totalCount {
		startIdx = max(totalCount-pageSize, 0)
		endIdx = totalCount
	} else if endIdx > totalCount {
		endIdx = totalCount
	}

	return startIdx, endIdx
}

// collectAllKeys retrieves all submission object keys from storage
func (r *R2Storage) collectAllKeys(ctx context.Context) ([]string, error) {
	var allKeys []string
	paginator := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{
		Bucket: &r.bucket,
		Prefix: aws.String("submissions/"),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}

			key := *obj.Key
			if len(key) < 13 || key[len(key)-5:] != ".json" {
				continue
			}

			allKeys = append(allKeys, key)
		}
	}

	return allKeys, nil
}

// loadResultsParallel fetches multiple results concurrently using errgroup
func (r *R2Storage) loadResultsParallel(ctx context.Context, keys []string) map[string]*proto.Result {
	results := make(map[string]*proto.Result)
	var mu sync.Mutex

	// Create errgroup with context for better error handling and context cancellation
	wg, ctx := errgroup.WithContext(ctx)
	wg.SetLimit(r.maxConcurrentFetches) // Limit concurrent requests based on config

	for _, key := range keys {
		wg.Go(func() error {
			submissionID := key[12 : len(key)-5] // Remove "submissions/" prefix and ".json" suffix
			result, err := r.LoadResult(ctx, submissionID)
			if err != nil {
				slog.Warn("Failed to load result", "submission_id", submissionID, "error", err)
				return nil // Don't fail entire batch on single error
			}

			mu.Lock()
			results[submissionID] = result
			mu.Unlock()

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := wg.Wait(); err != nil {
		slog.Error("Error loading results in parallel", "error", err)
	}

	return results
}

// ensureBucketExists checks if the bucket exists and creates it if it doesn't
func (r *R2Storage) ensureBucketExists(ctx context.Context) error {
	// Try to check if bucket exists by listing objects (HeadBucket might not work with all S3-compatible services)
	_, err := r.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  &r.bucket,
		MaxKeys: aws.Int32(1), // Just check if we can access the bucket
	})
	if err != nil {
		// If bucket doesn't exist, try to create it
		slog.Info("Bucket does not exist, attempting to create", "bucket", r.bucket)

		_, createErr := r.client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: &r.bucket,
		})

		if createErr != nil {
			return fmt.Errorf("failed to create bucket %s: %w", r.bucket, createErr)
		}

		slog.Info("Successfully created bucket", "bucket", r.bucket)
		return nil
	}

	slog.Info("Bucket already exists", "bucket", r.bucket)
	return nil
}
