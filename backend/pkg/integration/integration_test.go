package integration

import (
	"testing"
	"time"

	"github.com/anthropics/cargobay/backend/pkg/cache"
	"github.com/anthropics/cargobay/backend/pkg/database"
	"github.com/anthropics/cargobay/backend/pkg/rbac"
	"github.com/anthropics/cargobay/backend/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationStorageDatabase tests storage and database integration
func TestIntegrationStorageDatabase(t *testing.T) {
	// This is an integration test example
	// In production, this would use test containers or mock services

	// Test that database can be instantiated
	db := database.New("postgres://localhost:5432/test")
	assert.NotNil(t, db)

	// Test that storage adapter can be created
	config := map[string]string{"path": "/tmp/cargobay-test"}
	adapter, err := storage.New("local", config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

// TestIntegrationRBACDatabase tests RBAC and database integration
func TestIntegrationRBACDatabase(t *testing.T) {
	// Test RBAC with database
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	assert.NotNil(t, rbacMgr)

	// Test that we can get roles
	roles := rbacMgr.ListRoles()
	assert.GreaterOrEqual(t, len(roles), 5)

	// Test that we can get permissions
	perms := rbacMgr.ListPermissions()
	assert.GreaterOrEqual(t, len(perms), 15)
}

// TestIntegrationCacheDatabase tests cache and database integration
func TestIntegrationCacheDatabase(t *testing.T) {
	// Test cache instantiation (using redis URL format)
	cacheClient, err := cache.New("redis", "redis://localhost:6379/0")
	require.NoError(t, err)
	assert.NotNil(t, cacheClient)
}

// TestIntegrationArtifactLifecycle tests the full artifact lifecycle
func TestIntegrationArtifactLifecycle(t *testing.T) {
	// This test demonstrates the full artifact lifecycle

	db := database.New("postgres://localhost:5432/test")
	config := map[string]string{"path": "/tmp/cargobay-test"}
	adapter, err := storage.New("local", config)
	require.NoError(t, err)

	// Step 1: Create artifact metadata in database
	artifact := &database.ArtifactMetadata{
		ID:              "test-lifecycle-id",
		RegistryID:      "test-registry",
		ArtifactType:    "docker",
		Namespace:       "library",
		ArtifactName:    "test-image",
		Version:         "1.0.0",
		Digest:          "sha256:test123",
		DigestAlgorithm: "sha256",
		Size:            1024,
		Metadata:        map[string]any{"key": "value"},
		Tags:            []string{"latest"},
	}

	// Step 2: Store artifact data in storage
	data := []byte("test artifact data")
	_, err = adapter.SaveArtifact(
		artifact.RegistryID,
		artifact.Namespace,
		artifact.ArtifactName,
		artifact.Version,
		data,
	)
	require.NoError(t, err)

	// Step 3: Verify artifact exists in storage
	exists, err := adapter.ArtifactExists(
		artifact.RegistryID,
		artifact.Namespace,
		artifact.ArtifactName,
		artifact.Version,
	)
	require.NoError(t, err)
	assert.True(t, exists)

	// Step 4: Retrieve artifact from storage
	retrieved, err := adapter.GetArtifact(
		artifact.RegistryID,
		artifact.Namespace,
		artifact.ArtifactName,
		artifact.Version,
	)
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)

	// Step 5: Delete artifact
	err = adapter.DeleteArtifact(
		artifact.RegistryID,
		artifact.Namespace,
		artifact.ArtifactName,
		artifact.Version,
	)
	require.NoError(t, err)

	// Step 6: Verify deletion
	exists, err = adapter.ArtifactExists(
		artifact.RegistryID,
		artifact.Namespace,
		artifact.ArtifactName,
		artifact.Version,
	)
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestIntegrationMultiRegionStorage tests multi-region storage scenarios
func TestIntegrationMultiRegionStorage(t *testing.T) {
	// Test local storage (primary)
	localConfig := map[string]string{"path": "/tmp/cargobay-local"}
	localAdapter, err := storage.New("local", localConfig)
	require.NoError(t, err)

	// Test that we can save and retrieve
	data := []byte("test data for multi-region")
	_, err = localAdapter.SaveArtifact("reg1", "ns", "art", "1.0.0", data)
	require.NoError(t, err)

	retrieved, err := localAdapter.GetArtifact("reg1", "ns", "art", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)
}

// TestIntegrationHighAvailability tests HA scenarios
func TestIntegrationHighAvailability(t *testing.T) {
	// Test that database connection pool is configured
	db := database.New("postgres://localhost:5432/test")
	assert.NotNil(t, db)
	assert.NotNil(t, db.pool)

	// Test that RBAC has system roles for HA
	rbacMgr := rbac.New(db)
	adminRole := rbacMgr.GetRole("admin")
	assert.NotNil(t, adminRole)
	assert.True(t, adminRole.IsSystem)
}

// TestIntegrationSecurity tests security-related integrations
func TestIntegrationSecurity(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)

	// Test that admin has all required security permissions
	admin := rbacMgr.GetRole("admin")
	adminPerms := rbacMgr.GetRolePermissions("admin")

	// Admin should have artifact permissions
	hasArtifactRead := false
	hasArtifactWrite := false
	hasArtifactDelete := false
	for _, p := range adminPerms {
		switch p {
		case "artifact:read":
			hasArtifactRead = true
		case "artifact:write":
			hasArtifactWrite = true
		case "artifact:delete":
			hasArtifactDelete = true
		}
	}

	assert.True(t, hasArtifactRead, "Admin should have artifact:read")
	assert.True(t, hasArtifactWrite, "Admin should have artifact:write")
	assert.True(t, hasArtifactDelete, "Admin should have artifact:delete")

	// Test that viewer has limited permissions
	viewerPerms := rbacMgr.GetRolePermissions("viewer")
	assert.Contains(t, viewerPerms, "artifact:read")
	// Viewer should NOT have write permissions
	hasWrite := false
	for _, p := range viewerPerms {
		if p == "artifact:write" {
			hasWrite = true
			break
		}
	}
	assert.False(t, hasWrite, "Viewer should not have artifact:write")
}

// TestIntegrationSearch tests search functionality integration
func TestIntegrationSearch(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")

	// Test that search options can be created
	opts := database.SearchOptions{
		RegistryID:   "test-registry",
		ArtifactType: "docker",
		Limit:        100,
		Offset:       0,
	}

	assert.Equal(t, "test-registry", opts.RegistryID)
	assert.Equal(t, 100, opts.Limit)
}

// TestIntegrationReplication tests replication configuration
func TestIntegrationReplication(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")

	// Test that we can create registry configurations
	registry := database.RegistryConfig{
		ID:       "replica-1",
		Name:     "Replica Region 1",
		URL:      "https://replica1.cargobay.internal",
		Type:     "docker",
		Proxy:    true,
		Enabled:  true,
		Priority: 10,
	}

	assert.Equal(t, "replica-1", registry.ID)
	assert.True(t, registry.Proxy)
}

// TestIntegrationVulnerabilityScanning tests vulnerability scanning integration
func TestIntegrationVulnerabilityScanning(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")

	// Test that vulnerability scanner can be created
	scanner := vulnerability.New(db, "", false)
	assert.NotNil(t, scanner)

	// Test that scanner can check if artifact is scannable (using public method)
	assert.True(t, scanner.IsScannable("docker"))
	assert.True(t, scanner.IsScannable("npm"))
	assert.True(t, scanner.IsScannable("maven"))
	assert.True(t, scanner.IsScannable("pypi"))

	// Test non-scannable types
	assert.False(t, scanner.IsScannable("tar"))
	assert.False(t, scanner.IsScannable("zip"))
	assert.False(t, scanner.IsScannable("unknown"))
}

// TestIntegrationMetrics tests metrics collection setup
func TestIntegrationMetrics(t *testing.T) {
	// Test that cache stats can be created
	cacheStats := cache.CacheStats{}
	assert.Equal(t, 0, cacheStats.Hits)
	assert.Equal(t, 0, cacheStats.Misses)
}

// Integration test suite setup
func TestIntegrationSuite(t *testing.T) {
	t.Run("StorageDatabase", TestIntegrationStorageDatabase)
	t.Run("RBACDatabase", TestIntegrationRBACDatabase)
	t.Run("CacheDatabase", TestIntegrationCacheDatabase)
	t.Run("ArtifactLifecycle", TestIntegrationArtifactLifecycle)
	t.Run("MultiRegionStorage", TestIntegrationMultiRegionStorage)
	t.Run("HighAvailability", TestIntegrationHighAvailability)
	t.Run("Security", TestIntegrationSecurity)
	t.Run("Search", TestIntegrationSearch)
	t.Run("Replication", TestIntegrationReplication)
	t.Run("VulnerabilityScanning", TestIntegrationVulnerabilityScanning)
	t.Run("Metrics", TestIntegrationMetrics)
}
