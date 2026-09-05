package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/s3control"
)

// S3Adapter implements S3-compatible storage with intelligent tiering
type S3Adapter struct {
	client       *s3.Client
	clientControl *s3control.Client
	bucket       string
	prefix       string
	region       string
	endpoint     string
	ssl          bool
	tiering      string
}

// NewS3Adapter creates a new S3-compatible storage adapter
func NewS3Adapter(cfgMap map[string]string) (*S3Adapter, error) {
	bucket := cfgMap["bucket"]
	if bucket == "" {
		return nil, fmt.Errorf("bucket name is required for S3 storage")
	}

	prefix := cfgMap["prefix"]
	region := cfgMap["region"]
	if region == "" {
		region = "us-east-1"
	}

	endpoint := cfgMap["endpoint"]
	ssl := cfgMap["ssl"] != "false" // default to true
	tiering := cfgMap["tiering"]
	if tiering == "" {
		tiering = "STANDARD"
	}

	var cfg aws.Config
	var err error

	// Check for custom endpoint (MinIO, DigitalOcean, etc.)
	if endpoint != "" {
		// Custom endpoint configuration
		httpClient := &http.Client{
			Timeout: 30 * time.Second,
		}
		resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, opts ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{
				URL:               endpoint,
				HostnameImmutable: true,
			}, nil
		})

		cfg, err = config.LoadDefaultConfig(
			context.Background(),
			config.WithHTTPClient(httpClient),
			config.WithEndpointResolverWithOptions(resolver),
			config.WithRegion(region),
		)
	} else if cfgMap["access_key"] != "" && cfgMap["secret_key"] != "" {
		// Explicit credentials
		cfg, err = config.LoadDefaultConfig(context.Background(),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				cfgMap["access_key"],
				cfgMap["secret_key"],
				cfgMap["session_token"],
			)),
			config.WithRegion(region),
		)
	} else {
		// Use default credentials chain
		cfg, err = config.LoadDefaultConfig(context.Background())
	}

	if err != nil {
		return nil, fmt.Errorf("failed to configure AWS: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	clientControl := s3control.NewFromConfig(cfg)

	return &S3Adapter{
		client:        client,
		clientControl: clientControl,
		bucket:        bucket,
		prefix:        strings.TrimPrefix(prefix, "/"),
		region:        region,
		endpoint:      endpoint,
		ssl:           ssl,
		tiering:       tiering,
	}, nil
}

// Connect establishes connection to S3
func (a *S3Adapter) Connect() error {
	// Test connection
	_, err := a.client.ListBuckets(context.Background(), &s3.ListBucketsInput{})
	return err
}

// Disconnect closes connections
func (a *S3Adapter) Disconnect() error {
	return nil
}

// getStoragePath constructs the S3 key for an artifact
func (a *S3Adapter) getStoragePath(registryID, namespace, artifactName, version string) string {
	path := fmt.Sprintf("%s/%s/%s/%s/%s", registryID, namespace, artifactName, version, "artifact.bin")
	if a.prefix != "" {
		path = fmt.Sprintf("%s/%s", a.prefix, path)
	}
	return path
}

// SaveArtifact saves an artifact to S3 with intelligent tiering
func (a *S3Adapter) SaveArtifact(registryID, namespace, artifactName, version string, data []byte) (string, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	contentType := "application/octet-stream"
	if isTextArtifact(data) {
		contentType = "text/plain"
	}

	// Determine storage class based on artifact type and size
	storageClass := a.determineStorageClass(data)

	_, err := a.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:        aws.String(a.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentType:   aws.String(contentType),
		StorageClass:  types.StorageClass(storageClass),
		Metadata: map[string]string{
			"Registry":     registryID,
			"Namespace":    namespace,
			"ArtifactName": artifactName,
			"Version":      version,
			"SavedAt":      time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to save artifact to S3: %w", err)
	}

	return key, nil
}

// determineStorageClass returns the appropriate S3 storage class
func (a *S3Adapter) determineStorageClass(data []byte) types.StorageClass {
	// Large artifacts (>128MB) go to Intelligent-Tiering
	if len(data) > 128*1024*1024 {
		return types.StorageClassIntelligentTiering
	}

	// Small artifacts (<1MB) go to Glacier for archival
	if len(data) < 1*1024*1024 {
		switch a.tiering {
		case "glacier":
			return types.StorageClassGlacier
		case "deep_archive":
			return types.StorageClassDeepArchive
		default:
			return types.StorageClassStandard
		}
	}

	return types.StorageClassStandard
}

// SaveArtifactStream saves an artifact to S3 by streaming directly from r,
// without buffering the whole payload in memory first. Storage class is
// fixed at Standard since streamed uploads don't know the final size up
// front (determineStorageClass needs len(data)); callers proxying very
// large or very small objects that want tiering should use SaveArtifact.
func (a *S3Adapter) SaveArtifactStream(registryID, namespace, artifactName, version string, r io.Reader) (string, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	_, err := a.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String("application/octet-stream"),
		Metadata: map[string]string{
			"Registry":     registryID,
			"Namespace":    namespace,
			"ArtifactName": artifactName,
			"Version":      version,
			"SavedAt":      time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to save artifact to S3: %w", err)
	}

	return key, nil
}

// GetArtifactStream opens an artifact from S3 for streaming. Returns (nil,
// nil) if it isn't cached.
func (a *S3Adapter) GetArtifactStream(registryID, namespace, artifactName, version string) (io.ReadCloser, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	resp, err := a.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get artifact from S3: %w", err)
	}

	return resp.Body, nil
}

// GetArtifact retrieves an artifact from S3
func (a *S3Adapter) GetArtifact(registryID, namespace, artifactName, version string) ([]byte, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	resp, err := a.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get artifact from S3: %w", err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// DeleteArtifact removes an artifact from S3
func (a *S3Adapter) DeleteArtifact(registryID, namespace, artifactName, version string) error {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	_, err := a.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(key),
	})
	return err
}

// ArtifactExists checks if an artifact exists in S3
func (a *S3Adapter) ArtifactExists(registryID, namespace, artifactName, version string) (bool, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	_, err := a.client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Upload uploads a file to S3
func (a *S3Adapter) Upload(bucket, key string, data []byte, contentType string) error {
	if bucket == "" {
		bucket = a.bucket
	}

	_, err := a.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	return err
}

// Download downloads a file from S3
func (a *S3Adapter) Download(bucket, key string) ([]byte, error) {
	if bucket == "" {
		bucket = a.bucket
	}

	resp, err := a.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// DeleteFile deletes a file from S3
func (a *S3Adapter) DeleteFile(bucket, key string) error {
	if bucket == "" {
		bucket = a.bucket
	}

	_, err := a.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

// ListFiles lists files in S3 with prefix
func (a *S3Adapter) ListFiles(bucket, prefix string) ([]string, error) {
	if bucket == "" {
		bucket = a.bucket
	}

	var keys []string
	paginator := s3.NewListObjectsV2Paginator(a.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return nil, err
		}

		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}

	return keys, nil
}

// generatePresignedURL generates a presigned URL for direct client upload/download
func (a *S3Adapter) generatePresignedURL(method, key string, expires time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(a.client)
	req, err := presignClient.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(key),
	}, func(o *s3.PresignOptions) {
		o.Expires = expires
	})
	if err != nil {
		return "", err
	}

	// Build URL with proper scheme
	scheme := "https"
	if !a.ssl {
		scheme = "http"
	}

	parsedURL, err := url.Parse(req.URL)
	if err != nil {
		return "", err
	}
	parsedURL.Scheme = scheme
	parsedURL.Host = a.endpoint

	return parsedURL.String(), nil
}

// isTextArtifact checks if the data appears to be text
func isTextArtifact(data []byte) bool {
	// Check for common text patterns
	textCheck := strings.TrimSpace(string(data))
	if len(textCheck) == 0 {
		return true
	}

	// Simple heuristic: if first 1KB has many non-printable chars, it's binary
	binaryCount := 0
	for i, b := range data {
		if i >= 1024 {
			break
		}
		if (b < 32 && b != '\n' && b != '\r' && b != '\t') || b == 127 {
			binaryCount++
		}
	}

	return float64(binaryCount) < float64(len(data))*0.1
}
