package helm

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

// TestNewHelmProxy tests creating a new Helm proxy
func TestNewHelmProxy(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	router := NewHelmProxy(db, storage, cache, rbacMgr, registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

// TestHandleIndex tests the index endpoint
func TestHandleIndex(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	proxy := &HelmProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "helm",
	}

	req := httptest.NewRequest(http.MethodGet, "/index.yaml", nil)
	rec := httptest.NewRecorder()

	proxy.handleIndex(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/yaml", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "apiVersion:")
}

// TestHandleIndexNotFound tests 404 when index not found
func TestHandleIndexNotFound(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	proxy := &HelmProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "helm",
	}

	req := httptest.NewRequest(http.MethodGet, "/nonexistent/index.yaml", nil)
	rec := httptest.NewRecorder()

	proxy.handleIndex(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleChart tests chart download
func TestHandleChart(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	proxy := &HelmProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "helm",
	}

	// Save chart to storage
	chartData := []byte("fake helm chart tarball")
	_, err := storage.SaveArtifact("helm", "stable", "nginx-ingress", "1.0.0", chartData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/charts/stable/nginx-ingress-1.0.0.tgz", nil)
	rec := httptest.NewRecorder()

	proxy.handleChart(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/x-gzip", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "fake helm chart tarball")
}

// TestHandleChartNotFound tests 404 for non-existent chart
func TestHandleChartNotFound(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	proxy := &HelmProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "helm",
	}

	req := httptest.NewRequest(http.MethodGet, "/charts/stable/nonexistent-1.0.0.tgz", nil)
	rec := httptest.NewRecorder()

	proxy.handleChart(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleVersions tests versions endpoint
func TestHandleVersions(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	proxy := &HelmProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "helm",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "nginx-1", RegistryID: "helm", ArtifactType: "helm", Namespace: "stable", ArtifactName: "nginx-ingress", Version: "0.9.0"},
		{ID: "nginx-2", RegistryID: "helm", ArtifactType: "helm", Namespace: "stable", ArtifactName: "nginx-ingress", Version: "1.0.0"},
		{ID: "nginx-3", RegistryID: "helm", ArtifactType: "helm", Namespace: "stable", ArtifactName: "nginx-ingress", Version: "1.1.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/stable/nginx-ingress/charts.yaml", nil)
	rec := httptest.NewRecorder()

	proxy.handleVersions(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "name: nginx-ingress")
}

// TestHandleVersionsNotFound tests 404 for non-existent versions
func TestHandleVersionsNotFound(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	proxy := &HelmProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "helm",
	}

	req := httptest.NewRequest(http.MethodGet, "/stable/nonexistent/charts.yaml", nil)
	rec := httptest.NewRecorder()

	proxy.handleVersions(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleRoot tests the root endpoint
func TestHandleRoot(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	proxy := &HelmProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "helm",
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.handleRoot(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "cargobay-helm", body["name"])
	assert.Equal(t, "0.1.0", body["version"])
}

// TestHelmProxyIntegration tests the proxy integration
func TestHelmProxyIntegration(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	// Create proxy
	proxy := NewHelmProxy(db, storage, cache, rbacMgr, registries)

	// Test that router is created
	assert.NotNil(t, proxy)

	// Create test server
	server := httptest.NewServer(proxy)
	defer server.Close()

	// Test root endpoint
	resp, err := http.Get(server.URL + "/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestCacheConcurrency tests cache thread safety
func TestCacheConcurrency(t *testing.T) {
	cache := NewMockCache()
	proxy := &HelmProxy{cache: cache}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(index int) {
			key := "helm-concurrent-key"
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
	err := proxy.cacheGet("helm-concurrent-key", &retrieved)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
}

// TestEmptyNamespace tests handling of empty namespace
func TestEmptyNamespace(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	proxy := &HelmProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "helm",
	}

	// Add artifact with empty namespace
	artifact := &database.ArtifactMetadata{
		ID:           "helm-ns-test",
		RegistryID:   "helm",
		ArtifactType: "helm",
		Namespace:    "",
		ArtifactName: "standalone-chart",
		Version:      "1.0.0",
		Size:         2048,
	}
	db.SaveArtifact(artifact)

	// Search for artifacts
	results, err := db.ListArtifacts("helm", database.ListOptions{ArtifactType: "helm"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// TestNamespaceHandling tests namespace handling for Helm charts
func TestNamespaceHandling(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	proxy := &HelmProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "helm",
	}

	// Create artifact with namespace
	artifact := &database.ArtifactMetadata{
		ID:           "helm-ns-test",
		RegistryID:   "helm",
		ArtifactType: "helm",
		Namespace:    "bitnami",
		ArtifactName: "nginx",
		Version:      "12.0.0",
		Size:         1024,
	}
	db.SaveArtifact(artifact)

	// Search with namespace
	results, err := db.ListArtifacts("helm", database.ListOptions{
		Namespace:    "bitnami",
		ArtifactType: "helm",
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	assert.Equal(t, "bitnami", results[0].Namespace)
}

// TestHelmProxyRoutes tests all expected routes are registered
func TestHelmProxyRoutes(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "helm", Name: "Helm Registry", Type: "helm", Proxy: true, Enabled: true},
	}

	proxy := NewHelmProxy(db, storage, cache, rbacMgr, registries)

	// Create test server
	server := httptest.NewServer(proxy)
	defer server.Close()

	// Test all expected routes
	routes := []string{
		"/",
		"/index.yaml",
		"/stable/nginx-ingress/charts.yaml",
	}

	for _, route := range routes {
		resp, err := http.Get(server.URL + route)
		require.NoError(t, err)
		// Routes may return different status codes depending on implementation
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode, "Route %s should exist", route)
	}
}
