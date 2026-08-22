// Package testutil provides shared test utilities for cargobay
package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
)

// TestDBPath returns a temporary database path for testing
func TestDBPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.db")
}

// TestTempDir returns a temporary directory for testing
func TestTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// MockArtifactMetadata creates a mock artifact for testing
func MockArtifactMetadata(overrides map[string]interface{}) *database.ArtifactMetadata {
	artifact := &database.ArtifactMetadata{
		ID:              "test-artifact-" + generateID(),
		RegistryID:      "test-registry",
		ArtifactType:    "docker",
		Namespace:       "library",
		ArtifactName:    "nginx",
		Version:         "1.21.0",
		Digest:          "sha256:" + generateHex(64),
		DigestAlgorithm: "sha256",
		Size:            1024,
		Metadata:        map[string]any{"key": "value"},
		Tags:            []string{"latest", "test"},
		Created:         time.Now(),
		Updated:         time.Now(),
	}

	// Apply overrides
	for k, v := range overrides {
		switch k {
		case "id":
			artifact.ID = v.(string)
		case "registry_id":
			artifact.RegistryID = v.(string)
		case "artifact_type":
			artifact.ArtifactType = v.(string)
		case "namespace":
			artifact.Namespace = v.(string)
		case "artifact_name":
			artifact.ArtifactName = v.(string)
		case "version":
			artifact.Version = v.(string)
		case "digest":
			artifact.Digest = v.(string)
		case "digest_algorithm":
			artifact.DigestAlgorithm = v.(string)
		case "size":
			artifact.Size = v.(int64)
		case "metadata":
			artifact.Metadata = v.(map[string]any)
		case "tags":
			artifact.Tags = v.([]string)
		}
	}

	return artifact
}

// MockRegistryConfig creates a mock registry for testing
func MockRegistryConfig(overrides map[string]interface{}) *database.RegistryConfig {
	registry := &database.RegistryConfig{
		ID:       "test-registry-" + generateID(),
		Name:     "Test Registry",
		URL:      "https://registry.example.com",
		Type:     "docker",
		Proxy:    true,
		Enabled:  true,
		Priority: 100,
	}

	// Apply overrides
	for k, v := range overrides {
		switch k {
		case "id":
			registry.ID = v.(string)
		case "name":
			registry.Name = v.(string)
		case "url":
			registry.URL = v.(string)
		case "type":
			registry.Type = v.(string)
		case "proxy":
			registry.Proxy = v.(bool)
		case "enabled":
			registry.Enabled = v.(bool)
		case "priority":
			registry.Priority = v.(int)
		case "host":
			registry.Host = v.(string)
		case "upstream_auth_type":
			registry.UpstreamAuthType = v.(string)
		case "upstream_username":
			registry.UpstreamUsername = v.(string)
		case "upstream_secret":
			registry.UpstreamSecret = v.(string)
		}
	}

	return registry
}

// MockUser creates a mock user for testing
func MockUser(overrides map[string]interface{}) *database.UserRepository {
	user := &database.UserRepository{
		UserID:       "test-user-" + generateID(),
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "$2a$10$" + generateHex(53), // bcrypt hash format
		Roles:        []string{"viewer"},
		CreatedAt:    time.Now(),
		LastLogin:    nil,
		IsActive:     true,
	}

	// Apply overrides
	for k, v := range overrides {
		switch k {
		case "user_id":
			user.UserID = v.(string)
		case "username":
			user.Username = v.(string)
		case "email":
			user.Email = v.(string)
		case "password_hash":
			user.PasswordHash = v.(string)
		case "roles":
			user.Roles = v.([]string)
		case "is_active":
			user.IsActive = v.(bool)
		case "last_login":
			user.LastLogin = v.(*time.Time)
		}
	}

	return user
}

// MockAccessKey creates a mock access key for testing
func MockAccessKey(overrides map[string]interface{}) *database.AccessKey {
	expiresAt := time.Now().Add(24 * time.Hour)
	key := &database.AccessKey{
		ID:          "test-key-" + generateID(),
		UserID:      "test-user-123",
		Name:        "test-key",
		KeyHash:     generateHex(64),
		Permissions: []string{"artifact:read", "artifact:write"},
		CreatedAt:   time.Now(),
		LastUsed:    nil,
		ExpiresAt:   &expiresAt,
		IsActive:    true,
	}

	// Apply overrides
	for k, v := range overrides {
		switch k {
		case "id":
			key.ID = v.(string)
		case "user_id":
			key.UserID = v.(string)
		case "name":
			key.Name = v.(string)
		case "key_hash":
			key.KeyHash = v.(string)
		case "permissions":
			key.Permissions = v.([]string)
		case "expires_at":
			key.ExpiresAt = v.(*time.Time)
		case "is_active":
			key.IsActive = v.(bool)
		}
	}

	return key
}

// MockAuditLog creates a mock audit log for testing
func MockAuditLog(overrides map[string]interface{}) *database.AuditLog {
	log := &database.AuditLog{
		ID:          "test-log-" + generateID(),
		UserID:      "test-user-123",
		Action:      "artifact:upload",
		ResourceType: "artifact",
		ResourceID:   "test-artifact-123",
		Details:      "Uploaded nginx:1.21.0",
		CreatedAt:    time.Now(),
	}

	// Apply overrides
	for k, v := range overrides {
		switch k {
		case "id":
			log.ID = v.(string)
		case "user_id":
			log.UserID = v.(string)
		case "action":
			log.Action = v.(string)
		case "resource_type":
			log.ResourceType = v.(string)
		case "resource_id":
			log.ResourceID = v.(string)
		case "details":
			log.Details = v.(string)
		}
	}

	return log
}

// MockSignature creates a mock signature for testing
func MockSignature(overrides map[string]interface{}) database.Signature {
	sig := database.Signature{
		Type:      "pgp",
		KeyID:     "0x" + generateHex(16),
		Timestamp: time.Now(),
		Verified:  true,
	}

	// Apply overrides
	for k, v := range overrides {
		switch k {
		case "type":
			sig.Type = v.(string)
		case "key_id":
			sig.KeyID = v.(string)
		case "timestamp":
			sig.Timestamp = v.(time.Time)
		case "verified":
			sig.Verified = v.(bool)
		}
	}

	return sig
}

// MockVulnerability creates a mock vulnerability for testing
func MockVulnerability(overrides map[string]interface{}) map[string]interface{} {
	vuln := map[string]interface{}{
		"id":          "CVE-2024-" + generateID(),
		"package":     "openssl",
		"version":     "1.1.1",
		"pkg_type":    "os-pkgs",
		"severity":    "high",
		"cvss":        7.5,
		"installed":   "1.1.1",
		"fixed":       "1.1.1k",
		"description": "Buffer overflow vulnerability",
		"references":  []string{"https://nvd.nist.gov/vuln/detail/CVE-2024-1234"},
	}

	// Apply overrides
	for k, v := range overrides {
		vuln[k] = v
	}

	return vuln
}

// WriteTempFile writes content to a temporary file
func WriteTempFile(t *testing.T, prefix, suffix string, content []byte) string {
	t.Helper()
	file, err := os.CreateTemp("", prefix+"*"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}

// ReadJSONFile reads and parses a JSON file
func ReadJSONFile(t *testing.T, path string, v interface{}) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

// EqualJSON checks if two JSON strings are equal
func EqualJSON(t *testing.T, expected, actual string) bool {
	t.Helper()
	var expectedObj, actualObj interface{}
	if err := json.Unmarshal([]byte(expected), &expectedObj); err != nil {
		t.Logf("Failed to parse expected JSON: %v", err)
		return false
	}
	if err := json.Unmarshal([]byte(actual), &actualObj); err != nil {
		t.Logf("Failed to parse actual JSON: %v", err)
		return false
	}
	return deepEqual(expectedObj, actualObj)
}

// deepEqual compares two interface{} values for equality
func deepEqual(a, b interface{}) bool {
	if a == b {
		return true
	}
	switch a := a.(type) {
	case map[string]interface{}:
		if b, ok := b.(map[string]interface{}); ok {
			if len(a) != len(b) {
				return false
			}
			for k, v := range a {
				if !deepEqual(v, b[k]) {
					return false
				}
			}
			return true
		}
	case []interface{}:
		if b, ok := b.([]interface{}); ok {
			if len(a) != len(b) {
				return false
			}
			for i := range a {
				if !deepEqual(a[i], b[i]) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// generateID generates a short random ID
func generateID() string {
	return "test-" + generateHex(4)
}

// generateHex generates a random hex string
func generateHex(n int) string {
	chars := "0123456789abcdef"
	result := make([]byte, n)
	for i := 0; i < n; i++ {
		result[i] = chars[(i*7+11)%len(chars)]
	}
	return string(result)
}
