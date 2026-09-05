package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// GCSAdapter implements Google Cloud Storage with hot/cold tiering
type GCSAdapter struct {
	client *storage.Client
	bucket string
	prefix string
}

// NewGCSAdapter creates a new GCS storage adapter
func NewGCSAdapter(config map[string]string) (*GCSAdapter, error) {
	bucket := config["bucket"]
	if bucket == "" {
		return nil, fmt.Errorf("bucket name is required for GCS storage")
	}

	prefix := config["prefix"]

	ctx := context.Background()
	var client *storage.Client
	var err error

	// Check for service account file
	if jsonKey := config["json_key"]; jsonKey != "" {
		client, err = storage.NewClient(ctx, option.WithCredentialsJSON([]byte(jsonKey)))
	} else {
		client, err = storage.NewClient(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	return &GCSAdapter{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// Connect establishes connection to GCS
func (a *GCSAdapter) Connect() error {
	// Test connection by listing buckets
	_, err := a.client.Bucket(a.bucket).Attrs(context.Background())
	return err
}

// Disconnect closes connections
func (a *GCSAdapter) Disconnect() error {
	return a.client.Close()
}

// getStoragePath constructs the GCS object name for an artifact
func (a *GCSAdapter) getStoragePath(registryID, namespace, artifactName, version string) string {
	path := fmt.Sprintf("%s/%s/%s/%s/%s", registryID, namespace, artifactName, version, "artifact.bin")
	if a.prefix != "" {
		path = fmt.Sprintf("%s/%s", a.prefix, path)
	}
	return path
}

// SaveArtifact saves an artifact to GCS with automatic tiering
func (a *GCSAdapter) SaveArtifact(registryID, namespace, artifactName, version string, data []byte) (string, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	bucket := a.client.Bucket(a.bucket)
	obj := bucket.Object(key)

	// Determine storage class based on size
	storageClass := "STANDARD"
	if len(data) < 1*1024*1024 {
		storageClass = "ARCHIVE"
	}

	// Set metadata
	meta := &storage.ObjectAttrs{
		ContentType:      "application/octet-stream",
		CacheControl:     "public, max-age=31536000",
		StorageClass:     storageClass,
		CustomTime:       time.Now(),
		Metadata: map[string]string{
			"Registry":     registryID,
			"Namespace":    namespace,
			"ArtifactName": artifactName,
			"Version":      version,
			"SavedAt":      time.Now().UTC().Format(time.RFC3339),
		},
	}

	w := obj.NewWriter(context.Background())
	w.ObjectAttrs = *meta
	if _, err := w.Write(data); err != nil {
		return "", fmt.Errorf("failed to write to GCS: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("failed to close GCS writer: %w", err)
	}

	return key, nil
}

// SaveArtifactStream saves an artifact to GCS by streaming directly from r,
// without buffering the whole payload in memory first. Storage class is
// fixed at Standard since the streamed size isn't known up front.
func (a *GCSAdapter) SaveArtifactStream(registryID, namespace, artifactName, version string, r io.Reader) (string, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	obj := a.client.Bucket(a.bucket).Object(key)
	w := obj.NewWriter(context.Background())
	w.ContentType = "application/octet-stream"
	w.CacheControl = "public, max-age=31536000"
	w.StorageClass = "STANDARD"
	w.CustomTime = time.Now()
	w.Metadata = map[string]string{
		"Registry":     registryID,
		"Namespace":    namespace,
		"ArtifactName": artifactName,
		"Version":      version,
		"SavedAt":      time.Now().UTC().Format(time.RFC3339),
	}

	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return "", fmt.Errorf("failed to write to GCS: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("failed to close GCS writer: %w", err)
	}

	return key, nil
}

// GetArtifactStream opens an artifact from GCS for streaming. Returns (nil,
// nil) if it isn't cached.
func (a *GCSAdapter) GetArtifactStream(registryID, namespace, artifactName, version string) (io.ReadCloser, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	obj := a.client.Bucket(a.bucket).Object(key)
	r, err := obj.NewReader(context.Background())
	if err != nil {
		if storage.ErrObjectNotExist == err {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

// GetArtifact retrieves an artifact from GCS
func (a *GCSAdapter) GetArtifact(registryID, namespace, artifactName, version string) ([]byte, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	obj := a.client.Bucket(a.bucket).Object(key)
	r, err := obj.NewReader(context.Background())
	if err != nil {
		if storage.ErrObjectNotExist == err {
			return nil, nil
		}
		return nil, err
	}
	defer r.Close()

	return io.ReadAll(r)
}

// DeleteArtifact removes an artifact from GCS
func (a *GCSAdapter) DeleteArtifact(registryID, namespace, artifactName, version string) error {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	return a.client.Bucket(a.bucket).Object(key).Delete(context.Background())
}

// ArtifactExists checks if an artifact exists in GCS
func (a *GCSAdapter) ArtifactExists(registryID, namespace, artifactName, version string) (bool, error) {
	key := a.getStoragePath(registryID, namespace, artifactName, version)

	_, err := a.client.Bucket(a.bucket).Object(key).Attrs(context.Background())
	if err != nil {
		if storage.ErrObjectNotExist == err {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Upload uploads a file to GCS
func (a *GCSAdapter) Upload(bucket, key string, data []byte, contentType string) error {
	if bucket == "" {
		bucket = a.bucket
	}

	w := a.client.Bucket(bucket).Object(key).NewWriter(context.Background())
	w.ContentType = contentType
	if _, err := w.Write(data); err != nil {
		return err
	}
	return w.Close()
}

// Download downloads a file from GCS
func (a *GCSAdapter) Download(bucket, key string) ([]byte, error) {
	if bucket == "" {
		bucket = a.bucket
	}

	r, err := a.client.Bucket(bucket).Object(key).NewReader(context.Background())
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return io.ReadAll(r)
}

// DeleteFile deletes a file from GCS
func (a *GCSAdapter) DeleteFile(bucket, key string) error {
	if bucket == "" {
		bucket = a.bucket
	}

	return a.client.Bucket(bucket).Object(key).Delete(context.Background())
}

// ListFiles lists files in GCS with prefix
func (a *GCSAdapter) ListFiles(bucket, prefix string) ([]string, error) {
	if bucket == "" {
		bucket = a.bucket
	}

	query := &storage.Query{Prefix: prefix}
	it := a.client.Bucket(bucket).Objects(context.Background(), query)

	var keys []string
	for {
		attrs, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		keys = append(keys, attrs.Name)
	}

	return keys, nil
}
