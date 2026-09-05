package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
)

// withChiParams attaches URL params the way chi's router would, so handlers
// using chi.URLParam(r, ...) can be unit-tested without a full router.
func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// MockDatabase is a mock implementation of database.Database for testing
type MockDatabase struct {
	searchArtifactsFunc       func(string, database.SearchOptions) ([]database.ArtifactMetadata, error)
	getArtifactByParamsFunc   func(string, string, string, string) (*database.ArtifactMetadata, error)
	artifacts                 []database.ArtifactMetadata
}

// SearchArtifacts implements database.Database interface
func (m *MockDatabase) SearchArtifacts(query string, opts database.SearchOptions) ([]database.ArtifactMetadata, error) {
	if m.searchArtifactsFunc != nil {
		return m.searchArtifactsFunc(query, opts)
	}
	return m.artifacts, nil
}

// GetArtifactByParams implements database.Database interface
func (m *MockDatabase) GetArtifactByParams(registryID, namespace, artifactName, version string) (*database.ArtifactMetadata, error) {
	if m.getArtifactByParamsFunc != nil {
		return m.getArtifactByParamsFunc(registryID, namespace, artifactName, version)
	}
	for _, a := range m.artifacts {
		if a.RegistryID == registryID && a.Namespace == namespace && a.ArtifactName == artifactName && a.Version == version {
			return &a, nil
		}
	}
	return nil, nil
}

// MockStorageAdapter is a mock implementation of storage.StorageAdapter for testing
type MockStorageAdapter struct {
	getArtifactFunc func(string, string, string, string) ([]byte, error)
	artifacts       map[string][]byte
}

// GetArtifact implements storage.StorageAdapter interface
func (m *MockStorageAdapter) GetArtifact(registryID, namespace, artifactName, version string) ([]byte, error) {
	if m.getArtifactFunc != nil {
		return m.getArtifactFunc(registryID, namespace, artifactName, version)
	}
	key := fmt.Sprintf("%s/%s/%s/%s", registryID, namespace, artifactName, version)
	return m.artifacts[key], nil
}

// SaveArtifact implements storage.StorageAdapter interface
func (m *MockStorageAdapter) SaveArtifact(registryID, namespace, artifactName, version string, data []byte) (string, error) {
	return "", nil
}

// GetArtifactStream implements storage.StorageAdapter interface
func (m *MockStorageAdapter) GetArtifactStream(registryID, namespace, artifactName, version string) (io.ReadCloser, error) {
	data, err := m.GetArtifact(registryID, namespace, artifactName, version)
	if err != nil || data == nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// SaveArtifactStream implements storage.StorageAdapter interface
func (m *MockStorageAdapter) SaveArtifactStream(registryID, namespace, artifactName, version string, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return m.SaveArtifact(registryID, namespace, artifactName, version, data)
}

// DeleteArtifact implements storage.StorageAdapter interface
func (m *MockStorageAdapter) DeleteArtifact(registryID, namespace, artifactName, version string) error {
	return nil
}

// ArtifactExists implements storage.StorageAdapter interface
func (m *MockStorageAdapter) ArtifactExists(registryID, namespace, artifactName, version string) (bool, error) {
	key := fmt.Sprintf("%s/%s/%s/%s", registryID, namespace, artifactName, version)
	_, ok := m.artifacts[key]
	return ok, nil
}

// Upload implements storage.StorageAdapter interface
func (m *MockStorageAdapter) Upload(bucket, key string, data []byte, contentType string) error {
	return nil
}

// Download implements storage.StorageAdapter interface
func (m *MockStorageAdapter) Download(bucket, key string) ([]byte, error) {
	return nil, nil
}

// DeleteFile implements storage.StorageAdapter interface
func (m *MockStorageAdapter) DeleteFile(bucket, key string) error {
	return nil
}

// ListFiles implements storage.StorageAdapter interface
func (m *MockStorageAdapter) ListFiles(bucket, prefix string) ([]string, error) {
	return []string{}, nil
}

// Connect implements storage.StorageAdapter interface
func (m *MockStorageAdapter) Connect() error {
	return nil
}

// Disconnect implements storage.StorageAdapter interface
func (m *MockStorageAdapter) Disconnect() error {
	return nil
}

// MockCache is a mock implementation of cache.Cache for testing
type MockCache struct {
	data        map[string]interface{}
	getFunc     func(string, interface{}) error
	setFunc     func(string, interface{}) error
	setWithTTLFunc func(string, interface{}, time.Duration) error
}

// Get implements cache.Cache interface
func (m *MockCache) Get(key string, value interface{}) error {
	if m.getFunc != nil {
		return m.getFunc(key, value)
	}
	if v, ok := m.data[key]; ok {
		if b, ok := value.(*map[string]interface{}); ok {
			*b = v.(map[string]interface{})
		}
	}
	// Matches cache.Cache.Get: a miss is not an error, it just leaves value unset.
	return nil
}

// Set implements cache.Cache interface
func (m *MockCache) Set(key string, value interface{}) error {
	if m.setFunc != nil {
		return m.setFunc(key, value)
	}
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	m.data[key] = value
	return nil
}

// SetWithTTL implements cache.Cache interface
func (m *MockCache) SetWithTTL(key string, value interface{}, ttl time.Duration) error {
	if m.setWithTTLFunc != nil {
		return m.setWithTTLFunc(key, value, ttl)
	}
	if m.data == nil {
		m.data = make(map[string]interface{})
	}
	m.data[key] = value
	return nil
}

// TestNewProxyManager tests creating a new ProxyManager
func TestNewProxyManager(t *testing.T) {
	db := &MockDatabase{}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", URL: "https://registry.npmjs.org"},
	}

	pm := New(db, storage, cache, registries)

	assert.NotNil(t, pm)
	assert.Equal(t, db, pm.db)
	assert.Equal(t, storage, pm.storage)
	assert.Equal(t, cache, pm.cache)
	assert.Equal(t, registries, pm.registries)
}

// TestListRegistries tests the ListRegistries handler
func TestListRegistries(t *testing.T) {
	registries := []database.RegistryConfig{
		{
			ID:          "npm",
			Name:        "NPM Registry",
			URL:         "https://registry.npmjs.org",
			Type:        "npm",
			Proxy:       true,
			Enabled:     true,
			Priority:    1,
		},
		{
			ID:          "maven",
			Name:        "Maven Central",
			URL:         "https://repo.maven.apache.org",
			Type:        "maven",
			Proxy:       true,
			Enabled:     true,
			Priority:    2,
		},
	}
	db := &MockDatabase{}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, registries)

	req := httptest.NewRequest(http.MethodGet, "/registries", nil)
	w := httptest.NewRecorder()

	pm.ListRegistries(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Contains(t, response, "registries")
	registriesResponse := response["registries"].([]interface{})
	assert.Equal(t, 2, len(registriesResponse))

	// Check first registry
	firstRegistry := registriesResponse[0].(map[string]interface{})
	assert.Equal(t, "npm", firstRegistry["id"])
	assert.Equal(t, "NPM Registry", firstRegistry["name"])
}

// TestSearchArtifactsCacheHit tests search with cache hit
func TestSearchArtifactsCacheHit(t *testing.T) {
	cacheData := map[string]interface{}{
		"query":   "test",
		"results": []interface{}{},
		"total":   0,
		"limit":   100,
		"offset":  0,
	}

	db := &MockDatabase{}
	storage := &MockStorageAdapter{}
	cache := &MockCache{
		data: map[string]interface{}{
			"search:v2:test:::100:0": cacheData,
		},
	}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/search?q=test", nil)
	w := httptest.NewRecorder()

	pm.SearchArtifacts(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "HIT", w.Header().Get("X-Cache"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "test", response["query"])
}

// TestSearchArtifactsCacheMiss tests search with cache miss
func TestSearchArtifactsCacheMiss(t *testing.T) {
	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{
			{
				RegistryID:   "npm",
				Namespace:    "test",
				ArtifactName: "package",
				Version:      "1.0.0",
				Digest:       "sha256:abc123",
			},
		},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{
		data: make(map[string]interface{}),
	}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/search?q=test", nil)
	w := httptest.NewRecorder()

	pm.SearchArtifacts(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "MISS", w.Header().Get("X-Cache"))

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Contains(t, response, "results")
	assert.Equal(t, "test", response["query"])
}

// TestSearchArtifactsWithQuery tests search with query parameters
func TestSearchArtifactsWithQuery(t *testing.T) {
	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{
			{
				RegistryID:   "npm",
				Namespace:    "test",
				ArtifactName: "package",
				Version:      "1.0.0",
			},
		},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/search?q=package&registry=npm&type=npm&limit=50&offset=10", nil)
	w := httptest.NewRecorder()

	pm.SearchArtifacts(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "package", response["query"])
}

// TestSearchArtifactsWithDatabaseError tests search with database error
func TestSearchArtifactsWithDatabaseError(t *testing.T) {
	db := &MockDatabase{
		searchArtifactsFunc: func(string, database.SearchOptions) ([]database.ArtifactMetadata, error) {
			return nil, assert.AnError
		},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/search?q=test", nil)
	w := httptest.NewRecorder()

	pm.SearchArtifacts(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "Search failed")
}

// TestGetArtifactInfoSuccess tests GetArtifactInfo with successful lookup
func TestGetArtifactInfoSuccess(t *testing.T) {
	artifact := &database.ArtifactMetadata{
		RegistryID:   "npm",
		Namespace:    "test",
		ArtifactName: "package",
		Version:      "1.0.0",
		Digest:       "sha256:abc123",
	}

	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{*artifact},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/artifact/test/package/1.0.0?registry=npm", nil)
	req = withChiParams(req, map[string]string{
		"namespace":    "test",
		"artifactName": "package",
		"version":      "1.0.0",
	})
	w := httptest.NewRecorder()

	pm.GetArtifactInfo(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Contains(t, response, "artifact")
}

// TestGetArtifactInfoNotFound tests GetArtifactInfo with not found
func TestGetArtifactInfoNotFound(t *testing.T) {
	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/artifact/test/package/1.0.0", nil)
	req = withChiParams(req, map[string]string{
		"namespace":    "test",
		"artifactName": "package",
		"version":      "1.0.0",
	})
	w := httptest.NewRecorder()

	pm.GetArtifactInfo(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Artifact not found")
}

// TestGetArtifactInfoDatabaseError tests GetArtifactInfo with database error
func TestGetArtifactInfoDatabaseError(t *testing.T) {
	db := &MockDatabase{
		getArtifactByParamsFunc: func(string, string, string, string) (*database.ArtifactMetadata, error) {
			return nil, assert.AnError
		},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/artifact/test/package/1.0.0", nil)
	req = withChiParams(req, map[string]string{
		"namespace":    "test",
		"artifactName": "package",
		"version":      "1.0.0",
	})
	w := httptest.NewRecorder()

	pm.GetArtifactInfo(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "Failed to get artifact")
}

// TestDownloadArtifactSuccess tests DownloadArtifact with successful download
func TestDownloadArtifactSuccess(t *testing.T) {
	artifact := &database.ArtifactMetadata{
		RegistryID:   "npm",
		Namespace:    "test",
		ArtifactName: "package",
		Version:      "1.0.0",
		Digest:       "sha256:abc123",
	}

	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{*artifact},
	}
	storageData := []byte("artifact content")
	storage := &MockStorageAdapter{
		artifacts: map[string][]byte{
			"npm/test/package/1.0.0": storageData,
		},
	}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/download/test/package/1.0.0?registry=npm", nil)
	req = withChiParams(req, map[string]string{
		"namespace":    "test",
		"artifactName": "package",
		"version":      "1.0.0",
	})
	w := httptest.NewRecorder()

	pm.DownloadArtifact(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, w.Header().Get("Content-Disposition"), "package-1.0.0")
	assert.Contains(t, w.Header().Get("X-Content-Digest"), "sha256:abc123")
	assert.Equal(t, storageData, w.Body.Bytes())
}

// TestDownloadArtifactNotFound tests DownloadArtifact with not found
func TestDownloadArtifactNotFound(t *testing.T) {
	artifact := &database.ArtifactMetadata{
		RegistryID:   "npm",
		Namespace:    "test",
		ArtifactName: "package",
		Version:      "1.0.0",
		Digest:       "sha256:abc123",
	}

	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{*artifact},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/download/test/package/1.0.0?registry=npm", nil)
	req = withChiParams(req, map[string]string{
		"namespace":    "test",
		"artifactName": "package",
		"version":      "1.0.0",
	})
	w := httptest.NewRecorder()

	pm.DownloadArtifact(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Artifact data not found")
}

// TestDownloadArtifactStorageError tests DownloadArtifact with storage error
func TestDownloadArtifactStorageError(t *testing.T) {
	artifact := &database.ArtifactMetadata{
		RegistryID:   "npm",
		Namespace:    "test",
		ArtifactName: "package",
		Version:      "1.0.0",
	}

	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{*artifact},
	}
	storage := &MockStorageAdapter{
		getArtifactFunc: func(string, string, string, string) ([]byte, error) {
			return nil, assert.AnError
		},
	}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/download/test/package/1.0.0?registry=npm", nil)
	req = withChiParams(req, map[string]string{
		"namespace":    "test",
		"artifactName": "package",
		"version":      "1.0.0",
	})
	w := httptest.NewRecorder()

	pm.DownloadArtifact(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestProxyManagerMultipleRegistries tests proxy manager with multiple registries
func TestProxyManagerMultipleRegistries(t *testing.T) {
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm"},
		{ID: "maven", Name: "Maven Central", Type: "maven"},
		{ID: "docker", Name: "Docker Hub", Type: "docker"},
		{ID: "pypi", Name: "PyPI", Type: "pypi"},
	}
	db := &MockDatabase{}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, registries)

	assert.Equal(t, 4, len(pm.registries))

	req := httptest.NewRequest(http.MethodGet, "/registries", nil)
	w := httptest.NewRecorder()

	pm.ListRegistries(w, req)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	registriesResponse := response["registries"].([]interface{})
	assert.Equal(t, 4, len(registriesResponse))
}

// TestSearchArtifactsEmptyResult tests search with empty result
func TestSearchArtifactsEmptyResult(t *testing.T) {
	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/search?q=nonexistent", nil)
	w := httptest.NewRecorder()

	pm.SearchArtifacts(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	results := response["results"].([]interface{})
	assert.Empty(t, results)
}

// TestSearchArtifactsWithRegistryFilter tests search with registry filter
func TestSearchArtifactsWithRegistryFilter(t *testing.T) {
	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{
			{RegistryID: "npm", Namespace: "test", ArtifactName: "package", Version: "1.0.0"},
			{RegistryID: "maven", Namespace: "test", ArtifactName: "package", Version: "1.0.0"},
		},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/search?q=package&registry=npm", nil)
	w := httptest.NewRecorder()

	pm.SearchArtifacts(w, req)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	results := response["results"].([]interface{})
	// Results should include all artifacts, filtering is done by database
	assert.GreaterOrEqual(t, len(results), 1)
}

// TestSearchArtifactsCachingWithTTL tests search caching with TTL
func TestSearchArtifactsCachingWithTTL(t *testing.T) {
	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{
			{RegistryID: "npm", Namespace: "test", ArtifactName: "package", Version: "1.0.0"},
		},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{
		data: make(map[string]interface{}),
	}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/search?q=test", nil)
	w := httptest.NewRecorder()

	pm.SearchArtifacts(w, req)

	assert.Equal(t, "MISS", w.Header().Get("X-Cache"))

	// Second request should hit cache
	w2 := httptest.NewRecorder()
	pm.SearchArtifacts(w2, req)

	assert.Equal(t, "HIT", w2.Header().Get("X-Cache"))
}

// TestProxyManagerNilRegistries tests proxy manager with nil registries
func TestProxyManagerNilRegistries(t *testing.T) {
	db := &MockDatabase{}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	assert.Nil(t, pm.registries)

	req := httptest.NewRequest(http.MethodGet, "/registries", nil)
	w := httptest.NewRecorder()

	pm.ListRegistries(w, req)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	// A nil registries slice marshals to JSON null, not [], so it decodes
	// back as a nil interface{} rather than an empty []interface{}.
	assert.Empty(t, response["registries"])
}

// TestSearchArtifactsWithArtifactTypeFilter tests search with artifact type filter
func TestSearchArtifactsWithArtifactTypeFilter(t *testing.T) {
	db := &MockDatabase{
		artifacts: []database.ArtifactMetadata{
			{RegistryID: "npm", Namespace: "test", ArtifactName: "package", Version: "1.0.0", ArtifactType: "npm"},
			{RegistryID: "docker", Namespace: "test", ArtifactName: "image", Version: "1.0.0", ArtifactType: "docker"},
		},
	}
	storage := &MockStorageAdapter{}
	cache := &MockCache{}

	pm := New(db, storage, cache, nil)

	req := httptest.NewRequest(http.MethodGet, "/search?artifact_type=npm", nil)
	w := httptest.NewRecorder()

	pm.SearchArtifacts(w, req)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Contains(t, response, "results")
}
