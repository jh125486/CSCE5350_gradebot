package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
)

// Storage defines the interface for persistent storage of rubric results
type Storage interface {
	SaveResult(ctx context.Context, submissionID string, result *proto.Result) error
	LoadResult(ctx context.Context, submissionID string) (*proto.Result, error)
	ListResults(ctx context.Context) (map[string]*proto.Result, error)
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
	client *s3.Client
	bucket string
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

	storage := &R2Storage{
		client: client,
		bucket: cfg.Bucket,
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

// ListResults loads all rubric results from storage
func (r *R2Storage) ListResults(ctx context.Context) (map[string]*proto.Result, error) {
	start := time.Now()
	results := make(map[string]*proto.Result)

	// List all objects with submissions/ prefix
	resp, err := r.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &r.bucket,
		Prefix: aws.String("submissions/"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	for _, obj := range resp.Contents {
		if obj.Key == nil {
			continue
		}

		// Extract submission ID from key (submissions/{id}.json)
		key := *obj.Key
		if len(key) < 13 || key[len(key)-5:] != ".json" {
			continue
		}

		submissionID := key[12 : len(key)-5] // Remove "submissions/" prefix and ".json" suffix

		result, err := r.LoadResult(ctx, submissionID)
		if err != nil {
			slog.Warn("Failed to load result", "submission_id", submissionID, "error", err)
			continue
		}

		results[submissionID] = result
	}
	slog.Info("Listed rubric results",
		slog.Int("count", len(results)),
		slog.String("bucket", r.bucket),
		slog.Duration("duration", time.Since(start)),
	)

	return results, nil
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
