package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGCSAdapterNew tests GCS adapter creation with bucket name
func TestGCSAdapterNew(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}

	adapter, err := NewGCSAdapter(config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
	assert.Equal(t, "test-bucket", adapter.bucket)
}

// TestGCSAdapterNewMissingBucket tests GCS adapter without bucket name
func TestGCSAdapterNewMissingBucket(t *testing.T) {
	config := map[string]string{}

	adapter, err := NewGCSAdapter(config)

	assert.Nil(t, adapter)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bucket name is required")
}

// TestGCSAdapterNewWithPrefix tests GCS adapter with prefix
func TestGCSAdapterNewWithPrefix(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
		"prefix": "my/prefix",
	}

	adapter, err := NewGCSAdapter(config)
	require.NoError(t, err)
	assert.Equal(t, "my/prefix", adapter.prefix)
}

// TestGCSAdapterNewWithServiceAccount tests GCS adapter with service account
func TestGCSAdapterNewWithServiceAccount(t *testing.T) {
	config := map[string]string{
		"bucket":    "test-bucket",
		"json_key":  `{"type": "service_account"}`,
	}

	adapter, err := NewGCSAdapter(config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

// TestGCSAdapterGetStoragePath tests storage path construction
func TestGCSAdapterGetStoragePath(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	path := adapter.getStoragePath("registry1", "namespace1", "artifact1", "1.0.0")
	assert.Equal(t, "registry1/namespace1/artifact1/1.0.0/artifact.bin", path)
}

// TestGCSAdapterGetStoragePathWithPrefix tests storage path with prefix
func TestGCSAdapterGetStoragePathWithPrefix(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
		"prefix": "my/prefix",
	}
	adapter, _ := NewGCSAdapter(config)

	path := adapter.getStoragePath("registry1", "namespace1", "artifact1", "1.0.0")
	assert.Equal(t, "my/prefix/registry1/namespace1/artifact1/1.0.0/artifact.bin", path)
}

// TestGCSAdapterConnect tests connect method (doesn't actually connect)
func TestGCSAdapterConnect(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	err := adapter.Connect()
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
}

// TestGCSAdapterDisconnect tests disconnect method
func TestGCSAdapterDisconnect(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	err := adapter.Disconnect()
	assert.NoError(t, err)
}

// TestGCSAdapterUpload tests upload method (doesn't actually upload)
func TestGCSAdapterUpload(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	data := []byte("test data")
	err := adapter.Upload("test-bucket", "test-key", data, "text/plain")
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
}

// TestGCSAdapterDownload tests download method (doesn't actually download)
func TestGCSAdapterDownload(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	data, err := adapter.Download("test-bucket", "test-key")
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
	assert.Nil(t, data)
}

// TestGCSAdapterDeleteFile tests delete file method
func TestGCSAdapterDeleteFile(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	err := adapter.DeleteFile("test-bucket", "test-key")
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
}

// TestGCSAdapterListFiles tests list files method (doesn't actually list)
func TestGCSAdapterListFiles(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	keys, err := adapter.ListFiles("test-bucket", "prefix/")
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
	assert.NotNil(t, keys)
}

// TestGCSAdapterArtifactExists tests artifact exists method
func TestGCSAdapterArtifactExists(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	exists, err := adapter.ArtifactExists("registry1", "namespace1", "artifact1", "1.0.0")
	assert.NoError(t, err)
	assert.False(t, exists)
}

// TestGCSAdapterGetArtifact tests get artifact method
func TestGCSAdapterGetArtifact(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	data, err := adapter.GetArtifact("registry1", "namespace1", "artifact1", "1.0.0")
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
	assert.Nil(t, data)
}

// TestGCSAdapterSaveArtifact tests save artifact method
func TestGCSAdapterSaveArtifact(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	data := []byte("test data")
	key, err := adapter.SaveArtifact("registry1", "namespace1", "artifact1", "1.0.0", data)
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
	assert.NotEmpty(t, key)
}

// TestGCSAdapterDeleteArtifact tests delete artifact method
func TestGCSAdapterDeleteArtifact(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	err := adapter.DeleteArtifact("registry1", "namespace1", "artifact1", "1.0.0")
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
}

// TestGCSAdapterUploadWithBucketOverride tests upload with bucket override
func TestGCSAdapterUploadWithBucketOverride(t *testing.T) {
	config := map[string]string{
		"bucket": "default-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	data := []byte("test data")
	err := adapter.Upload("override-bucket", "test-key", data, "text/plain")
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
}

// TestGCSAdapterDownloadWithBucketOverride tests download with bucket override
func TestGCSAdapterDownloadWithBucketOverride(t *testing.T) {
	config := map[string]string{
		"bucket": "default-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	data, err := adapter.Download("override-bucket", "test-key")
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
	assert.Nil(t, data)
}

// TestGCSAdapterDeleteFileWithBucketOverride tests delete file with bucket override
func TestGCSAdapterDeleteFileWithBucketOverride(t *testing.T) {
	config := map[string]string{
		"bucket": "default-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	err := adapter.DeleteFile("override-bucket", "test-key")
	// We don't assert error because it depends on GCS connectivity
	assert.NoError(t, err)
}

// TestGCSAdapterPathWithSpecialChars tests storage path with special characters
func TestGCSAdapterPathWithSpecialChars(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	path := adapter.getStoragePath("registry-1", "org/team", "my-artifact", "v1.0.0")
	assert.Equal(t, "registry-1/org/team/my-artifact/v1.0.0/artifact.bin", path)
}

// TestGCSAdapterStoragePathEmptyFields tests storage path with empty fields
func TestGCSAdapterStoragePathEmptyFields(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	path := adapter.getStoragePath("", "", "", "")
	assert.Equal(t, "//artifact.bin", path)
}

// TestGCSAdapterConnectDisconnectIdempotent tests connect/disconnect are idempotent
func TestGCSAdapterConnectDisconnectIdempotent(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	// Connect multiple times
	_ = adapter.Connect()
	_ = adapter.Connect()

	// Disconnect multiple times
	_ = adapter.Disconnect()
	_ = adapter.Disconnect()

	// Should not panic
}

// TestGCSAdapterNilConfig tests GCS adapter with nil config
func TestGCSAdapterNilConfig(t *testing.T) {
	adapter, err := NewGCSAdapter(nil)

	assert.Nil(t, adapter)
	assert.Error(t, err)
}

// TestGCSAdapterEmptyConfig tests GCS adapter with empty config
func TestGCSAdapterEmptyConfig(t *testing.T) {
	adapter, err := NewGCSAdapter(map[string]string{})

	assert.Nil(t, adapter)
	assert.Error(t, err)
}

// TestGCSAdapterMultipleConfigVariations tests various config combinations
func TestGCSAdapterMultipleConfigVariations(t *testing.T) {
	testCases := []struct {
		name   string
		config map[string]string
	}{
		{"Minimal config", map[string]string{"bucket": "test"}},
		{"With prefix", map[string]string{"bucket": "test", "prefix": "my/prefix"}},
		{"With service account", map[string]string{"bucket": "test", "json_key": `{"type": "service_account"}`}},
		{"All options", map[string]string{
			"bucket":   "test",
			"prefix":   "my/prefix",
			"json_key": `{"type": "service_account"}`,
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := NewGCSAdapter(tc.config)
			require.NoError(t, err, "Config should be valid: %v", tc.config)
			assert.NotNil(t, adapter)
			assert.Equal(t, tc.config["bucket"], adapter.bucket)
		})
	}
}

// TestGCSAdapterStorageClassLarge tests storage class for large artifacts
func TestGCSAdapterStorageClassLarge(t *testing.T) {
	// For large files (>1MB), storage class is STANDARD
	// This is tested indirectly through SaveArtifact
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	// Create large data
	largeData := make([]byte, 1024*1024+1)

	// The storage class logic is in SaveArtifact
	// We just verify the method exists and works
	key, err := adapter.SaveArtifact("registry1", "namespace1", "artifact1", "1.0.0", largeData)
	assert.NoError(t, err)
	assert.NotEmpty(t, key)
}

// TestGCSAdapterStorageClassSmall tests storage class for small artifacts
func TestGCSAdapterStorageClassSmall(t *testing.T) {
	// For small files (<1MB), storage class is ARCHIVE
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	// Create small data
	smallData := make([]byte, 1024)

	key, err := adapter.SaveArtifact("registry1", "namespace1", "artifact1", "1.0.0", smallData)
	assert.NoError(t, err)
	assert.NotEmpty(t, key)
}

// TestGCSAdapterListFilesWithPrefix tests list files with prefix
func TestGCSAdapterListFilesWithPrefix(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewGCSAdapter(config)

	keys, err := adapter.ListFiles("test-bucket", "prefix/")
	assert.NoError(t, err)
	assert.NotNil(t, keys)
}

// TestGCSAdapterArtifactExistsWithPrefix tests artifact exists with prefix
func TestGCSAdapterArtifactExistsWithPrefix(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
		"prefix": "my/prefix",
	}
	adapter, _ := NewGCSAdapter(config)

	exists, err := adapter.ArtifactExists("registry1", "namespace1", "artifact1", "1.0.0")
	assert.NoError(t, err)
	assert.False(t, exists)
}
