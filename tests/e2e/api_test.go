package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropics/cargobay/backend/pkg/api"
	"github.com/anthropics/cargobay/backend/pkg/cache"
	"github.com/anthropics/cargobay/backend/pkg/database"
	"github.com/anthropics/cargobay/backend/pkg/rbac"
	"github.com/anthropics/cargobay/backend/pkg/storage"
	"github.com/anthropics/cargobay/backend/pkg/vulnerability"
	"github.com/stretchr/testify/assert"
)

// TestAPIHealth tests the health endpoint
func TestAPIHealth(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)
	assert.Equal(t, "ok", body["status"])
}

// TestAPIVersion tests the version endpoint
func TestAPIVersion(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	resp, err := http.Get(server.URL + "/version")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)
	assert.Equal(t, "1.0.0", body["version"])
}

// TestAPIListArtifacts tests listing artifacts
func TestAPIListArtifacts(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	// Create a test artifact
	artifact := &database.ArtifactMetadata{
		ID:              "test-artifact-1",
		RegistryID:      "test-registry",
		ArtifactType:    "docker",
		Namespace:       "library",
		ArtifactName:    "nginx",
		Version:         "1.0.0",
		Digest:          "sha256:test123",
		DigestAlgorithm: "sha256",
		Size:            1024,
	}

	err := db.SaveArtifact(artifact)
	assert.NoError(t, err)

	// List artifacts
	resp, err := http.Get(server.URL + "/api/v1/artifacts")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)

	artifacts := body["artifacts"].([]interface{})
	assert.GreaterOrEqual(t, len(artifacts), 1)
}

// TestAPIGetArtifact tests getting a single artifact
func TestAPIGetArtifact(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	// Create a test artifact
	artifact := &database.ArtifactMetadata{
		ID:              "test-artifact-2",
		RegistryID:      "test-registry",
		ArtifactType:    "docker",
		Namespace:       "library",
		ArtifactName:    "nginx",
		Version:         "1.0.0",
		Digest:          "sha256:test123",
		DigestAlgorithm: "sha256",
		Size:            1024,
	}

	err := db.SaveArtifact(artifact)
	assert.NoError(t, err)

	// Get artifact by ID
	resp, err := http.Get(server.URL + "/api/v1/artifacts/" + artifact.ID)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)
	assert.Equal(t, artifact.ID, body["id"])
}

// TestAPISearch tests the search endpoint
func TestAPISearch(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	// Create test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "search-test-1", RegistryID: "test", ArtifactType: "docker", Namespace: "library", ArtifactName: "nginx", Version: "1.0.0"},
		{ID: "search-test-2", RegistryID: "test", ArtifactType: "npm", Namespace: "", ArtifactName: "express", Version: "4.0.0"},
	}

	for _, a := range artifacts {
		err := db.SaveArtifact(a)
		assert.NoError(t, err)
	}

	// Search
	resp, err := http.Get(server.URL + "/api/v1/search?q=nginx")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)
	results := body["results"].([]interface{})
	assert.GreaterOrEqual(t, len(results), 1)
}

// TestAPIRegistries tests registry endpoints
func TestAPIRegistries(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	// List registries
	resp, err := http.Get(server.URL + "/api/v1/registries")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)
	assert.NotNil(t, body["registries"])
}

// TestAPIRoles tests role endpoints
func TestAPIRoles(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	// List roles
	resp, err := http.Get(server.URL + "/api/v1/roles")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)

	roles := body["roles"].([]interface{})
	assert.GreaterOrEqual(t, len(roles), 5) // admin, operator, developer, viewer, auditor
}

// TestAPIMetrics tests the metrics endpoint
func TestAPIMetrics(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	// Get metrics
	resp, err := http.Get(server.URL + "/metrics")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Metrics should be text/plain format
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/plain")

	body := resp.Body
	assert.NotNil(t, body)
}

// TestAPIArtifactsCRUD tests artifact create, read, delete operations
func TestAPIArtifactsCRUD(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	// Create artifact
	createReq := map[string]interface{}{
		"registry_id":     "test-registry",
		"artifact_type":   "docker",
		"namespace":       "library",
		"artifact_name":   "test-crud",
		"version":         "1.0.0",
		"digest":          "sha256:crudtest",
		"digest_algorithm": "sha256",
		"size":            2048,
	}

	jsonData, _ := json.Marshal(createReq)
	resp, err := http.Post(server.URL+"/api/v1/artifacts", "application/json", jsonData)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var created map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&created)
	assert.NoError(t, err)
	artifactID := created["id"].(string)

	// Read artifact
	resp, err = http.Get(server.URL + "/api/v1/artifacts/" + artifactID)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var read map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&read)
	assert.NoError(t, err)
	assert.Equal(t, artifactID, read["id"])

	// Delete artifact
	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/artifacts/"+artifactID, nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err = client.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify deletion
	resp, err = http.Get(server.URL + "/api/v1/artifacts/" + artifactID)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestAPISearchCursor tests cursor-based pagination
func TestAPISearchCursor(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	// Create multiple artifacts for pagination
	for i := 0; i < 150; i++ {
		artifact := &database.ArtifactMetadata{
			ID:              "cursor-test-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			RegistryID:      "test",
			ArtifactType:    "docker",
			Namespace:       "library",
			ArtifactName:    "cursor-test",
			Version:         "1.0.0",
		}
		db.SaveArtifact(artifact)
	}

	// List with limit
	resp, err := http.Get(server.URL + "/api/v1/artifacts?limit=10")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)
	artifacts := body["artifacts"].([]interface{})
	assert.Equal(t, 10, len(artifacts))
	assert.True(t, body["hasMore"].(bool))
}

// TestAPIUserEndpoints tests user management endpoints
func TestAPIUserEndpoints(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	// List users
	resp, err := http.Get(server.URL + "/api/v1/users")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)
	assert.NotNil(t, body["users"])
}

// TestAPIReplicationEndpoints tests replication endpoints
func TestAPIReplicationEndpoints(t *testing.T) {
	db := database.New("postgres://localhost:5432/test")
	rbacMgr := rbac.New(db)
	scanner := vulnerability.NewScanner(db, nil, nil)
	storageAdapter, _ := storage.New("local", map[string]string{"path": "/tmp/cargobay-test"})
	cacheClient, _ := cache.New("redis", "redis://localhost:6379")

	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)
	server := httptest.NewServer(apiServer)
	defer server.Close()

	// Get replication status
	resp, err := http.Get(server.URL + "/api/v1/replication/status")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)
	assert.Equal(t, "running", body["status"])
}
