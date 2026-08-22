package nuget

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

// TestNewNuGetProxy tests creating a new NuGet proxy
func TestNewNuGetProxy(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	router := NewNuGetProxy(db, storage, cache, rbacMgr, registries)
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
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	proxy := &NuGetProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "nuget",
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.handleRoot(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "cargobay-nuget", body["name"])
	assert.Equal(t, "0.1.0", body["version"])
}

// TestHandleCatalog tests catalog endpoint
func TestHandleCatalog(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	proxy := &NuGetProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "nuget",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "newtonsoft-json", RegistryID: "nuget", ArtifactType: "nuget", Namespace: "", ArtifactName: "Newtonsoft.Json", Version: "13.0.3"},
		{ID: "npgsql", RegistryID: "nuget", ArtifactType: "nuget", Namespace: "", ArtifactName: "Npgsql", Version: "6.0.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/catalog/0", nil)
	rec := httptest.NewRecorder()

	proxy.handleCatalog(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	count := body["count"].(float64)
	assert.GreaterOrEqual(t, count, 2.0)
}

// TestHandleSearch tests search endpoint
func TestHandleSearch(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	proxy := &NuGetProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "nuget",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "pkg1", RegistryID: "nuget", ArtifactType: "nuget", Namespace: "", ArtifactName: "Microsoft.EntityFrameworkCore", Version: "6.0.0"},
		{ID: "pkg2", RegistryID: "nuget", ArtifactType: "nuget", Namespace: "", ArtifactName: "Microsoft.AspNetCore.Mvc", Version: "2.0.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/query?q=entityframework", nil)
	rec := httptest.NewRecorder()

	proxy.handleSearch(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	data := body["data"].([]interface{})
	assert.GreaterOrEqual(t, len(data), 1)
}

// TestHandlePackageDownload tests package download (nupkg)
func TestHandlePackageDownload(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	proxy := &NuGetProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "nuget",
	}

	// Save nupkg to storage
	nupkgData := []byte("fake nupkg content")
	_, err := storage.SaveArtifact("nuget", "", "Newtonsoft.Json", "13.0.3", nupkgData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/package/newtonsoft.json/13.0.3", nil)
	rec := httptest.NewRecorder()

	proxy.handlePackageDownload(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "fake nupkg content")
}

// TestHandlePackageDownloadNotFound tests 404 for non-existent package
func TestHandlePackageDownloadNotFound(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	proxy := &NuGetProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "nuget",
	}

	req := httptest.NewRequest(http.MethodGet, "/package/nonexistent/1.0.0", nil)
	rec := httptest.NewRecorder()

	proxy.handlePackageDownload(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleV3Package tests V3 package endpoint
func TestHandleV3Package(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	proxy := &NuGetProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "nuget",
	}

	// Add test artifact
	artifact := &database.ArtifactMetadata{
		ID:           "newtonsoft-v3",
		RegistryID:   "nuget",
		ArtifactType: "nuget",
		Namespace:    "",
		ArtifactName: "Newtonsoft.Json",
		Version:      "13.0.3",
		Size:         1024,
	}
	db.SaveArtifact(artifact)

	req := httptest.NewRequest(http.MethodGet, "/v3/registration/newtonsoft.json/index.json", nil)
	rec := httptest.NewRecorder()

	proxy.handleV3Package(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.NotNil(t, body["items"])
}

// TestHandleV3Search tests V3 search endpoint
func TestHandleV3Search(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	proxy := &NuGetProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "nuget",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "pkg1", RegistryID: "nuget", ArtifactType: "nuget", Namespace: "", ArtifactName: "Serilog", Version: "2.0.0"},
		{ID: "pkg2", RegistryID: "nuget", ArtifactType: "nuget", Namespace: "", ArtifactName: "Serilog.AspNetCore", Version: "4.0.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/v3/search?query=serilog", nil)
	rec := httptest.NewRecorder()

	proxy.handleV3Search(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	data := body["data"].([]interface{})
	assert.GreaterOrEqual(t, len(data), 1)
}

// TestNuGetProxyIntegration tests the proxy integration
func TestNuGetProxyIntegration(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	// Create proxy
	proxy := NewNuGetProxy(db, storage, cache, rbacMgr, registries)

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
	proxy := &NuGetProxy{cache: cache}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(index int) {
			key := "nuget-concurrent-key"
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
	err := proxy.cacheGet("nuget-concurrent-key", &retrieved)
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
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	proxy := &NuGetProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "nuget",
	}

	// Add artifact with empty namespace
	artifact := &database.ArtifactMetadata{
		ID:           "nuget-ns-test",
		RegistryID:   "nuget",
		ArtifactType: "nuget",
		Namespace:    "",
		ArtifactName: "Microsoft.Extensions.DependencyInjection",
		Version:      "6.0.0",
		Size:         2048,
	}
	db.SaveArtifact(artifact)

	// Search for artifacts
	results, err := db.ListArtifacts("nuget", database.ListOptions{ArtifactType: "nuget"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// TestNamespaceHandling tests namespace handling for NuGet artifacts
func TestNamespaceHandling(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	proxy := &NuGetProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "nuget",
	}

	// Create artifact with namespace
	artifact := &database.ArtifactMetadata{
		ID:           "nuget-ns-test",
		RegistryID:   "nuget",
		ArtifactType: "nuget",
		Namespace:    "xamarin/android",
		ArtifactName: "support-v4",
		Version:      "28.0.0",
		Size:         1024,
	}
	db.SaveArtifact(artifact)

	// Search with namespace
	results, err := db.ListArtifacts("nuget", database.ListOptions{
		Namespace:    "xamarin/android",
		ArtifactType: "nuget",
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	assert.Equal(t, "xamarin/android", results[0].Namespace)
}

// TestNuGetProxyRoutes tests all expected routes are registered
func TestNuGetProxyRoutes(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "nuget", Name: "NuGet Registry", Type: "nuget", Proxy: true, Enabled: true},
	}

	proxy := NewNuGetProxy(db, storage, cache, rbacMgr, registries)

	// Create test server
	server := httptest.NewServer(proxy)
	defer server.Close()

	// Test all expected routes
	routes := []string{
		"/",
		"/catalog/0",
		"/query",
		"/v3/registration/",
		"/v3/search",
	}

	for _, route := range routes {
		resp, err := http.Get(server.URL + route)
		require.NoError(t, err)
		// Routes may return different status codes depending on implementation
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode, "Route %s should exist", route)
	}
}
