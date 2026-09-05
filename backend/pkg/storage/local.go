package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// StorageAdapter is the interface for artifact storage backends
type StorageAdapter interface {
	// Artifact operations
	SaveArtifact(registryID, namespace, artifactName, version string, data []byte) (string, error)
	GetArtifact(registryID, namespace, artifactName, version string) ([]byte, error)
	DeleteArtifact(registryID, namespace, artifactName, version string) error
	ArtifactExists(registryID, namespace, artifactName, version string) (bool, error)

	// Streaming artifact operations. Large payloads (e.g. Docker image
	// layers) route through these instead of the []byte versions above, so
	// they never sit fully buffered in process memory. GetArtifactStream
	// returns (nil, nil) when the artifact isn't cached, mirroring
	// GetArtifact's not-found convention; callers must Close a non-nil
	// ReadCloser.
	SaveArtifactStream(registryID, namespace, artifactName, version string, r io.Reader) (string, error)
	GetArtifactStream(registryID, namespace, artifactName, version string) (io.ReadCloser, error)

	// File operations
	Upload(bucket, key string, data []byte, contentType string) error
	Download(bucket, key string) ([]byte, error)
	DeleteFile(bucket, key string) error
	ListFiles(bucket, prefix string) ([]string, error)

	// Connection
	Connect() error
	Disconnect() error
}

// New creates a new StorageAdapter based on type
func New(storageType string, config map[string]string) (StorageAdapter, error) {
	switch storageType {
	case "", "local":
		return NewLocalAdapter(config)
	case "s3":
		return NewS3Adapter(config)
	case "gcs":
		return NewGCSAdapter(config)
	case "azure":
		return NewAzureAdapter(config)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}
}

// LocalAdapter implements local file system storage
type LocalAdapter struct {
	basePath string
}

// NewLocalAdapter creates a new LocalAdapter
func NewLocalAdapter(config map[string]string) (*LocalAdapter, error) {
	path := config["path"]
	if path == "" {
		path = "./storage"
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	return &LocalAdapter{basePath: path}, nil
}

// Connect is a no-op for local adapter
func (a *LocalAdapter) Connect() error {
	return nil
}

// Disconnect is a no-op for local adapter
func (a *LocalAdapter) Disconnect() error {
	return nil
}

// getArtifactPath constructs the storage path for an artifact
func (a *LocalAdapter) getArtifactPath(registryID, namespace, artifactName, version string) string {
	// Path structure: registry/namespace/artifact/version/digest
	return filepath.Join(a.basePath, registryID, namespace, artifactName, version)
}

// SaveArtifact saves an artifact to local storage
func (a *LocalAdapter) SaveArtifact(registryID, namespace, artifactName, version string, data []byte) (string, error) {
	path := a.getArtifactPath(registryID, namespace, artifactName, version)
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", err
	}

	// Write to file
	filePath := filepath.Join(path, "artifact.bin")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", err
	}

	// Return the path as storage reference
	return filePath, nil
}

// GetArtifact retrieves an artifact from local storage
func (a *LocalAdapter) GetArtifact(registryID, namespace, artifactName, version string) ([]byte, error) {
	path := a.getArtifactPath(registryID, namespace, artifactName, version)
	filePath := filepath.Join(path, "artifact.bin")

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// SaveArtifactStream saves an artifact to local storage by copying directly
// from r, without buffering the whole payload in memory first.
func (a *LocalAdapter) SaveArtifactStream(registryID, namespace, artifactName, version string, r io.Reader) (string, error) {
	path := a.getArtifactPath(registryID, namespace, artifactName, version)
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", err
	}

	filePath := filepath.Join(path, "artifact.bin")
	f, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		os.Remove(filePath)
		return "", err
	}

	return filePath, nil
}

// GetArtifactStream opens an artifact from local storage for streaming.
// Returns (nil, nil) if it isn't cached.
func (a *LocalAdapter) GetArtifactStream(registryID, namespace, artifactName, version string) (io.ReadCloser, error) {
	path := a.getArtifactPath(registryID, namespace, artifactName, version)
	filePath := filepath.Join(path, "artifact.bin")

	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return f, nil
}

// DeleteArtifact removes an artifact from local storage
func (a *LocalAdapter) DeleteArtifact(registryID, namespace, artifactName, version string) error {
	path := a.getArtifactPath(registryID, namespace, artifactName, version)
	return os.RemoveAll(path)
}

// ArtifactExists checks if an artifact exists
func (a *LocalAdapter) ArtifactExists(registryID, namespace, artifactName, version string) (bool, error) {
	path := a.getArtifactPath(registryID, namespace, artifactName, version)
	filePath := filepath.Join(path, "artifact.bin")
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Upload uploads a file to local storage
func (a *LocalAdapter) Upload(bucket, key string, data []byte, contentType string) error {
	path := filepath.Join(a.basePath, bucket, key)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Download downloads a file from local storage
func (a *LocalAdapter) Download(bucket, key string) ([]byte, error) {
	path := filepath.Join(a.basePath, bucket, key)
	return os.ReadFile(path)
}

// DeleteFile deletes a file from local storage
func (a *LocalAdapter) DeleteFile(bucket, key string) error {
	path := filepath.Join(a.basePath, bucket, key)
	return os.Remove(path)
}

// ListFiles lists files in local storage. Keys are returned relative to
// bucket (matching the S3/GCS/Azure adapters), so each one can be passed
// straight back into Download/DeleteFile without re-deriving a path.
func (a *LocalAdapter) ListFiles(bucket, prefix string) ([]string, error) {
	base := filepath.Join(a.basePath, bucket)
	matches, err := filepath.Glob(filepath.Join(base, prefix, "*"))
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, err := filepath.Rel(base, m)
		if err != nil {
			continue
		}
		keys = append(keys, rel)
	}
	return keys, nil
}
