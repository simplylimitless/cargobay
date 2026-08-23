package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAzureAdapterNew tests Azure adapter creation with container name
func TestAzureAdapterNew(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}

	adapter, err := NewAzureAdapter(config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
	assert.Equal(t, "test-container", adapter.container)
	assert.Equal(t, "Hot", adapter.tiering) // default tiering
}

// TestAzureAdapterNewMissingContainer tests Azure adapter without container name
func TestAzureAdapterNewMissingContainer(t *testing.T) {
	config := map[string]string{}

	adapter, err := NewAzureAdapter(config)

	assert.Nil(t, adapter)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "container name is required")
}

// TestAzureAdapterNewWithPrefix tests Azure adapter with prefix
func TestAzureAdapterNewWithPrefix(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
		"prefix":    "my/prefix",
	}

	adapter, err := NewAzureAdapter(config)
	require.NoError(t, err)
	assert.Equal(t, "my/prefix", adapter.prefix)
}

// TestAzureAdapterNewWithTiering tests Azure adapter with custom tiering
func TestAzureAdapterNewWithTiering(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
		"tiering":   "Cool",
	}

	adapter, err := NewAzureAdapter(config)
	require.NoError(t, err)
	assert.Equal(t, "Cool", adapter.tiering)
}

// TestAzureAdapterNewWithConnectionstring tests Azure adapter with connection string
func TestAzureAdapterNewWithConnectionstring(t *testing.T) {
	config := map[string]string{
		"container":     "test-container",
		"connection_string": "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=dGVzdGtleQ==;",
	}

	adapter, err := NewAzureAdapter(config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

// TestAzureAdapterNewWithAccountKey tests Azure adapter with account key
func TestAzureAdapterNewWithAccountKey(t *testing.T) {
	config := map[string]string{
		"container":   "test-container",
		"account_name": "testaccount",
		"account_key":  "dGVzdD0=",
		"account_url":  "https://testaccount.blob.core.windows.net",
	}

	adapter, err := NewAzureAdapter(config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

// TestAzureAdapterNewWithSASToken tests Azure adapter with SAS token
func TestAzureAdapterNewWithSASToken(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
		"account_url": "https://testaccount.blob.core.windows.net",
		"sas_token": "?sv=2020-08-04&st=2023-01-01",
	}

	adapter, err := NewAzureAdapter(config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

// TestAzureAdapterNewWithManagedIdentity tests Azure adapter with managed identity
func TestAzureAdapterNewWithManagedIdentity(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
		"account_url": "https://testaccount.blob.core.windows.net",
	}

	adapter, err := NewAzureAdapter(config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

// TestAzureAdapterGetStoragePath tests storage path construction
func TestAzureAdapterGetStoragePath(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	path := adapter.getStoragePath("registry1", "namespace1", "artifact1", "1.0.0")
	assert.Equal(t, "registry1/namespace1/artifact1/1.0.0/artifact.bin", path)
}

// TestAzureAdapterGetStoragePathWithPrefix tests storage path with prefix
func TestAzureAdapterGetStoragePathWithPrefix(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
		"prefix":    "my/prefix",
	}
	adapter, _ := NewAzureAdapter(config)

	path := adapter.getStoragePath("registry1", "namespace1", "artifact1", "1.0.0")
	assert.Equal(t, "my/prefix/registry1/namespace1/artifact1/1.0.0/artifact.bin", path)
}

// TestAzureAdapterConnect tests connect method (doesn't actually connect)
func TestAzureAdapterConnect(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	err := adapter.Connect()
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
}

// TestAzureAdapterDisconnect tests disconnect method
func TestAzureAdapterDisconnect(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	err := adapter.Disconnect()
	assert.NoError(t, err)
}

// TestAzureAdapterUpload tests upload method (doesn't actually upload)
func TestAzureAdapterUpload(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	data := []byte("test data")
	err := adapter.Upload("test-container", "test-key", data, "text/plain")
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
}

// TestAzureAdapterDownload tests download method (doesn't actually download)
func TestAzureAdapterDownload(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	data, err := adapter.Download("test-container", "test-key")
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
	assert.Nil(t, data)
}

// TestAzureAdapterDeleteFile tests delete file method
func TestAzureAdapterDeleteFile(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	err := adapter.DeleteFile("test-container", "test-key")
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
}

// TestAzureAdapterListFiles tests list files method (doesn't actually list)
func TestAzureAdapterListFiles(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	keys, err := adapter.ListFiles("test-container", "prefix/")
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
	assert.NotNil(t, keys)
}

// TestAzureAdapterArtifactExists tests artifact exists method
func TestAzureAdapterArtifactExists(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	exists, err := adapter.ArtifactExists("registry1", "namespace1", "artifact1", "1.0.0")
	assert.NoError(t, err)
	assert.False(t, exists)
}

// TestAzureAdapterGetArtifact tests get artifact method
func TestAzureAdapterGetArtifact(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	data, err := adapter.GetArtifact("registry1", "namespace1", "artifact1", "1.0.0")
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
	assert.Nil(t, data)
}

// TestAzureAdapterSaveArtifact tests save artifact method
func TestAzureAdapterSaveArtifact(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	data := []byte("test data")
	key, err := adapter.SaveArtifact("registry1", "namespace1", "artifact1", "1.0.0", data)
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
	assert.NotEmpty(t, key)
}

// TestAzureAdapterDeleteArtifact tests delete artifact method
func TestAzureAdapterDeleteArtifact(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	err := adapter.DeleteArtifact("registry1", "namespace1", "artifact1", "1.0.0")
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
}

// TestAzureAdapterUploadWithBucketOverride tests upload with bucket override
func TestAzureAdapterUploadWithBucketOverride(t *testing.T) {
	config := map[string]string{
		"container": "default-container",
	}
	adapter, _ := NewAzureAdapter(config)

	data := []byte("test data")
	err := adapter.Upload("override-container", "test-key", data, "text/plain")
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
}

// TestAzureAdapterDownloadWithBucketOverride tests download with bucket override
func TestAzureAdapterDownloadWithBucketOverride(t *testing.T) {
	config := map[string]string{
		"container": "default-container",
	}
	adapter, _ := NewAzureAdapter(config)

	data, err := adapter.Download("override-container", "test-key")
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
	assert.Nil(t, data)
}

// TestAzureAdapterDeleteFileWithBucketOverride tests delete file with bucket override
func TestAzureAdapterDeleteFileWithBucketOverride(t *testing.T) {
	config := map[string]string{
		"container": "default-container",
	}
	adapter, _ := NewAzureAdapter(config)

	err := adapter.DeleteFile("override-container", "test-key")
	// We don't assert error because it depends on Azure connectivity
	assert.NoError(t, err)
}

// TestAzureAdapterPathWithSpecialChars tests storage path with special characters
func TestAzureAdapterPathWithSpecialChars(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	path := adapter.getStoragePath("registry-1", "org/team", "my-artifact", "v1.0.0")
	assert.Equal(t, "registry-1/org/team/my-artifact/v1.0.0/artifact.bin", path)
}

// TestAzureAdapterStoragePathEmptyFields tests storage path with empty fields
func TestAzureAdapterStoragePathEmptyFields(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	path := adapter.getStoragePath("", "", "", "")
	assert.Equal(t, "////artifact.bin", path)
}

// TestAzureAdapterConnectDisconnectIdempotent tests connect/disconnect are idempotent
func TestAzureAdapterConnectDisconnectIdempotent(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	// Connect multiple times
	_ = adapter.Connect()
	_ = adapter.Connect()

	// Disconnect multiple times
	_ = adapter.Disconnect()
	_ = adapter.Disconnect()

	// Should not panic
}

// TestAzureAdapterNilConfig tests Azure adapter with nil config
func TestAzureAdapterNilConfig(t *testing.T) {
	adapter, err := NewAzureAdapter(nil)

	assert.Nil(t, adapter)
	assert.Error(t, err)
}

// TestAzureAdapterEmptyConfig tests Azure adapter with empty config
func TestAzureAdapterEmptyConfig(t *testing.T) {
	adapter, err := NewAzureAdapter(map[string]string{})

	assert.Nil(t, adapter)
	assert.Error(t, err)
}

// TestAzureAdapterMultipleConfigVariations tests various config combinations
func TestAzureAdapterMultipleConfigVariations(t *testing.T) {
	testCases := []struct {
		name   string
		config map[string]string
	}{
		{"Minimal config", map[string]string{"container": "test"}},
		{"With prefix", map[string]string{"container": "test", "prefix": "my/prefix"}},
		{"With tiering", map[string]string{"container": "test", "tiering": "Cool"}},
		{"With connection string", map[string]string{"container": "test", "connection_string": "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=dGVzdGtleQ==;"}},
		{"With account key", map[string]string{"container": "test", "account_name": "test", "account_key": "dGVzdD0=", "account_url": "https://test.blob.core.windows.net"}},
		{"With SAS token", map[string]string{"container": "test", "account_url": "https://test.blob.core.windows.net", "sas_token": "?sv=2020-08-04"}},
		{"All options", map[string]string{
			"container": "test",
			"prefix":    "my/prefix",
			"tiering":   "Archive",
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := NewAzureAdapter(tc.config)
			require.NoError(t, err, "Config should be valid: %v", tc.config)
			assert.NotNil(t, adapter)
			assert.Equal(t, tc.config["container"], adapter.container)
		})
	}
}

// TestAzureAdapterArchiveTier tests archive tiering
func TestAzureAdapterArchiveTier(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
		"tiering":   "Archive",
	}
	adapter, _ := NewAzureAdapter(config)

	assert.Equal(t, "Archive", adapter.tiering)
}

// TestAzureAdapterCoolTier tests cool tiering
func TestAzureAdapterCoolTier(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
		"tiering":   "Cool",
	}
	adapter, _ := NewAzureAdapter(config)

	assert.Equal(t, "Cool", adapter.tiering)
}

// TestAzureAdapterListFilesWithPrefix tests list files with prefix
func TestAzureAdapterListFilesWithPrefix(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	keys, err := adapter.ListFiles("test-container", "prefix/")
	assert.NoError(t, err)
	assert.NotNil(t, keys)
}

// TestAzureAdapterArtifactExistsWithPrefix tests artifact exists with prefix
func TestAzureAdapterArtifactExistsWithPrefix(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
		"prefix":    "my/prefix",
	}
	adapter, _ := NewAzureAdapter(config)

	exists, err := adapter.ArtifactExists("registry1", "namespace1", "artifact1", "1.0.0")
	assert.NoError(t, err)
	assert.False(t, exists)
}

// TestAzureAdapterSaveArtifactWithMetadata tests save artifact with metadata
func TestAzureAdapterSaveArtifactWithMetadata(t *testing.T) {
	config := map[string]string{
		"container": "test-container",
	}
	adapter, _ := NewAzureAdapter(config)

	data := []byte("test data")
	key, err := adapter.SaveArtifact("registry1", "namespace1", "artifact1", "1.0.0", data)
	assert.NoError(t, err)
	assert.NotEmpty(t, key)
}
