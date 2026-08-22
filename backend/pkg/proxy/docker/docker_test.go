package docker

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
)

// MockDatabase is a simple mock database for testing
type MockDatabase struct {
	Artifacts   map[string]*database.ArtifactMetadata
	Registries  map[string]*database.RegistryConfig
	mu          sync.RWMutex
}

func NewMockDatabase() *MockDatabase {
	return &MockDatabase{
		Artifacts:  make(map[string]*database.ArtifactMetadata),
		Registries: make(map[string]*database.RegistryConfig),
	}
}

func (m *MockDatabase) SaveArtifact(a *database.ArtifactMetadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Artifacts[a.ID] = a
	return nil
}

func (m *MockDatabase) ListArtifacts(registry string, opts database.ListOptions) ([]database.ArtifactMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []database.ArtifactMetadata
	for _, a := range m.Artifacts {
		if opts.ArtifactType != "" && a.ArtifactType != opts.ArtifactType {
			continue
		}
		if opts.Namespace != "" && a.Namespace != opts.Namespace {
			continue
		}
		result = append(result, *a)
	}
	return result, nil
}

func (m *MockDatabase) SaveRegistry(r *database.RegistryConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Registries[r.ID] = r
	return nil
}

func (m *MockDatabase) GetRegistry(id string) (*database.RegistryConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Registries[id], nil
}

// MockStorageAdapter is a mock storage for testing
type MockStorageAdapter struct {
	Artifacts map[string][]byte
	mu        sync.RWMutex
}

func NewMockStorage() *MockStorageAdapter {
	return &MockStorageAdapter{
		Artifacts: make(map[string][]byte),
	}
}

func (m *MockStorageAdapter) Connect() error                      { return nil }
func (m *MockStorageAdapter) Disconnect() error                   { return nil }
func (m *MockStorageAdapter) SaveArtifact(reg, ns, name, ver string, data []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Artifacts[reg+"/"+ns+"/"+name+"/"+ver] = data
	return "", nil
}
func (m *MockStorageAdapter) GetArtifact(reg, ns, name, ver string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Artifacts[reg+"/"+ns+"/"+name+"/"+ver], nil
}
func (m *MockStorageAdapter) DeleteArtifact(reg, ns, name, ver string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Artifacts, reg+"/"+ns+"/"+name+"/"+ver)
	return nil
}
func (m *MockStorageAdapter) ArtifactExists(reg, ns, name, ver string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.Artifacts[reg+"/"+ns+"/"+name+"/"+ver]
	return ok, nil
}

// MockCache is a mock cache for testing
type MockCache struct {
	Data map[string][]byte
	mu   sync.RWMutex
}

func NewMockCache() *MockCache {
	return &MockCache{
		Data: make(map[string][]byte),
	}
}

func (m *MockCache) Get(key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if data, ok := m.Data[key]; ok {
		if val, ok := value.(*[]byte); ok {
			*val = data
		}
		return nil
	}
	return cache.ErrCacheMiss
}

func (m *MockCache) Set(key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if data, ok := value.([]byte); ok {
		m.Data[key] = data
	}
}

func (m *MockCache) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Data, key)
}

func (m *MockCache) Close() error { return nil }

// TestNewDockerProxy tests creating a new Docker proxy
func TestNewDockerProxy(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	router := NewDockerProxy(db, storage, cache, rbacMgr, registries, nil)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

// TestHandleV1Ping tests v1 ping endpoint
func TestHandleV1Ping(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/_ping", nil)
	rec := httptest.NewRecorder()

	proxy.handleV1Ping(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", rec.Body.String()) // Docker v1 ping returns empty body
	assert.Equal(t, "1", rec.Header().Get("X-Docker-Registry-Version"))
}

// TestHandleV2Ping tests v2 ping endpoint
func TestHandleV2Ping(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	req := httptest.NewRequest(http.MethodGet, "/v2/_ping", nil)
	rec := httptest.NewRecorder()

	proxy.handleV2Ping(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "ok", body["status"])
}

// TestHandleCatalog tests catalog endpoint
func TestHandleCatalog(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "nginx", RegistryID: "docker", ArtifactType: "docker", Namespace: "", ArtifactName: "nginx", Version: "1.0.0"},
		{ID: "redis", RegistryID: "docker", ArtifactType: "docker", Namespace: "", ArtifactName: "redis", Version: "1.0.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/v2/_catalog", nil)
	rec := httptest.NewRecorder()

	proxy.handleCatalog(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	repositories := body["repositories"].([]interface{})
	assert.GreaterOrEqual(t, len(repositories), 2)
}

// TestHandleTags tests tags endpoint
func TestHandleTags(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "nginx-1", RegistryID: "docker", ArtifactType: "docker", Namespace: "", ArtifactName: "nginx", Version: "1.14.0"},
		{ID: "nginx-2", RegistryID: "docker", ArtifactType: "docker", Namespace: "", ArtifactName: "nginx", Version: "1.15.0"},
		{ID: "nginx-3", RegistryID: "docker", ArtifactType: "docker", Namespace: "", ArtifactName: "nginx", Version: "1.16.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/v2/library/nginx/tags/list", nil)
	rec := httptest.NewRecorder()

	proxy.handleTags(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	name := body["name"].(string)
	tags := body["tags"].([]interface{})
	assert.Equal(t, "nginx", name)
	assert.GreaterOrEqual(t, len(tags), 3)
}

// TestHandleManifest tests manifest endpoint
func TestHandleManifest(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	// Add test artifact
	artifact := &database.ArtifactMetadata{
		ID:           "nginx-manifest",
		RegistryID:   "docker",
		ArtifactType: "docker",
		Namespace:    "",
		ArtifactName: "nginx",
		Version:      "1.14.0",
		Digest:       "sha256:abc123",
		Size:         1024,
	}
	db.SaveArtifact(artifact)

	// Save manifest to storage
	manifest := []byte(`{"schemaVersion": 2, "mediaType": "application/vnd.docker.distribution.manifest.v2+json"}`)
	_, err := storage.SaveArtifact("docker", "", "nginx", "1.14.0", manifest)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/v2/library/nginx/manifests/1.14.0", nil)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	rec := httptest.NewRecorder()

	proxy.handleManifest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/vnd.docker.distribution.manifest.v2+json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "schemaVersion")
}

// TestHandleManifestNotFound tests 404 for non-existent manifest
func TestHandleManifestNotFound(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	req := httptest.NewRequest(http.MethodGet, "/v2/library/nonexistent/manifests/1.0.0", nil)
	rec := httptest.NewRecorder()

	proxy.handleManifest(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleBlob tests blob download
func TestHandleBlob(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	// Save blob to storage
	blobData := []byte("fake blob data")
	_, err := storage.SaveArtifact("docker", "", "nginx", "sha256:abc123", blobData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/v2/library/nginx/blobs/sha256:abc123", nil)
	rec := httptest.NewRecorder()

	proxy.handleBlob(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, blobData, rec.Body.Bytes())
}

// TestHandleBlobNotFound tests 404 for non-existent blob
func TestHandleBlobNotFound(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	req := httptest.NewRequest(http.MethodGet, "/v2/library/nginx/blobs/sha256:nonexistent", nil)
	rec := httptest.NewRecorder()

	proxy.handleBlob(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleLayers tests layers endpoint
func TestHandleLayers(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "layer-1", RegistryID: "docker", ArtifactType: "docker", Namespace: "", ArtifactName: "layer-test", Version: "v1"},
		{ID: "layer-2", RegistryID: "docker", ArtifactType: "docker", Namespace: "", ArtifactName: "layer-test", Version: "v2"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/v2/library/layer-test/layers", nil)
	rec := httptest.NewRecorder()

	proxy.handleLayers(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	versions := body["versions"].([]interface{})
	assert.GreaterOrEqual(t, len(versions), 2)
}

// TestDockerProxyIntegration tests the proxy integration
func TestDockerProxyIntegration(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	// Create proxy
	proxy := NewDockerProxy(db, storage, cache, rbacMgr, registries, nil)

	// Test that router is created
	assert.NotNil(t, proxy)

	// Create test server
	server := httptest.NewServer(proxy)
	defer server.Close()

	// Test v2 ping endpoint
	resp, err := http.Get(server.URL + "/v2/_ping")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestScopedRepository tests handling of scoped repositories
func TestScopedRepository(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	// Add scoped repository artifact
	artifact := &database.ArtifactMetadata{
		ID:           "gcr-test",
		RegistryID:   "docker",
		ArtifactType: "docker",
		Namespace:    "gcr.io",
		ArtifactName: "my-project/my-app",
		Version:      "1.0.0",
		Size:         1024,
	}
	db.SaveArtifact(artifact)

	req := httptest.NewRequest(http.MethodGet, "/v2/gcr.io/my-project%2Fmy-app/tags/list", nil)
	rec := httptest.NewRecorder()

	proxy.handleTags(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "gcr.io/my-project/my-app", body["name"])
}

// TestCacheConcurrency tests cache thread safety
func TestCacheConcurrency(t *testing.T) {
	cache := NewMockCache()
	proxy := &DockerProxy{cache: cache}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(index int) {
			key := "docker-concurrent-key"
			data := []byte(`{"value": ` + string(rune('0'+index)) + `}`)
			proxy.cacheSet(key, data)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify last write wins
	var retrieved []byte
	err := proxy.cacheGet("docker-concurrent-key", &retrieved)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
}

// TestEmptyNamespace tests handling of empty namespace (default library)
func TestEmptyNamespace(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	// Add artifact with empty namespace (library)
	artifact := &database.ArtifactMetadata{
		ID:           "library-test",
		RegistryID:   "docker",
		ArtifactType: "docker",
		Namespace:    "",
		ArtifactName: "alpine",
		Version:      "3.14",
		Size:         512,
	}
	db.SaveArtifact(artifact)

	// Search for artifacts
	results, err := db.ListArtifacts("docker", database.ListOptions{ArtifactType: "docker"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// TestDockerProxyRoutes tests all expected routes are registered
func TestDockerProxyRoutes(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "docker", Name: "Docker Registry", Type: "docker", Proxy: true, Enabled: true},
	}

	proxy := NewDockerProxy(db, storage, cache, rbacMgr, registries, nil)

	// Create test server
	server := httptest.NewServer(proxy)
	defer server.Close()

	// Test all expected routes
	routes := []string{
		"/v1/_ping",
		"/v2/_ping",
		"/v2/_catalog",
		"/v2/library/nginx/tags/list",
		"/v2/library/nginx/manifests/1.0.0",
		"/v2/library/nginx/blobs/sha256:abc",
	}

	for _, route := range routes {
		resp, err := http.Get(server.URL + route)
		require.NoError(t, err)
		// Routes may return different status codes depending on implementation
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode, "Route %s should exist", route)
	}
}
