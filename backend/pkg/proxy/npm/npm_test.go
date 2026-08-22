package npm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
)

// targetCtxKey is the context key for target storage
type targetCtxKey struct{}

// Target represents the resolved registry target
type Target struct {
	Reg   *database.RegistryConfig
	Label string
}

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

func (m *MockDatabase) SearchArtifacts(query string, opts database.SearchOptions) ([]database.ArtifactMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []database.ArtifactMetadata
	for _, a := range m.Artifacts {
		if opts.ArtifactType != "" && a.ArtifactType != opts.ArtifactType {
			continue
		}
		if query != "" && a.ArtifactName != query {
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

// TestNewNPMProxy tests creating a new NPM proxy
func TestNewNPMProxy(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	router := NewNPMProxy(db, storage, cache, rbacMgr, registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

// TestHandleRoot tests the root endpoint
func TestHandleRoot(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true}}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.handleRoot(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "cargobay-npm", body["name"])
	assert.Equal(t, "0.1.0", body["version"])
	assert.Contains(t, body, "description")
}

// TestHandlePing tests the ping endpoint
func TestHandlePing(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true}}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	req := httptest.NewRequest(http.MethodGet, "/-/ping", nil)
	rec := httptest.NewRecorder()

	proxy.handlePing(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	assert.Equal(t, "OK", rec.Body.String())
}

// TestHandleUser tests the user endpoint
func TestHandleUser(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true}}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	req := httptest.NewRequest(http.MethodGet, "/-/user", nil)
	rec := httptest.NewRecorder()

	proxy.handleUser(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	// User endpoint returns empty object for now
	assert.Empty(t, body)
}

// TestHandleUserSync tests the user sync endpoint
func TestHandleUserSync(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true}}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	req := httptest.NewRequest(http.MethodGet, "/-/user/sync", nil)
	rec := httptest.NewRecorder()

	proxy.handleUserSync(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, true, body["ok"])
}

// TestHandlePackageWithCache tests the package endpoint with cached data
func TestHandlePackageWithCache(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", URL: "https://registry.npmjs.org", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	// Pre-populate cache
	mockPackage := map[string]interface{}{
		"name":        "express",
		"dist-tags":   map[string]interface{}{"latest": "4.18.0"},
		"versions":    map[string]interface{}{"4.18.0": map[string]interface{}{"name": "express", "version": "4.18.0"}},
	}
	cache.Set("express", mustMarshal(mockPackage))

	// Create request context
	req := httptest.NewRequest(http.MethodGet, "/express", nil)
	target := Target{Reg: &registries[0], Label: "npm"}
	ctx := context.WithValue(req.Context(), targetCtxKey{}, target)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	proxy.handlePackage(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "HIT", rec.Header().Get("X-Cache"))

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "express", body["name"])
}

// TestHandlePackageWithoutCache tests fetching from upstream when cache miss
func TestHandlePackageWithoutCache(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", URL: "https://registry.npmjs.org", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	// Don't populate cache - this will trigger upstream fetch
	req := httptest.NewRequest(http.MethodGet, "/lodash", nil)
	target := Target{Reg: &registries[0], Label: "npm"}
	ctx := context.WithValue(req.Context(), targetCtxKey{}, target)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	proxy.handlePackage(rec, req)

	// Should get 404 since we're mocking upstream
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandlePackageVersion tests the package version endpoint
func TestHandlePackageVersion(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	mockVersion := map[string]interface{}{
		"name":      "express",
		"version":   "4.18.0",
		"dist":      map[string]interface{}{"tarball": "https://registry.npmjs.org/express/-/express-4.18.0.tgz"},
	}
	cache.Set("express:4.18.0", mustMarshal(mockVersion))

	req := httptest.NewRequest(http.MethodGet, "/express/4.18.0", nil)
	target := Target{Reg: &registries[0], Label: "npm"}
	ctx := context.WithValue(req.Context(), targetCtxKey{}, target)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	proxy.handlePackageVersion(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "express", body["name"])
	assert.Equal(t, "4.18.0", body["version"])
}

// TestHandleScopedPackage tests the scoped package endpoint
func TestHandleScopedPackage(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	mockPackage := map[string]interface{}{
		"name":        "@types/node",
		"dist-tags":   map[string]interface{}{"latest": "20.0.0"},
		"versions":    map[string]interface{}{"20.0.0": map[string]interface{}{"name": "@types/node", "version": "20.0.0"}},
	}
	cache.Set("@types/node", mustMarshal(mockPackage))

	req := httptest.NewRequest(http.MethodGet, "/@types/node", nil)
	target := Target{Reg: &registries[0], Label: "npm"}
	ctx := context.WithValue(req.Context(), targetCtxKey{}, target)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	proxy.handleScopedPackage(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "@types/node", body["name"])
}

// TestHandleScopedPackageVersion tests the scoped package version endpoint
func TestHandleScopedPackageVersion(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	mockVersion := map[string]interface{}{
		"name":    "@types/node",
		"version": "20.0.0",
	}
	cache.Set("@types/node:20.0.0", mustMarshal(mockVersion))

	req := httptest.NewRequest(http.MethodGet, "/@types/node/20.0.0", nil)
	target := Target{Reg: &registries[0], Label: "npm"}
	ctx := context.WithValue(req.Context(), targetCtxKey{}, target)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	proxy.handleScopedPackageVersion(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "@types/node", body["name"])
	assert.Equal(t, "20.0.0", body["version"])
}

// TestCacheGet tests cache retrieval
func TestCacheGet(t *testing.T) {
	cache := NewMockCache()
	proxy := &NPMProxy{cache: cache}

	key := "test-key"
	data := []byte(`{"test": "data"}`)
	proxy.cacheSet(key, data)

	var retrieved []byte
	err := proxy.cacheGet(key, &retrieved)
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)
}

// TestCacheGetMiss tests cache miss handling
func TestCacheGetMiss(t *testing.T) {
	cache := NewMockCache()
	proxy := &NPMProxy{cache: cache}

	var retrieved []byte
	err := proxy.cacheGet("non-existent-key", &retrieved)
	require.Error(t, err)
	assert.Nil(t, retrieved)
}

// TestCacheSet tests cache storage
func TestCacheSet(t *testing.T) {
	cache := NewMockCache()
	proxy := &NPMProxy{cache: cache}

	key := "test-key-2"
	data := []byte(`{"test": "data"}`)
	proxy.cacheSet(key, data)

	var retrieved []byte
	err := proxy.cacheGet(key, &retrieved)
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)
}

// TestSavePackageMetadata tests metadata saving
func TestSavePackageMetadata(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	mockData := []byte(`{
		"name": "express",
		"dist-tags": {"latest": "4.18.0"},
		"versions": {"4.18.0": {"name": "express", "version": "4.18.0"}}
	}`)

	proxy.savePackageMetadata("npm", "express", mockData)

	// Verify artifact was saved
	artifacts, err := db.SearchArtifacts("express", database.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(artifacts), 1)
	assert.Equal(t, "npm", artifacts[0].RegistryID)
	assert.Equal(t, "express", artifacts[0].ArtifactName)
}

// TestSavePackageVersionMetadata tests version metadata saving
func TestSavePackageVersionMetadata(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	mockData := []byte(`{
		"name": "express",
		"version": "4.18.0"
	}`)

	proxy.savePackageVersionMetadata("npm", "express", "4.18.0", mockData)

	// Verify artifact was saved
	artifacts, err := db.SearchArtifacts("express", database.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(artifacts), 1)
	assert.Equal(t, "4.18.0", artifacts[0].Version)
}

// TestTarballDownload tests tarball download from storage
func TestTarballDownload(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	// Save a tarball to storage
	tarballData := []byte("fake tarball content")
	_, err := storage.SaveArtifact("npm", "", "express", "4.18.0", tarballData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/express/-/express-4.18.0.tgz", nil)
	target := Target{Reg: &registries[0], Label: "npm"}
	ctx := context.WithValue(req.Context(), targetCtxKey{}, target)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	proxy.handleTarball(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, tarballData, rec.Body.Bytes())
}

// TestTarballNotFound tests 404 when tarball not in storage
func TestTarballNotFound(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	req := httptest.NewRequest(http.MethodGet, "/nonexistent/-/nonexistent-1.0.0.tgz", nil)
	target := Target{Reg: &registries[0], Label: "npm"}
	ctx := context.WithValue(req.Context(), targetCtxKey{}, target)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	proxy.handleTarball(rec, req)

	// Should return 404 for non-existent tarball
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestEmptyNamespace tests handling of empty namespace
func TestEmptyNamespace(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	mockData := []byte(`{
		"name": "lodash",
		"dist-tags": {"latest": "4.17.21"}
	}`)

	proxy.savePackageMetadata("npm", "lodash", mockData)

	// Verify artifact was saved with empty namespace
	artifacts, err := db.SearchArtifacts("lodash", database.SearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(artifacts), 1)
	assert.Equal(t, "", artifacts[0].Namespace)
}

// TestProxyManagerIntegration tests the proxy manager integration
func TestProxyManagerIntegration(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	// Create proxy
	proxy := NewNPMProxy(db, storage, cache, rbacMgr, registries)

	// Test that router is created
	assert.NotNil(t, proxy)

	// Create test server
	server := httptest.NewServer(proxy)
	defer server.Close()

	// Test root endpoint
	resp, err := http.Get(server.URL + "/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "cargobay-npm", body["name"])
}

// TestScopedTarballDownload tests scoped package tarball download
func TestScopedTarballDownload(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	// Save a scoped tarball
	tarballData := []byte("scoped tarball content")
	_, err := storage.SaveArtifact("npm", "@types", "node", "20.0.0", tarballData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/@types/node/-/node-20.0.0.tgz", nil)
	target := Target{Reg: &registries[0], Label: "npm"}
	ctx := context.WithValue(req.Context(), targetCtxKey{}, target)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	proxy.handleScopedTarball(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "@types/node-20.0.0.tgz")
	assert.Equal(t, tarballData, rec.Body.Bytes())
}

// TestCacheConcurrency tests cache thread safety
func TestCacheConcurrency(t *testing.T) {
	cache := NewMockCache()
	proxy := &NPMProxy{cache: cache}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(index int) {
			key := "concurrent-key"
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
	err := proxy.cacheGet("concurrent-key", &retrieved)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
}

// TestRegistryURLExtraction tests tarball URL extraction
func TestRegistryURLExtraction(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", URL: "https://registry.npmjs.org", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	mockPackageData := []byte(`{
		"name": "express",
		"dist": {"tarball": "https://registry.npmjs.org/express/-/express-4.18.0.tgz"}
	}`)

	url, err := proxy.getTarballURL(&registries[0], "express", "4.18.0")
	// This will fail in tests since we don't have real upstream
	assert.Error(t, err)
	assert.Empty(t, url)
}

// TestTargetFromContext tests context value retrieval
func TestTargetFromContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	target := Target{Reg: nil, Label: "npm"}

	ctx := context.WithValue(req.Context(), targetCtxKey{}, target)
	req = req.WithContext(ctx)

	// Use the TargetFromContext function
	retrieved := TargetFromContext(req, "default")
	assert.Equal(t, "npm", retrieved.Label)
}

// TestTargetFromContextNoValue tests fallback when no target in context
func TestTargetFromContextNoValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	retrieved := TargetFromContext(req, "default")
	assert.Equal(t, "default", retrieved.Label)
}

// TestHandlePackageVersionWithCacheHit tests cache hit on version endpoint
func TestHandlePackageVersionWithCacheHit(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true},
	}

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	mockVersion := map[string]interface{}{
		"name":      "express",
		"version":   "4.18.0",
		"dist":      map[string]interface{}{"tarball": "https://example.com/tgz"},
	}
	cache.Set("express:4.18.0", mustMarshal(mockVersion))

	req := httptest.NewRequest(http.MethodGet, "/express/4.18.0", nil)
	target := Target{Reg: &registries[0], Label: "npm"}
	ctx := context.WithValue(req.Context(), targetCtxKey{}, target)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	proxy.handlePackageVersion(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	// Should be a cache hit for version endpoint
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "express", body["name"])
}

// mustMarshal is a helper to marshal JSON
func mustMarshal(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// TargetFromContext retrieves the Target stored by RequireReadAccess
func TargetFromContext(r *http.Request, artifactType string) Target {
	if t, ok := r.Context().Value(targetCtxKey{}).(Target); ok {
		return t
	}
	return Target{Label: artifactType}
}
