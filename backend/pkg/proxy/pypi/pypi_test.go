package pypi

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

// TestNewPyPIProxy tests creating a new PyPI proxy
func TestNewPyPIProxy(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	router := NewPyPIProxy(db, storage, cache, rbacMgr, registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

// TestHandleRoot tests the root endpoint
func TestHandleRoot(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.handleRoot(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "cargobay-pypi", body["name"])
	assert.Equal(t, "0.1.0", body["version"])
}

// TestHandleSimpleIndex tests the simple index endpoint
func TestHandleSimpleIndex(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "requests", RegistryID: "pypi", ArtifactType: "pypi", Namespace: "", ArtifactName: "requests", Version: "2.28.0"},
		{ID: "flask", RegistryID: "pypi", ArtifactType: "pypi", Namespace: "", ArtifactName: "flask", Version: "2.0.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/simple/", nil)
	rec := httptest.NewRecorder()

	proxy.handleSimpleIndex(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "requests")
	assert.Contains(t, rec.Body.String(), "flask")
}

// TestHandlePackageIndex tests package index endpoint
func TestHandlePackageIndex(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "requests-1", RegistryID: "pypi", ArtifactType: "pypi", Namespace: "", ArtifactName: "requests", Version: "2.27.0"},
		{ID: "requests-2", RegistryID: "pypi", ArtifactType: "pypi", Namespace: "", ArtifactName: "requests", Version: "2.28.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/simple/requests/", nil)
	rec := httptest.NewRecorder()

	proxy.handlePackageIndex(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "requests")
}

// TestHandlePackageDownload tests package download
func TestHandlePackageDownload(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	// Save wheel to storage
	wheelData := []byte("fake wheel content")
	_, err := storage.SaveArtifact("pypi", "", "requests", "2.28.0", wheelData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/packages/requests-2.28.0-py3-none-any.whl", nil)
	rec := httptest.NewRecorder()

	proxy.handlePackageDownload(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "fake wheel content")
}

// TestHandlePackageDownloadNotFound tests 404 for non-existent package
func TestHandlePackageDownloadNotFound(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	req := httptest.NewRequest(http.MethodGet, "/packages/nonexistent-1.0.0-py3-none-any.whl", nil)
	rec := httptest.NewRecorder()

	proxy.handlePackageDownload(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleJSONAPI tests JSON API endpoint
func TestHandleJSONAPI(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	// Add test artifact
	artifact := &database.ArtifactMetadata{
		ID:           "requests-json",
		RegistryID:   "pypi",
		ArtifactType: "pypi",
		Namespace:    "",
		ArtifactName: "requests",
		Version:      "2.28.0",
		Size:         1024,
	}
	db.SaveArtifact(artifact)

	// Save metadata to storage
	metadata := []byte(`{"name": "requests", "version": "2.28.0"}`)
	_, err := storage.SaveArtifact("pypi", "", "requests", "2.28.0", metadata)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/pypi/requests/json", nil)
	rec := httptest.NewRecorder()

	proxy.handleJSONAPI(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err = json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "requests", body["name"])
}

// TestHandleProjectList tests project list endpoint
func TestHandleProjectList(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "pkg1", RegistryID: "pypi", ArtifactType: "pypi", Namespace: "", ArtifactName: "django", Version: "4.0.0"},
		{ID: "pkg2", RegistryID: "pypi", ArtifactType: "pypi", Namespace: "", ArtifactName: "celery", Version: "5.0.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/pypi/", nil)
	rec := httptest.NewRecorder()

	proxy.handleProjectList(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	projects := body["projects"].([]interface{})
	assert.GreaterOrEqual(t, len(projects), 2)
}

// TestPyPIProxyIntegration tests the proxy integration
func TestPyPIProxyIntegration(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	// Create proxy
	proxy := NewPyPIProxy(db, storage, cache, rbacMgr, registries)

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
	proxy := &PyPIProxy{cache: cache}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(index int) {
			key := "pypi-concurrent-key"
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
	err := proxy.cacheGet("pypi-concurrent-key", &retrieved)
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
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	// Add artifact with empty namespace
	artifact := &database.ArtifactMetadata{
		ID:           "pypi-ns-test",
		RegistryID:   "pypi",
		ArtifactType: "pypi",
		Namespace:    "",
		ArtifactName: "numpy",
		Version:      "1.23.0",
		Size:         2048,
	}
	db.SaveArtifact(artifact)

	// Search for artifacts
	results, err := db.ListArtifacts("pypi", database.ListOptions{ArtifactType: "pypi"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// TestNamespaceHandling tests namespace handling for PyPI artifacts
func TestNamespaceHandling(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	// Create artifact with namespace
	artifact := &database.ArtifactMetadata{
		ID:           "pypi-ns-test",
		RegistryID:   "pypi",
		ArtifactType: "pypi",
		Namespace:    "google/cloud",
		ArtifactName: "storage",
		Version:      "1.0.0",
		Size:         1024,
	}
	db.SaveArtifact(artifact)

	// Search with namespace
	results, err := db.ListArtifacts("pypi", database.ListOptions{
		Namespace:    "google/cloud",
		ArtifactType: "pypi",
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	assert.Equal(t, "google/cloud", results[0].Namespace)
}

// TestHandleProjectSearch tests project search endpoint
func TestHandleProjectSearch(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "pkg1", RegistryID: "pypi", ArtifactType: "pypi", Namespace: "", ArtifactName: "django-redis", Version: "1.0.0"},
		{ID: "pkg2", RegistryID: "pypi", ArtifactType: "pypi", Namespace: "", ArtifactName: "flask-redis", Version: "1.0.0"},
		{ID: "pkg3", RegistryID: "pypi", ArtifactType: "pypi", Namespace: "", ArtifactName: "celery", Version: "1.0.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/search?q=redis", nil)
	rec := httptest.NewRecorder()

	proxy.handleProjectSearch(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	results := body["results"].([]interface{})
	assert.GreaterOrEqual(t, len(results), 2)
}

// TestPyPIProxyRoutes tests all expected routes are registered
func TestPyPIProxyRoutes(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "pypi", Name: "PyPI Registry", Type: "pypi", Proxy: true, Enabled: true},
	}

	proxy := NewPyPIProxy(db, storage, cache, rbacMgr, registries)

	// Create test server
	server := httptest.NewServer(proxy)
	defer server.Close()

	// Test all expected routes
	routes := []string{
		"/",
		"/simple/",
		"/simple/requests/",
		"/pypi/",
		"/pypi/requests/json",
		"/search",
	}

	for _, route := range routes {
		resp, err := http.Get(server.URL + route)
		require.NoError(t, err)
		// Routes may return different status codes depending on implementation
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode, "Route %s should exist", route)
	}
}
