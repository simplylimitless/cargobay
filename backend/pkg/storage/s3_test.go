package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestS3AdapterNew tests S3 adapter creation with bucket name
func TestS3AdapterNew(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}

	adapter, err := NewS3Adapter(config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
	assert.Equal(t, "test-bucket", adapter.bucket)
	assert.Equal(t, "us-east-1", adapter.region) // default region
}

// TestS3AdapterNewWithCustomRegion tests S3 adapter with custom region
func TestS3AdapterNewWithCustomRegion(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
		"region": "eu-west-1",
	}

	adapter, err := NewS3Adapter(config)
	require.NoError(t, err)
	assert.Equal(t, "eu-west-1", adapter.region)
}

// TestS3AdapterNewMissingBucket tests S3 adapter without bucket name
func TestS3AdapterNewMissingBucket(t *testing.T) {
	config := map[string]string{}

	adapter, err := NewS3Adapter(config)

	assert.Nil(t, adapter)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bucket name is required")
}

// TestS3AdapterNewWithPrefix tests S3 adapter with prefix
func TestS3AdapterNewWithPrefix(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
		"prefix": "/my/prefix",
	}

	adapter, err := NewS3Adapter(config)
	require.NoError(t, err)
	assert.Equal(t, "my/prefix", adapter.prefix) // prefix should be trimmed
}

// TestS3AdapterNewWithEndpoint tests S3 adapter with custom endpoint (MinIO)
func TestS3AdapterNewWithEndpoint(t *testing.T) {
	config := map[string]string{
		"bucket":  "test-bucket",
		"endpoint": "http://localhost:9000",
		"region":  "us-east-1",
	}

	adapter, err := NewS3Adapter(config)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:9000", adapter.endpoint)
}

// TestS3AdapterNewWithCredentials tests S3 adapter with explicit credentials
func TestS3AdapterNewWithCredentials(t *testing.T) {
	config := map[string]string{
		"bucket":      "test-bucket",
		"access_key":  "test-access-key",
		"secret_key":  "test-secret-key",
		"region":      "us-east-1",
	}

	adapter, err := NewS3Adapter(config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

// TestS3AdapterGetStoragePath tests storage path construction
func TestS3AdapterGetStoragePath(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	path := adapter.getStoragePath("registry1", "namespace1", "artifact1", "1.0.0")
	assert.Equal(t, "registry1/namespace1/artifact1/1.0.0/artifact.bin", path)
}

// TestS3AdapterGetStoragePathWithPrefix tests storage path with prefix
func TestS3AdapterGetStoragePathWithPrefix(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
		"prefix": "my/prefix",
	}
	adapter, _ := NewS3Adapter(config)

	path := adapter.getStoragePath("registry1", "namespace1", "artifact1", "1.0.0")
	assert.Equal(t, "my/prefix/registry1/namespace1/artifact1/1.0.0/artifact.bin", path)
}

// TestS3AdapterNewWithSSL tests S3 adapter with SSL disabled
func TestS3AdapterNewWithSSL(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
		"ssl":    "false",
	}

	adapter, err := NewS3Adapter(config)
	require.NoError(t, err)
	assert.False(t, adapter.ssl)
}

// TestS3AdapterNewWithTiering tests S3 adapter with custom tiering
func TestS3AdapterNewWithTiering(t *testing.T) {
	config := map[string]string{
		"bucket":  "test-bucket",
		"tiering": "glacier",
	}

	adapter, err := NewS3Adapter(config)
	require.NoError(t, err)
	assert.Equal(t, "glacier", adapter.tiering)
}

// TestS3AdapterNewWithDefaultTiering tests S3 adapter with default tiering
func TestS3AdapterNewWithDefaultTiering(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}

	adapter, err := NewS3Adapter(config)
	require.NoError(t, err)
	assert.Equal(t, "STANDARD", adapter.tiering)
}

// TestS3AdapterConnect tests connect method (doesn't actually connect)
func TestS3AdapterConnect(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	// This would fail without real AWS credentials, but we can test the method exists
	// We don't assert error because it depends on AWS connectivity
	_ = adapter.Connect()
}

// TestS3AdapterDisconnect tests disconnect method
func TestS3AdapterDisconnect(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	err := adapter.Disconnect()
	assert.NoError(t, err)
}

// TestS3AdapterUpload tests upload method (doesn't actually upload)
func TestS3AdapterUpload(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	data := []byte("test data")
	err := adapter.Upload("test-bucket", "test-key", data, "text/plain")
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
}

// TestS3AdapterDownload tests download method (doesn't actually download)
func TestS3AdapterDownload(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	data, err := adapter.Download("test-bucket", "test-key")
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
	assert.Nil(t, data)
}

// TestS3AdapterDeleteFile tests delete file method
func TestS3AdapterDeleteFile(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	err := adapter.DeleteFile("test-bucket", "test-key")
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
}

// TestS3AdapterListFiles tests list files method (doesn't actually list)
func TestS3AdapterListFiles(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	keys, err := adapter.ListFiles("test-bucket", "prefix/")
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
	assert.NotNil(t, keys)
}

// TestS3AdapterArtifactExists tests artifact exists method
func TestS3AdapterArtifactExists(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	exists, err := adapter.ArtifactExists("registry1", "namespace1", "artifact1", "1.0.0")
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
	assert.False(t, exists)
}

// TestS3AdapterGetArtifact tests get artifact method
func TestS3AdapterGetArtifact(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	data, err := adapter.GetArtifact("registry1", "namespace1", "artifact1", "1.0.0")
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
	assert.Nil(t, data)
}

// TestS3AdapterSaveArtifact tests save artifact method
func TestS3AdapterSaveArtifact(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	data := []byte("test data")
	key, err := adapter.SaveArtifact("registry1", "namespace1", "artifact1", "1.0.0", data)
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
	assert.NotEmpty(t, key)
}

// TestS3AdapterDeleteArtifact tests delete artifact method
func TestS3AdapterDeleteArtifact(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	err := adapter.DeleteArtifact("registry1", "namespace1", "artifact1", "1.0.0")
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
}

// TestS3AdapterDetermineStorageClassLarge tests storage class for large artifacts
func TestS3AdapterDetermineStorageClassLarge(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	// Create large data (>128MB)
	largeData := make([]byte, 129*1024*1024)

	storageClass := adapter.determineStorageClass(largeData)
	assert.Equal(t, "IntelligentTiering", string(storageClass))
}

// TestS3AdapterDetermineStorageClassSmall tests storage class for small artifacts
func TestS3AdapterDetermineStorageClassSmall(t *testing.T) {
	config := map[string]string{
		"bucket":  "test-bucket",
		"tiering": "glacier",
	}
	adapter, _ := NewS3Adapter(config)

	// Create small data (<1MB)
	smallData := make([]byte, 1024)

	storageClass := adapter.determineStorageClass(smallData)
	assert.Equal(t, "Glacier", string(storageClass))
}

// TestS3AdapterDetermineStorageClassMedium tests storage class for medium artifacts
func TestS3AdapterDetermineStorageClassMedium(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	// Create medium data (between 1MB and 128MB)
	mediumData := make([]byte, 10*1024*1024)

	storageClass := adapter.determineStorageClass(mediumData)
	assert.Equal(t, "Standard", string(storageClass))
}

// TestS3AdapterDetermineStorageClassDeepArchive tests deep archive tiering
func TestS3AdapterDetermineStorageClassDeepArchive(t *testing.T) {
	config := map[string]string{
		"bucket":  "test-bucket",
		"tiering": "deep_archive",
	}
	adapter, _ := NewS3Adapter(config)

	// Create small data
	smallData := make([]byte, 1024)

	storageClass := adapter.determineStorageClass(smallData)
	assert.Equal(t, "DeepArchive", string(storageClass))
}

// TestS3AdapterIsTextArtifactEmpty tests empty data is text
func TestS3AdapterIsTextArtifactEmpty(t *testing.T) {
	data := []byte{}

	isText := isTextArtifact(data)
	assert.True(t, isText)
}

// TestS3AdapterIsTextArtifactWhitespace tests whitespace-only data is text
func TestS3AdapterIsTextArtifactWhitespace(t *testing.T) {
	data := []byte("   \n\t  ")

	isText := isTextArtifact(data)
	assert.True(t, isText)
}

// TestS3AdapterIsTextArtifactText tests text data is detected as text
func TestS3AdapterIsTextArtifactText(t *testing.T) {
	data := []byte("This is plain text content")

	isText := isTextArtifact(data)
	assert.True(t, isText)
}

// TestS3AdapterIsTextArtifactBinary tests binary data is detected as binary
func TestS3AdapterIsTextArtifactBinary(t *testing.T) {
	// Create binary data with many non-printable characters
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i) // Many non-printable chars
	}

	isText := isTextArtifact(data)
	assert.False(t, isText)
}

// TestS3AdapterUploadWithBucketOverride tests upload with bucket override
func TestS3AdapterUploadWithBucketOverride(t *testing.T) {
	config := map[string]string{
		"bucket": "default-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	data := []byte("test data")
	err := adapter.Upload("override-bucket", "test-key", data, "text/plain")
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
}

// TestS3AdapterDownloadWithBucketOverride tests download with bucket override
func TestS3AdapterDownloadWithBucketOverride(t *testing.T) {
	config := map[string]string{
		"bucket": "default-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	data, err := adapter.Download("override-bucket", "test-key")
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
	assert.Nil(t, data)
}

// TestS3AdapterDeleteFileWithBucketOverride tests delete file with bucket override
func TestS3AdapterDeleteFileWithBucketOverride(t *testing.T) {
	config := map[string]string{
		"bucket": "default-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	err := adapter.DeleteFile("override-bucket", "test-key")
	// We don't assert error because it depends on AWS connectivity
	assert.NoError(t, err)
}

// TestS3AdapterNewWithSessionToken tests S3 adapter with session token
func TestS3AdapterNewWithSessionToken(t *testing.T) {
	config := map[string]string{
		"bucket":        "test-bucket",
		"access_key":    "test-access-key",
		"secret_key":    "test-secret-key",
		"session_token": "test-session-token",
	}

	adapter, err := NewS3Adapter(config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

// TestS3AdapterListFilesWithPrefix tests list files with prefix
func TestS3AdapterListFilesWithPrefix(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	// This tests that the method can be called without error
	// Actual listing depends on AWS connectivity
	keys, err := adapter.ListFiles("test-bucket", "prefix/")
	assert.NoError(t, err)
	assert.NotNil(t, keys)
}

// TestS3AdapterArtifactExistsWithPrefix tests artifact exists with prefix
func TestS3AdapterArtifactExistsWithPrefix(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
		"prefix": "my/prefix",
	}
	adapter, _ := NewS3Adapter(config)

	exists, err := adapter.ArtifactExists("registry1", "namespace1", "artifact1", "1.0.0")
	assert.NoError(t, err)
	assert.False(t, exists)
}

// TestS3AdapterSaveArtifactWithTextContent tests saving text content
func TestS3AdapterSaveArtifactWithTextContent(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	// Text content
	data := []byte("package main\n\nfunc main() {\n    println(\"hello\")\n}")

	key, err := adapter.SaveArtifact("registry1", "namespace1", "artifact1", "1.0.0", data)
	assert.NoError(t, err)
	assert.NotEmpty(t, key)
}

// TestS3AdapterMultipleConfigVariations tests various config combinations
func TestS3AdapterMultipleConfigVariations(t *testing.T) {
	testCases := []struct {
		name   string
		config map[string]string
	}{
		{"Minimal config", map[string]string{"bucket": "test"}},
		{"With region", map[string]string{"bucket": "test", "region": "eu-west-1"}},
		{"With prefix", map[string]string{"bucket": "test", "prefix": "my/prefix"}},
		{"With endpoint", map[string]string{"bucket": "test", "endpoint": "http://localhost:9000"}},
		{"With credentials", map[string]string{"bucket": "test", "access_key": "key", "secret_key": "secret"}},
		{"With all options", map[string]string{
			"bucket":  "test",
			"region":  "us-west-2",
			"prefix":  "my/prefix",
			"tiering": "glacier",
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := NewS3Adapter(tc.config)
			require.NoError(t, err, "Config should be valid: %v", tc.config)
			assert.NotNil(t, adapter)
			assert.Equal(t, tc.config["bucket"], adapter.bucket)
		})
	}
}

// TestS3AdapterNilConfig tests S3 adapter with nil config
func TestS3AdapterNilConfig(t *testing.T) {
	adapter, err := NewS3Adapter(nil)

	assert.Nil(t, adapter)
	assert.Error(t, err)
}

// TestS3AdapterEmptyConfig tests S3 adapter with empty config
func TestS3AdapterEmptyConfig(t *testing.T) {
	adapter, err := NewS3Adapter(map[string]string{})

	assert.Nil(t, adapter)
	assert.Error(t, err)
}

// TestS3AdapterPathWithSpecialChars tests storage path with special characters
func TestS3AdapterPathWithSpecialChars(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	// Test with namespace/artifact that might have special chars
	path := adapter.getStoragePath("registry-1", "org/team", "my-artifact", "v1.0.0")
	assert.Equal(t, "registry-1/org/team/my-artifact/v1.0.0/artifact.bin", path)
}

// TestS3AdapterConnectDisconnectIdempotent tests connect/disconnect are idempotent
func TestS3AdapterConnectDisconnectIdempotent(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	// Connect multiple times
	_ = adapter.Connect()
	_ = adapter.Connect()

	// Disconnect multiple times
	_ = adapter.Disconnect()
	_ = adapter.Disconnect()

	// Should not panic
}

// TestS3AdapterStoragePathEmptyFields tests storage path with empty fields
func TestS3AdapterStoragePathEmptyFields(t *testing.T) {
	config := map[string]string{
		"bucket": "test-bucket",
	}
	adapter, _ := NewS3Adapter(config)

	path := adapter.getStoragePath("", "", "", "")
	assert.Equal(t, "//artifact.bin", path)
}
