package e2e

import (
	"testing"

	"github.com/anthropics/cargobay/backend/pkg/cache"
	"github.com/anthropics/cargobay/backend/pkg/database"
	"github.com/anthropics/cargobay/backend/pkg/rbac"
	"github.com/anthropics/cargobay/backend/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatabaseConnection tests database connectivity
func TestDatabaseConnection(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")
	assert.NotNil(t, db)
}

// TestStorageAdapter tests storage adapter creation
func TestStorageAdapter(t *testing.T) {
	config := map[string]string{"path": "/tmp/cargobay-test-storage"}
	adapter, err := storage.New("local", config)
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}

// TestCacheConnection tests cache connectivity
func TestCacheConnection(t *testing.T) {
	cacheClient, err := cache.New("redis", "redis://localhost:6379")
	require.NoError(t, err)
	assert.NotNil(t, cacheClient)
	defer cacheClient.Close()
}

// TestRBACRoles tests RBAC role creation and retrieval
func TestRBACRoles(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")
	rbacMgr := rbac.New(db)
	assert.NotNil(t, rbacMgr)

	roles := rbacMgr.ListRoles()
	assert.GreaterOrEqual(t, len(roles), 5)

	// Check for expected roles
	expectedRoles := map[string]bool{
		"admin":     false,
		"operator":  false,
		"developer": false,
		"viewer":    false,
		"auditor":   false,
	}

	for _, role := range roles {
		if _, ok := expectedRoles[role.Name]; ok {
			expectedRoles[role.Name] = true
		}
	}

	for role, found := range expectedRoles {
		assert.True(t, found, "Role %s not found", role)
	}
}

// TestRBACPermissions tests RBAC permission creation and retrieval
func TestRBACPermissions(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")
	rbacMgr := rbac.New(db)
	assert.NotNil(t, rbacMgr)

	permissions := rbacMgr.ListPermissions()
	assert.GreaterOrEqual(t, len(permissions), 15)

	// Check for expected permission categories
	expectedPermissions := map[string]bool{
		"artifact:read":      false,
		"artifact:write":     false,
		"artifact:delete":    false,
		"artifact:scan":      false,
		"artifact:sign":      false,
		"registry:read":      false,
		"registry:write":     false,
		"registry:delete":    false,
		"user:read":          false,
		"user:write":         false,
		"user:delete":        false,
		"role:read":          false,
		"role:write":         false,
		"role:delete":        false,
		"system:read":        false,
		"system:write":       false,
	}

	for _, perm := range permissions {
		if _, ok := expectedPermissions[perm.ID]; ok {
			expectedPermissions[perm.ID] = true
		}
	}

	// Verify at least key permissions exist
	assert.True(t, expectedPermissions["artifact:read"], "artifact:read permission not found")
	assert.True(t, expectedPermissions["artifact:write"], "artifact:write permission not found")
	assert.True(t, expectedPermissions["registry:read"], "registry:read permission not found")
}

// TestArtifactLifecycle tests the complete artifact lifecycle
func TestArtifactLifecycle(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")
	storageAdapter, err := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	require.NoError(t, err)

	// Create artifact
	artifact := &database.ArtifactMetadata{
		ID:              "lifecycle-test-" + generateID(),
		RegistryID:      "test-registry",
		ArtifactType:    "docker",
		Namespace:       "library",
		ArtifactName:    "lifecycle-test",
		Version:         "1.0.0",
		Digest:          "sha256:lifecycletest",
		DigestAlgorithm: "sha256",
		Size:            1024,
		Metadata:        map[string]any{"test": "value"},
		Tags:            []string{"latest", "test"},
	}

	err = db.SaveArtifact(artifact)
	require.NoError(t, err)

	// Retrieve artifact
	retrieved, err := db.GetArtifact(artifact.ID)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, artifact.ID, retrieved.ID)

	// Search for artifact
	results, err := db.SearchArtifacts("lifecycle-test", database.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)

	// Update artifact
	retrieved.Tags = append(retrieved.Tags, "updated")
	err = db.SaveArtifact(retrieved)
	require.NoError(t, err)

	// Verify update
	updated, err := db.GetArtifact(artifact.ID)
	require.NoError(t, err)
	assert.Contains(t, updated.Tags, "updated")

	// Cleanup
	err = db.DeleteArtifact(artifact.ID)
	require.NoError(t, err)

	// Verify deletion
	deleted, err := db.GetArtifact(artifact.ID)
	assert.Nil(t, deleted)
	assert.Error(t, err)
}

// TestSearchWithFilters tests searching with filters
func TestSearchWithFilters(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")

	// Create test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "filter-1", RegistryID: "npm", ArtifactType: "npm", ArtifactName: "package-a", Version: "1.0.0"},
		{ID: "filter-2", RegistryID: "npm", ArtifactType: "npm", ArtifactName: "package-b", Version: "2.0.0"},
		{ID: "filter-3", RegistryID: "maven", ArtifactType: "maven", ArtifactName: "package-c", Version: "3.0.0"},
	}

	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	// Search by artifact type
	results, err := db.SearchArtifacts("", database.SearchOptions{
		ArtifactType: "npm",
		Limit:        10,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, len(results))

	// Search by registry ID
	results, err = db.SearchArtifacts("", database.SearchOptions{
		RegistryID: "maven",
		Limit:      10,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results))
}

// TestPagination tests cursor-based pagination
func TestPagination(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")

	// Create many artifacts
	for i := 0; i < 50; i++ {
		db.SaveArtifact(&database.ArtifactMetadata{
			ID:              "pagetest-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			RegistryID:      "test",
			ArtifactType:    "docker",
			ArtifactName:    "pagination-test",
			Version:         "1.0.0",
		})
	}

	// First page
	results1, err := db.SearchArtifacts("", database.SearchOptions{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Equal(t, 10, len(results1))

	// Second page
	results2, err := db.SearchArtifacts("", database.SearchOptions{Limit: 10, Offset: 10})
	require.NoError(t, err)
	assert.Equal(t, 10, len(results2))

	// Results should be different
	assert.NotEqual(t, results1[0].ID, results2[0].ID)
}

// TestConcurrency tests concurrent database operations
func TestConcurrency(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			artifact := &database.ArtifactMetadata{
				ID:              "concurrent-" + string(rune('a'+id)),
				RegistryID:      "test",
				ArtifactType:    "docker",
				ArtifactName:    "concurrent-test",
				Version:         "1.0.0",
			}
			db.SaveArtifact(artifact)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all artifacts were created
	count, err := db.CountArtifacts("test", database.ListOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 10)
}

// TestSearchCount tests search count functionality
func TestSearchCount(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")

	// Create test artifacts
	for i := 0; i < 25; i++ {
		db.SaveArtifact(&database.ArtifactMetadata{
			ID:              "counttest-" + string(rune('a'+i%26)),
			RegistryID:      "test",
			ArtifactType:    "docker",
			ArtifactName:    "count-test",
			Version:         "1.0.0",
		})
	}

	// Get count
	count, err := db.SearchCount("", database.SearchOptions{Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 25)
}

// TestArtifactTypes tests different artifact types
func TestArtifactTypes(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")

	artifactTypes := []string{"docker", "npm", "maven", "pypi", "nuget", "helm", "generic"}

	for _, artifactType := range artifactTypes {
		artifact := &database.ArtifactMetadata{
			ID:              "types-" + artifactType,
			RegistryID:      "test",
			ArtifactType:    artifactType,
			ArtifactName:    "type-test",
			Version:         "1.0.0",
		}
		err := db.SaveArtifact(artifact)
		require.NoError(t, err)

		retrieved, err := db.GetArtifact(artifact.ID)
		require.NoError(t, err)
		assert.Equal(t, artifactType, retrieved.ArtifactType)

		db.DeleteArtifact(artifact.ID)
	}
}

// TestArtifactMetadata tests artifact metadata handling
func TestArtifactMetadata(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")

	metadata := map[string]any{
		"key1":    "value1",
		"key2":    123,
		"key3":    true,
		"key4":    []string{"a", "b", "c"},
		"key5":    map[string]any{"nested": "value"},
		"key6":    nil,
		"key7":    45.67,
	}

	artifact := &database.ArtifactMetadata{
		ID:              "metadata-test",
		RegistryID:      "test",
		ArtifactType:    "docker",
		ArtifactName:    "metadata-test",
		Version:         "1.0.0",
		Metadata:        metadata,
	}

	err := db.SaveArtifact(artifact)
	require.NoError(t, err)

	retrieved, err := db.GetArtifact(artifact.ID)
	require.NoError(t, err)
	assert.Equal(t, metadata, retrieved.Metadata)
}

// TestArtifactTags tests artifact tag handling
func TestArtifactTags(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")

	tags := []string{"latest", "stable", "v1.0.0", "release-candidate"}

	artifact := &database.ArtifactMetadata{
		ID:              "tags-test",
		RegistryID:      "test",
		ArtifactType:    "docker",
		ArtifactName:    "tags-test",
		Version:         "1.0.0",
		Tags:            tags,
	}

	err := db.SaveArtifact(artifact)
	require.NoError(t, err)

	retrieved, err := db.GetArtifact(artifact.ID)
	require.NoError(t, err)
	assert.Equal(t, tags, retrieved.Tags)

	// Add more tags
	retrieved.Tags = append(retrieved.Tags, "new-tag")
	err = db.SaveArtifact(retrieved)
	require.NoError(t, err)

	updated, err := db.GetArtifact(artifact.ID)
	require.NoError(t, err)
	assert.Contains(t, updated.Tags, "new-tag")
}

// TestArtifactSearch tests full-text search
func TestArtifactSearch(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")

	artifacts := []*database.ArtifactMetadata{
		{ID: "search-nginx", RegistryID: "test", ArtifactType: "docker", ArtifactName: "nginx", Namespace: "library", Version: "1.0.0"},
		{ID: "search-apache", RegistryID: "test", ArtifactType: "docker", ArtifactName: "apache", Namespace: "httpd", Version: "2.4.0"},
		{ID: "search-express", RegistryID: "test", ArtifactType: "npm", ArtifactName: "express", Version: "4.18.0"},
	}

	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	// Search by name
	results, err := db.SearchArtifacts("nginx", database.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)

	// Search by namespace
	results, err = db.SearchArtifacts("library", database.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)

	// Search by type
	results, err = db.SearchArtifacts("npm", database.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// TestRegistryOperations tests registry CRUD operations
func TestRegistryOperations(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")

	// Create registry
	registry := &database.RegistryConfig{
		ID:       "test-registry-e2e",
		Name:     "Test Registry",
		URL:      "https://registry.example.com",
		Type:     "docker",
		Enabled:  true,
		Priority: 100,
	}

	err := db.SaveRegistry(registry)
	require.NoError(t, err)

	// Get registry
	retrieved, err := db.GetRegistry(registry.ID)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, registry.Name, retrieved.Name)

	// List registries
	registries, err := db.ListRegistries()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(registries), 1)

	// Update registry
	retrieved.URL = "https://updated.example.com"
	err = db.SaveRegistry(retrieved)
	require.NoError(t, err)

	updated, err := db.GetRegistry(registry.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://updated.example.com", updated.URL)

	// Delete registry
	err = db.DeleteRegistry(registry.ID)
	require.NoError(t, err)

	// Verify deletion
	deleted, err := db.GetRegistry(registry.ID)
	assert.Nil(t, deleted)
}

// TestUserOperations tests user CRUD operations
func TestUserOperations(t *testing.T) {
	db := database.New("postgres://localhost:5432/cargobay")

	// Create user
	user := &database.User{
		UserID:       "test-user-e2e",
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "$2a$10$testpasswordhash",
		Roles:        []string{"viewer"},
	}

	err := db.SaveUser(user)
	require.NoError(t, err)

	// Get user
	retrieved, err := db.GetUser(user.UserID)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, user.Username, retrieved.Username)

	// List users
	users, err := db.ListUsers()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(users), 1)

	// Update user
	retrieved.Email = "updated@example.com"
	err = db.SaveUser(retrieved)
	require.NoError(t, err)

	updated, err := db.GetUser(user.UserID)
	require.NoError(t, err)
	assert.Equal(t, "updated@example.com", updated.Email)

	// Delete user
	err = db.DeleteUser(user.UserID)
	require.NoError(t, err)

	// Verify deletion
	deleted, err := db.GetUser(user.UserID)
	assert.Nil(t, deleted)
}

// generateID generates a random ID for testing
func generateID() string {
	return "e2e-" + string(rune('a'+1)) + string(rune('0'+1))
}
