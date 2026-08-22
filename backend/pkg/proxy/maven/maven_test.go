package maven

import (
	"encoding/json"
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

// TestNewMavenProxy tests creating a new Maven proxy
func TestNewMavenProxy(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	router := NewMavenProxy(db, storage, cache, rbacMgr, registries)
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
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	proxy.handleRoot(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "Cargobay Maven Proxy")
}

// TestHandleJAR tests JAR file download from storage
func TestHandleJAR(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	// Save JAR to storage
	jarData := []byte("fake jar content")
	_, err := storage.SaveArtifact("maven", "com/example", "my-app", "1.0.0", jarData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/com/example/my-app/1.0.0/my-app-1.0.0.jar", nil)
	rec := httptest.NewRecorder()

	proxy.handleJAR(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/java-archive", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "fake jar content")
}

// TestHandleJARNotFound tests 404 for non-existent JAR
func TestHandleJARNotFound(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	req := httptest.NewRequest(http.MethodGet, "/nonexistent/group/artifact/1.0.0/artifact-1.0.0.jar", nil)
	rec := httptest.NewRecorder()

	proxy.handleJAR(rec, req)

	// Should return 404 for non-existent JAR
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandlePOM tests POM file download from storage
func TestHandlePOM(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	pomData := []byte(`<?xml version="1.0"?>
<project>
	<groupId>com.example</groupId>
	<artifactId>my-app</artifactId>
	<version>1.0.0</version>
</project>`)

	_, err := storage.SaveArtifact("maven", "com/example", "my-app", "1.0.0", pomData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/com/example/my-app/1.0.0/my-app-1.0.0.pom", nil)
	rec := httptest.NewRecorder()

	proxy.handlePOM(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "<project>")
}

// TestHandleWAR tests WAR file download
func TestHandleWAR(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	warData := []byte("fake war content")
	_, err := storage.SaveArtifact("maven", "com/example", "webapp", "1.0.0", warData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/com/example/webapp/1.0.0/webapp-1.0.0.war", nil)
	rec := httptest.NewRecorder()

	proxy.handleWAR(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHandleZIP tests ZIP file download
func TestHandleZIP(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	zipData := []byte("fake zip content")
	_, err := storage.SaveArtifact("maven", "com/example", "archive", "1.0.0", zipData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/com/example/archive/1.0.0/archive-1.0.0.zip", nil)
	rec := httptest.NewRecorder()

	proxy.handleZIP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHandleTGZ tests TGZ file download
func TestHandleTGZ(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	tgzData := []byte("fake tgz content")
	_, err := storage.SaveArtifact("maven", "com/example", "compressed", "1.0.0", tgzData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/com/example/compressed/1.0.0/compressed-1.0.0.tgz", nil)
	rec := httptest.NewRecorder()

	proxy.handleTGZ(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHandleVersionDir tests version directory listing
func TestHandleVersionDir(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "test-1", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "my-app", Version: "1.0.0"},
		{ID: "test-2", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "my-app", Version: "1.0.1"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/com/example/my-app/1.0.0/", nil)
	rec := httptest.NewRecorder()

	proxy.handleVersionDir(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	files := body["files"].([]interface{})
	assert.GreaterOrEqual(t, len(files), 1)
}

// TestHandleArtifactDir tests artifact directory listing
func TestHandleArtifactDir(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "test-1", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "my-app", Version: "1.0.0"},
		{ID: "test-2", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "my-app", Version: "1.0.1"},
		{ID: "test-3", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "other-app", Version: "1.0.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/com/example/my-app/", nil)
	rec := httptest.NewRecorder()

	proxy.handleArtifactDir(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	versions := body["versions"].([]interface{})
	assert.GreaterOrEqual(t, len(versions), 2)
}

// TestHandleGroupDir tests group directory listing
func TestHandleGroupDir(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	// Add test artifacts
	artifacts := []*database.ArtifactMetadata{
		{ID: "test-1", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "my-app", Version: "1.0.0"},
		{ID: "test-2", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "other-app", Version: "1.0.0"},
		{ID: "test-3", RegistryID: "maven", ArtifactType: "maven", Namespace: "org/test", ArtifactName: "test-lib", Version: "1.0.0"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/com/example/", nil)
	rec := httptest.NewRecorder()

	proxy.handleGroupDir(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	artifactsList := body["artifacts"].([]interface{})
	assert.GreaterOrEqual(t, len(artifactsList), 2)
}

// TestHandleMetadata tests maven-metadata.xml generation
func TestHandleMetadata(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	// Add test artifacts with versions
	artifacts := []*database.ArtifactMetadata{
		{ID: "test-1", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "my-app", Version: "1.0.0"},
		{ID: "test-2", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "my-app", Version: "1.0.1"},
	}
	for _, a := range artifacts {
		db.SaveArtifact(a)
	}

	req := httptest.NewRequest(http.MethodGet, "/com/example/my-app/1.0.1/maven-metadata.xml", nil)
	rec := httptest.NewRecorder()

	proxy.handleMetadata(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<metadata>")
	assert.Contains(t, rec.Body.String(), "<groupId>com.example</groupId>")
	assert.Contains(t, rec.Body.String(), "<artifactId>my-app</artifactId>")
}

// TestCacheConcurrency tests cache thread safety
func TestCacheConcurrency(t *testing.T) {
	cache := NewMockCache()
	proxy := &MavenProxy{cache: cache}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(index int) {
			key := "maven-concurrent-key"
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
	err := proxy.cacheGet("maven-concurrent-key", &retrieved)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
}

// TestMavenProxyIntegration tests the proxy integration
func TestMavenProxyIntegration(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	// Create proxy
	proxy := NewMavenProxy(db, storage, cache, rbacMgr, registries)

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

// TestEmptyNamespace tests handling of empty namespace
func TestEmptyNamespace(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	// Create artifact with empty namespace
	artifact := &database.ArtifactMetadata{
		ID:           "maven-ns-test",
		RegistryID:   "maven",
		ArtifactType: "maven",
		Namespace:    "",
		ArtifactName: "standalone",
		Version:      "1.0.0",
		Size:         1024,
	}
	err := db.SaveArtifact(artifact)
	require.NoError(t, err)

	// Search for artifact
	results, err := db.ListArtifacts("maven", database.ListOptions{ArtifactType: "maven"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// TestNamespaceHandling tests namespace handling for Maven artifacts
func TestNamespaceHandling(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	// Create artifact with namespace
	artifact := &database.ArtifactMetadata{
		ID:           "maven-ns-test",
		RegistryID:   "maven",
		ArtifactType: "maven",
		Namespace:    "com/example",
		ArtifactName: "my-lib",
		Version:      "1.0.0",
		Size:         2048,
	}
	err := db.SaveArtifact(artifact)
	require.NoError(t, err)

	// Search with namespace
	results, err := db.ListArtifacts("maven", database.ListOptions{
		Namespace:    "com/example",
		ArtifactType: "maven",
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	assert.Equal(t, "com/example", results[0].Namespace)
}

// TestArtifactSearch tests searching artifacts
func TestArtifactSearch(t *testing.T) {
	db := NewMockDatabase()
	storage := NewMockStorage()
	cache := NewMockCache()
	rbacMgr := rbac.New(database.New("postgres://localhost:5432/test"))
	registries := []database.RegistryConfig{
		{ID: "maven", Name: "Maven Registry", Type: "maven", Proxy: true, Enabled: true},
	}

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "maven",
	}

	// Create test artifacts
	testArtifacts := []*database.ArtifactMetadata{
		{ID: "app1", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "app1", Version: "1.0.0"},
		{ID: "app2", RegistryID: "maven", ArtifactType: "maven", Namespace: "com/example", ArtifactName: "app2", Version: "2.0.0"},
		{ID: "lib1", RegistryID: "maven", ArtifactType: "maven", Namespace: "org/test", ArtifactName: "test-lib", Version: "1.0.0"},
	}
	for _, a := range testArtifacts {
		db.SaveArtifact(a)
	}

	// Search for artifacts with namespace
	results, err := db.ListArtifacts("maven", database.ListOptions{
		Namespace:    "com/example",
		ArtifactType: "maven",
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 2)
}
