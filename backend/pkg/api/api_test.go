package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/cargobay/backend/pkg/database"
	"github.com/anthropics/cargobay/backend/pkg/rbac"
	"github.com/anthropics/cargobay/backend/pkg/vulnerability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockDatabase is a mock implementation of the Database interface
type MockDatabase struct{}

// MockRBAC is a mock implementation of the RBAC interface
type MockRBAC struct{}

// MockVulnerabilityScanner is a mock implementation of the VulnerabilityScanner interface
type MockVulnerabilityScanner struct{}

// MockStorageAdapter is a mock implementation of the StorageAdapter interface
type MockStorageAdapter struct{}

// TestServer creates a test server for API testing
func TestServer(t *testing.T) {
	// Create mock dependencies
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	// Create the server
	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	assert.NotNil(t, server)
	assert.NotNil(t, server.router)
}

// TestAPIHealthCheck tests the health check endpoint
func TestAPIHealthCheck(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var response map[string]string
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "ok", response["status"])
	assert.Contains(t, response, "timestamp")
}

// TestAPIVersion tests the version endpoint
func TestAPIVersion(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]string
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "version")
	assert.Contains(t, response, "build")
}

// TestAPIListArtifacts tests the list artifacts endpoint
func TestAPIListArtifacts(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "artifacts")
	assert.Contains(t, response, "total")
	assert.Contains(t, response, "hasMore")
}

// TestAPIGetArtifact tests the get artifact endpoint
func TestAPIGetArtifact(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/artifact-123", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "id")
	assert.Contains(t, response, "registryId")
	assert.Contains(t, response, "artifactType")
}

// TestAPICreateArtifact tests the create artifact endpoint
func TestAPICreateArtifact(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request body
	body := map[string]interface{}{
		"registryId":      "test-registry",
		"artifactType":    "docker",
		"namespace":       "library",
		"artifactName":    "nginx",
		"version":         "1.21.0",
		"digest":          "sha256:abc123",
		"digestAlgorithm": "sha256",
		"size":            1024,
		"metadata":        map[string]interface{}{"key": "value"},
		"tags":            []string{"latest"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusCreated, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "id")
}

// TestAPIDeleteArtifact tests the delete artifact endpoint
func TestAPIDeleteArtifact(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/artifact-123", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "message")
}

// TestAPISearchArtifacts tests the search artifacts endpoint
func TestAPISearchArtifacts(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request with query
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=nginx", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "results")
	assert.Contains(t, response, "total")
}

// TestAPIListRegistries tests the list registries endpoint
func TestAPIListRegistries(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/registries", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "registries")
}

// TestAPICreateRegistry tests the create registry endpoint
func TestAPICreateRegistry(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request body
	body := map[string]interface{}{
		"id":       "npm-registry",
		"name":     "NPM Registry",
		"url":      "https://registry.npmjs.org",
		"type":     "npm",
		"proxy":    true,
		"enabled":  true,
		"priority": 1,
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/registries", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusCreated, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "id")
}

// TestAPIListUsers tests the list users endpoint
func TestAPIListUsers(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "users")
}

// TestAPIListRoles tests the list roles endpoint
func TestAPIListRoles(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/roles", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "roles")
}

// TestAPIGetRole tests the get role endpoint
func TestAPIGetRole(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/roles/admin", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "id")
	assert.Contains(t, response, "name")
}

// TestAPIListPermissions tests the list permissions endpoint
func TestAPIListPermissions(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "permissions")
}

// TestAPIGetPermission tests the get permission endpoint
func TestAPIGetPermission(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions/artifact:read", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "id")
	assert.Contains(t, response, "name")
}

// TestAPIGetUserRoles tests the get user roles endpoint
func TestAPIGetUserRoles(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-123/roles", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "roles")
}

// TestAPIGetUserPermissions tests the get user permissions endpoint
func TestAPIGetUserPermissions(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-123/permissions", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "permissions")
}

// TestAPIScanArtifact tests the scan artifact endpoint
func TestAPIScanArtifact(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/artifact-123/scan", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "artifactId")
	assert.Contains(t, response, "severity")
}

// TestAPIReplicationStatus tests the replication status endpoint
func TestAPIReplicationStatus(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/api/v1/replication/status", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "status")
	assert.Contains(t, response, "regions")
}

// TestAPIReplicationSync tests the replication sync endpoint
func TestAPIReplicationSync(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodPost, "/api/v1/replication/sync", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "message")
}

// TestAPIMetrics tests the metrics endpoint
func TestAPIMetrics(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	// Serve request
	server.router.ServeHTTP(rec, req)

	// Verify response
	assert.Equal(t, http.StatusOK, rec.Code)

	// Metrics should return Prometheus-style format
	assert.Contains(t, rec.Body.String(), "cargobay_")
}

// TestAPIRateLimiting tests rate limiting middleware
func TestAPIRateLimiting(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create multiple requests
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		// Should all succeed with 200
		assert.Equal(t, http.StatusOK, rec.Code)
	}
}

// TestAPIErrorHandling tests error handling
func TestAPIErrorHandling(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Test with invalid method
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/artifact-123", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	// Should return 404 or 405
	assert.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, rec.Code)
}

// TestAPIContentType tests content type headers
func TestAPIContentType(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	// Verify content type
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

// TestAPIPagination tests pagination support
func TestAPIPagination(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request with pagination params
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts?limit=10&offset=20", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	// Should succeed
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAPISorting tests sorting support
func TestAPISorting(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request with sorting params
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts?orderBy=created&order=desc", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	// Should succeed
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAPIFiltering tests filtering support
func TestAPIFiltering(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request with filter params
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts?artifactType=docker&namespace=library", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	// Should succeed
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAPIServerRoutes tests that all expected routes are registered
func TestAPIServerRoutes(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Test that the server has a router
	assert.NotNil(t, server.router)

	// Test that all expected routes are accessible
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/health"},
		{http.MethodGet, "/version"},
		{http.MethodGet, "/api/v1/artifacts"},
		{http.MethodPost, "/api/v1/artifacts"},
		{http.MethodGet, "/api/v1/artifacts/artifact-123"},
		{http.MethodDelete, "/api/v1/artifacts/artifact-123"},
		{http.MethodGet, "/api/v1/search"},
		{http.MethodGet, "/api/v1/registries"},
		{http.MethodPost, "/api/v1/registries"},
		{http.MethodGet, "/api/v1/users"},
		{http.MethodGet, "/api/v1/roles"},
		{http.MethodGet, "/api/v1/roles/admin"},
		{http.MethodGet, "/api/v1/permissions"},
		{http.MethodGet, "/api/v1/permissions/artifact:read"},
		{http.MethodGet, "/metrics"},
	}

	for _, route := range routes {
		req := httptest.NewRequest(route.method, route.path, nil)
		rec := httptest.NewRecorder()

		server.router.ServeHTTP(rec, req)

		// Should not return 404 for registered routes
		assert.NotEqual(t, http.StatusNotFound, rec.Code,
			"Route %s %s should be registered", route.method, route.path)
	}
}

// TestAPIServerMiddleware tests middleware chain
func TestAPIServerMiddleware(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Test health check (should pass through all middleware)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	// Should succeed
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAPIServerOptions tests CORS preflight handling
func TestAPIServerOptions(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create OPTIONS request (CORS preflight)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/artifacts", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	// Should handle preflight
	assert.Contains(t, []int{http.StatusOK, http.StatusNoContent}, rec.Code)
}

// TestAPIServerHead tests HEAD method handling
func TestAPIServerHead(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create HEAD request
	req := httptest.NewRequest(http.MethodHead, "/health", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	// Should return headers without body
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
}

// TestAPIJSONResponse tests JSON response formatting
func TestAPIJSONResponse(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	// Verify response is valid JSON
	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	// Should have required fields
	assert.Contains(t, response, "status")
}

// TestAPIErrorResponse tests error response formatting
func TestAPIErrorResponse(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRBAC := &MockRBAC{}
	mockScanner := &MockVulnerabilityScanner{}

	server := NewServer(mockDB, mockRBAC, mockScanner, nil)

	// Test with non-existent resource
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/non-existent", nil)
	rec := httptest.NewRecorder()

	server.router.ServeHTTP(rec, req)

	// Verify error response
	assert.Equal(t, http.StatusNotFound, rec.Code)

	var response map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response, "error")
	assert.Contains(t, response, "message")
}

// Connect implements StorageAdapter interface
func (m *MockStorageAdapter) Connect() error { return nil }

// Disconnect implements StorageAdapter interface
func (m *MockStorageAdapter) Disconnect() error { return nil }

// SaveArtifact implements StorageAdapter interface
func (m *MockStorageAdapter) SaveArtifact(registryID, namespace, artifactName, version string, data []byte) (string, error) {
	return "test-key", nil
}

// GetArtifact implements StorageAdapter interface
func (m *MockStorageAdapter) GetArtifact(registryID, namespace, artifactName, version string) ([]byte, error) {
	return []byte("test data"), nil
}

// DeleteArtifact implements StorageAdapter interface
func (m *MockStorageAdapter) DeleteArtifact(registryID, namespace, artifactName, version string) error {
	return nil
}

// ArtifactExists implements StorageAdapter interface
func (m *MockStorageAdapter) ArtifactExists(registryID, namespace, artifactName, version string) (bool, error) {
	return true, nil
}
