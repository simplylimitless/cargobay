package storage

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLocalAdapter tests the local storage adapter
func TestLocalAdapter(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	config := map[string]string{"path": tempDir}
	adapter, err := NewLocalAdapter(config)
	require.NoError(t, err)

	// Test SaveArtifact
	registryID := "test-registry"
	namespace := "test-namespace"
	artifactName := "test-artifact"
	version := "1.0.0"
	data := []byte("test data content")

	filePath, err := adapter.SaveArtifact(registryID, namespace, artifactName, version, data)
	require.NoError(t, err)
	assert.NotEmpty(t, filePath)

	// Test GetArtifact
	retrievedData, err := adapter.GetArtifact(registryID, namespace, artifactName, version)
	require.NoError(t, err)
	assert.Equal(t, data, retrievedData)

	// Test ArtifactExists
	exists, err := adapter.ArtifactExists(registryID, namespace, artifactName, version)
	require.NoError(t, err)
	assert.True(t, exists)

	// Test DeleteArtifact
	err = adapter.DeleteArtifact(registryID, namespace, artifactName, version)
	require.NoError(t, err)

	// Verify deletion
	exists, err = adapter.ArtifactExists(registryID, namespace, artifactName, version)
	require.NoError(t, err)
	assert.False(t, exists)

	// Test non-existent artifact
	nonExistentData, err := adapter.GetArtifact(registryID, namespace, "non-existent", version)
	require.NoError(t, err)
	assert.Nil(t, nonExistentData)
}

// TestLocalAdapterWithDifferentPaths tests storage with various path structures
func TestLocalAdapterWithDifferentPaths(t *testing.T) {
	tempDir := t.TempDir()
	config := map[string]string{"path": tempDir}
	adapter, err := NewLocalAdapter(config)
	require.NoError(t, err)

	// Test with empty namespace
	data := []byte("test data")
	_, err = adapter.SaveArtifact("registry", "", "artifact", "1.0.0", data)
	require.NoError(t, err)

	_, err = adapter.GetArtifact("registry", "", "artifact", "1.0.0")
	require.NoError(t, err)
}

// TestLocalAdapterConcurrent tests concurrent access
func TestLocalAdapterConcurrent(t *testing.T) {
	tempDir := t.TempDir()
	config := map[string]string{"path": tempDir}
	adapter, err := NewLocalAdapter(config)
	require.NoError(t, err)

	data := []byte("test data")
	done := make(chan bool, 10)

	// Run concurrent saves
	for i := 0; i < 10; i++ {
		go func(index int) {
			_, err := adapter.SaveArtifact(
				"concurrent-registry",
				"namespace",
				"artifact",
				fmt.Sprintf("version-%d", index),
				data,
			)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all artifacts were saved
	for i := 0; i < 10; i++ {
		exists, err := adapter.ArtifactExists(
			"concurrent-registry",
			"namespace",
			"artifact",
			fmt.Sprintf("version-%d", i),
		)
		require.NoError(t, err)
		assert.True(t, exists)
	}
}

// TestLocalAdapterLargeFiles tests handling of large files
func TestLocalAdapterLargeFiles(t *testing.T) {
	tempDir := t.TempDir()
	config := map[string]string{"path": tempDir}
	adapter, err := NewLocalAdapter(config)
	require.NoError(t, err)

	// Create a 10MB file
	data := make([]byte, 10*1024*1024)
	_, err = io.ReadFull(rand.Reader, data)
	require.NoError(t, err)

	_, err = adapter.SaveArtifact("large-registry", "ns", "artifact", "1.0.0", data)
	require.NoError(t, err)

	retrievedData, err := adapter.GetArtifact("large-registry", "ns", "artifact", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, len(data), len(retrievedData))
}

// TestStorageAdapterInterface ensures all implementations satisfy the interface
func TestStorageAdapterInterface(t *testing.T) {
	// Test that LocalAdapter implements StorageAdapter
	tempDir := t.TempDir()
	config := map[string]string{"path": tempDir}
	adapter, err := NewLocalAdapter(config)
	require.NoError(t, err)

	var _ StorageAdapter = adapter
}

// TestNewStorageAdapter tests the New function
func TestNewStorageAdapter(t *testing.T) {
	// Test local storage
	config := map[string]string{}
	adapter, err := New("local", config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)

	// Test unknown storage type
	_, err = New("unknown", map[string]string{})
	assert.Error(t, err)
}
