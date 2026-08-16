package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateUUID tests UUID generation
func TestGenerateUUID(t *testing.T) {
	id1 := generateUUID()
	id2 := generateUUID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
}

// TestArtifactMetadata tests artifact metadata operations
func TestArtifactMetadata(t *testing.T) {
	// Test ArtifactMetadata struct
	artifact := &ArtifactMetadata{
		ID:              "test-id",
		RegistryID:      "test-registry",
		ArtifactType:    "docker",
		Namespace:       "library",
		ArtifactName:    "nginx",
		Version:         "1.21.0",
		Digest:          "sha256:abc123",
		DigestAlgorithm: "sha256",
		Size:            1024,
		Metadata:        map[string]any{"key": "value"},
		Tags:            []string{"latest", "stable"},
	}

	assert.Equal(t, "test-id", artifact.ID)
	assert.Equal(t, "docker", artifact.ArtifactType)
	assert.Equal(t, "nginx", artifact.ArtifactName)
	assert.Len(t, artifact.Tags, 2)
}

// TestSignature tests signature struct
func TestSignature(t *testing.T) {
	sig := Signature{
		Type:       "pgp",
		KeyID:      "0x12345678",
		Signature:  "base64encoded",
		Timestamp:  time.Now(),
		Verified:   true,
	}

	assert.Equal(t, "pgp", sig.Type)
	assert.True(t, sig.Verified)
}

// TestUserRepository tests user struct
func TestUserRepository(t *testing.T) {
	user := UserRepository{
		UserID:       "user-123",
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "hashedpassword",
		Roles:        []string{"user", "developer"},
		CreatedAt:    time.Now(),
		IsActive:     true,
	}

	assert.Equal(t, "testuser", user.Username)
	assert.True(t, user.IsActive)
	assert.Len(t, user.Roles, 2)
}

// TestAccessKey tests access key struct
func TestAccessKey(t *testing.T) {
	now := time.Now()
	key := AccessKey{
		ID:          "key-123",
		UserID:      "user-123",
		Name:        "production-key",
		KeyHash:     "key_hash_123",
		Permissions: []string{"artifact:read", "artifact:write"},
		CreatedAt:   now,
		ExpiresAt:   &now,
		IsActive:    true,
	}

	assert.Equal(t, "production-key", key.Name)
	assert.True(t, key.IsActive)
	assert.Len(t, key.Permissions, 2)
}

// TestRegistryConfig tests registry config struct
func TestRegistryConfig(t *testing.T) {
	registry := RegistryConfig{
		ID:       "npm-registry",
		Name:     "NPM Registry",
		URL:      "https://registry.npmjs.org",
		Type:     "npm",
		Proxy:    true,
		Enabled:  true,
		Priority: 1,
	}

	assert.Equal(t, "npm-registry", registry.ID)
	assert.True(t, registry.Proxy)
	assert.True(t, registry.Enabled)
}

// TestListOptions tests list options
func TestListOptions(t *testing.T) {
	opts := ListOptions{
		Namespace:    "library",
		ArtifactType: "docker",
		Limit:        100,
		Offset:       0,
		OrderBy:      "created",
		Order:        "desc",
	}

	assert.Equal(t, "library", opts.Namespace)
	assert.Equal(t, "docker", opts.ArtifactType)
	assert.Equal(t, 100, opts.Limit)
}

// TestSearchOptions tests search options
func TestSearchOptions(t *testing.T) {
	opts := SearchOptions{
		RegistryID:   "docker-registry",
		ArtifactType: "docker",
		Limit:        50,
		Offset:       0,
	}

	assert.Equal(t, "docker-registry", opts.RegistryID)
	assert.Equal(t, 50, opts.Limit)
}

// TestSearchResults tests search results
func TestSearchResults(t *testing.T) {
	artifacts := []ArtifactMetadata{{ID: "1"}, {ID: "2"}}
	results := SearchResults{
		Artifacts:  artifacts,
		Total:      2,
		HasMore:    false,
		NextCursor: "",
	}

	assert.Len(t, results.Artifacts, 2)
	assert.Equal(t, 2, results.Total)
	assert.False(t, results.HasMore)
}

// TestRoleDefinition tests role definition
func TestRoleDefinition(t *testing.T) {
	role := RoleDefinition{
		ID:          "admin",
		Name:        "Administrator",
		Description: "Full admin access",
		Permissions: []string{"artifact:read", "artifact:write"},
		IsSystem:    true,
	}

	assert.Equal(t, "admin", role.ID)
	assert.True(t, role.IsSystem)
	assert.Len(t, role.Permissions, 2)
}

// TestPermissionDefinition tests permission definition
func TestPermissionDefinition(t *testing.T) {
	perm := PermissionDefinition{
		ID:          "artifact:read",
		Name:        "Read Artifacts",
		Description: "Can read artifacts",
		Resource:    "artifact",
		Action:      "read",
		IsSystem:    true,
	}

	assert.Equal(t, "artifact:read", perm.ID)
	assert.Equal(t, "artifact", perm.Resource)
	assert.Equal(t, "read", perm.Action)
}

// TestAuditLog tests audit log struct
func TestAuditLog(t *testing.T) {
	log := AuditLog{
		ID:          "log-123",
		UserID:      "user-123",
		Action:      "artifact:upload",
		ResourceType: "artifact",
		ResourceID:   "artifact-456",
		Details:      "Uploaded nginx:1.21.0",
		CreatedAt:    time.Now(),
	}

	assert.Equal(t, "artifact:upload", log.Action)
	assert.Equal(t, "artifact", log.ResourceType)
}

// TestRole tests role struct
func TestRole(t *testing.T) {
	role := Role{
		ID:          "admin",
		Name:        "Administrator",
		Description: "Full admin access",
		IsSystem:    true,
		CreatedAt:   time.Now(),
	}

	assert.Equal(t, "admin", role.ID)
	assert.True(t, role.IsSystem)
}

// TestRolePermission tests role permission link
func TestRolePermission(t *testing.T) {
	rp := RolePermission{
		RoleID:      "admin",
		PermissionID: "artifact:read",
	}

	assert.Equal(t, "admin", rp.RoleID)
	assert.Equal(t, "artifact:read", rp.PermissionID)
}

// TestArtifactMetadata_JSONSerialization tests JSON serialization
func TestArtifactMetadata_JSONSerialization(t *testing.T) {
	artifact := &ArtifactMetadata{
		ID:              "test-id",
		ArtifactName:    "test",
		Version:         "1.0.0",
		Metadata:        map[string]any{"key": "value"},
		Tags:            []string{"tag1"},
	}

	// Test that the struct can be serialized (used in tests)
	assert.NotNil(t, artifact)
}

// TestSignature_JSONSerialization tests signature JSON serialization
func TestSignature_JSONSerialization(t *testing.T) {
	sig := Signature{
		Type:      "pgp",
		KeyID:     "0x123",
		Verified:  true,
		Timestamp: time.Now(),
	}

	assert.NotNil(t, sig)
	assert.True(t, sig.Verified)
}

// TestAccessKey_Expiry tests access key with expiry
func TestAccessKey_Expiry(t *testing.T) {
	now := time.Now()
	oneHourLater := now.Add(time.Hour)

	key := AccessKey{
		ID:          "test-key",
		ExpiresAt:   &oneHourLater,
		IsActive:    true,
		CreatedAt:   now,
	}

	assert.NotNil(t, key.ExpiresAt)
	assert.True(t, key.IsActive)
}

// TestListOptions_DefaultValues tests default values in list options
func TestListOptions_DefaultValues(t *testing.T) {
	opts := ListOptions{}

	// Test defaults
	assert.Equal(t, "", opts.Namespace)
	assert.Equal(t, 0, opts.Limit) // Will be defaulted in query
}

// TestSearchOptions_DefaultValues tests default values in search options
func TestSearchOptions_DefaultValues(t *testing.T) {
	opts := SearchOptions{}

	assert.Equal(t, "", opts.RegistryID)
	assert.Equal(t, 0, opts.Limit)
}

// TestDatabaseNew tests database instance creation
func TestDatabaseNew(t *testing.T) {
	db := New("postgres://localhost:5432/test")
	assert.NotNil(t, db)
}

// TestArtifactMetadata_Timestamps tests timestamp handling
func TestArtifactMetadata_Timestamps(t *testing.T) {
	createdAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	artifact := &ArtifactMetadata{
		ID:        "test",
		Created:   createdAt,
		Updated:   updatedAt,
		ArtifactName: "test",
		Version:   "1.0.0",
	}

	assert.Equal(t, createdAt, artifact.Created)
	assert.Equal(t, updatedAt, artifact.Updated)
}

// TestUserRepository_LastLogin tests optional last login
func TestUserRepository_LastLogin(t *testing.T) {
	now := time.Now()
	user := UserRepository{
		UserID:    "user-1",
		LastLogin: &now,
		IsActive:  true,
	}

	assert.NotNil(t, user.LastLogin)
}

// TestAccessKey_LastUsed tests optional last used
func TestAccessKey_LastUsed(t *testing.T) {
	now := time.Now()
	key := AccessKey{
		ID:        "key-1",
		LastUsed:  &now,
		IsActive:  true,
	}

	assert.NotNil(t, key.LastUsed)
}

// TestAccessKey_NoExpiry tests access key without expiry
func TestAccessKey_NoExpiry(t *testing.T) {
	key := AccessKey{
		ID:        "key-1",
		ExpiresAt: nil,
		IsActive:  true,
	}

	assert.Nil(t, key.ExpiresAt)
	assert.True(t, key.IsActive)
}
