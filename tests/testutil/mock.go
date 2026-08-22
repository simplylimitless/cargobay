// Package testutil provides shared test utilities for cargobay
package testutil

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/simplylimitless/cargobay/backend/pkg/vulnerability"
)

// MockDatabase is a mock implementation of the Database interface for testing
type MockDatabase struct {
	Artifacts      map[string]*database.ArtifactMetadata
	Registries     map[string]*database.RegistryConfig
	Users          map[string]*database.UserRepository
	AccessKeys     map[string]*database.AccessKey
	Roles          map[string]*database.Role
	Permissions    map[string]*database.Permission
	RolePerms      map[string][]string
	AuditLogs      []*database.AuditLog
	NextArtifactID int
	NextLogID      int
	mu             sync.RWMutex
}

// NewMockDatabase creates a new MockDatabase instance
func NewMockDatabase() *MockDatabase {
	db := &MockDatabase{
		Artifacts:    make(map[string]*database.ArtifactMetadata),
		Registries:   make(map[string]*database.RegistryConfig),
		Users:        make(map[string]*database.UserRepository),
		AccessKeys:   make(map[string]*database.AccessKey),
		Roles:        make(map[string]*database.Role),
		Permissions:  make(map[string]*database.Permission),
		RolePerms:    make(map[string][]string),
		AuditLogs:    make([]*database.AuditLog, 0),
	}
	db.initDefaults()
	return db
}

func (m *MockDatabase) initDefaults() {
	// Initialize default roles
	roles := []*database.Role{
		{ID: "admin", Name: "Administrator", Description: "Full administrative access", IsSystem: true, CreatedAt: time.Now()},
		{ID: "developer", Name: "Developer", Description: "Standard developer access", IsSystem: true, CreatedAt: time.Now()},
		{ID: "viewer", Name: "Viewer", Description: "Read-only access", IsSystem: true, CreatedAt: time.Now()},
		{ID: "publisher", Name: "Publisher", Description: "Can upload and sign artifacts", IsSystem: true, CreatedAt: time.Now()},
		{ID: "auditor", Name: "Auditor", Description: "Can read artifacts and audit logs", IsSystem: true, CreatedAt: time.Now()},
	}
	for _, role := range roles {
		m.Roles[role.ID] = role
	}

	// Initialize default permissions
	permissions := []*database.Permission{
		{ID: "artifact:read", Name: "Read Artifacts", Description: "Can read artifacts", Resource: "artifact", Action: "read", IsSystem: true},
		{ID: "artifact:write", Name: "Write Artifacts", Description: "Can write artifacts", Resource: "artifact", Action: "write", IsSystem: true},
		{ID: "artifact:delete", Name: "Delete Artifacts", Description: "Can delete artifacts", Resource: "artifact", Action: "delete", IsSystem: true},
		{ID: "artifact:search", Name: "Search Artifacts", Description: "Can search artifacts", Resource: "artifact", Action: "search", IsSystem: true},
		{ID: "artifact:sign", Name: "Sign Artifacts", Description: "Can sign artifacts", Resource: "artifact", Action: "sign", IsSystem: true},
		{ID: "artifact:replicate", Name: "Replicate Artifacts", Description: "Can replicate artifacts", Resource: "artifact", Action: "replicate", IsSystem: true},
		{ID: "registry:read", Name: "Read Registries", Description: "Can read registries", Resource: "registry", Action: "read", IsSystem: true},
		{ID: "registry:write", Name: "Write Registries", Description: "Can write registries", Resource: "registry", Action: "write", IsSystem: true},
		{ID: "registry:delete", Name: "Delete Registries", Description: "Can delete registries", Resource: "registry", Action: "delete", IsSystem: true},
		{ID: "user:read", Name: "Read Users", Description: "Can read users", Resource: "user", Action: "read", IsSystem: true},
		{ID: "user:write", Name: "Write Users", Description: "Can write users", Resource: "user", Action: "write", IsSystem: true},
		{ID: "user:admin", Name: "Admin Users", Description: "Can admin users", Resource: "user", Action: "admin", IsSystem: true},
		{ID: "audit:read", Name: "Read Audit Logs", Description: "Can read audit logs", Resource: "audit", Action: "read", IsSystem: true},
		{ID: "system:read", Name: "Read System", Description: "Can read system config", Resource: "system", Action: "read", IsSystem: true},
		{ID: "system:write", Name: "Write System", Description: "Can write system config", Resource: "system", Action: "write", IsSystem: true},
	}
	for _, perm := range permissions {
		m.Permissions[perm.ID] = perm
	}

	// Link admin to all permissions
	adminPerms := make([]string, len(permissions))
	for i, perm := range permissions {
		adminPerms[i] = perm.ID
	}
	m.RolePerms["admin"] = adminPerms

	// Link developer to subset
	developerPerms := []string{"artifact:read", "artifact:write", "artifact:search"}
	m.RolePerms["developer"] = developerPerms

	// Link viewer to read-only
	viewerPerms := []string{"artifact:read", "artifact:search", "registry:read"}
	m.RolePerms["viewer"] = viewerPerms
}

// GetUserByUsername implements Database interface
func (m *MockDatabase) GetUserByUsername(username string) (*database.UserRepository, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.Users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, nil
}

// GetUser implements Database interface
func (m *MockDatabase) GetUser(userID string) (*database.UserRepository, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.Users[userID]
	if !ok {
		return nil, nil
	}
	return user, nil
}

// GetUserRoles implements Database interface
func (m *MockDatabase) GetUserRoles(userID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.Users[userID]
	if !ok {
		return []string{}, nil
	}
	return user.Roles, nil
}

// GetUserRolePermissions implements Database interface
func (m *MockDatabase) GetUserRolePermissions(userID string) ([]database.RolePermission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, ok := m.Users[userID]
	if !ok {
		return []database.RolePermission{}, nil
	}

	var perms []database.RolePermission
	for _, roleID := range user.Roles {
		for _, permID := range m.RolePerms[roleID] {
			perms = append(perms, database.RolePermission{
				RoleID:       roleID,
				PermissionID: permID,
			})
		}
	}
	return perms, nil
}

// CreateAccessKey implements Database interface
func (m *MockDatabase) CreateAccessKey(userID, name string, permissions []string, expiresAt *time.Time) (*database.AccessKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := &database.AccessKey{
		ID:          "key-" + generateHex(8),
		UserID:      userID,
		Name:        name,
		KeyHash:     generateHex(32),
		Permissions: permissions,
		CreatedAt:   time.Now(),
		ExpiresAt:   expiresAt,
		IsActive:    true,
	}
	m.AccessKeys[key.ID] = key
	return key, nil
}

// InvalidateAccessKey implements Database interface
func (m *MockDatabase) InvalidateAccessKey(keyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key, ok := m.AccessKeys[keyID]; ok {
		key.IsActive = false
	}
	return nil
}

// SaveArtifact implements Database interface
func (m *MockDatabase) SaveArtifact(artifact *database.ArtifactMetadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Artifacts[artifact.ID] = artifact
	return nil
}

// GetArtifact implements Database interface
func (m *MockDatabase) GetArtifact(id string) (*database.ArtifactMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	artifact, ok := m.Artifacts[id]
	if !ok {
		return nil, nil
	}
	return artifact, nil
}

// DeleteArtifact implements Database interface
func (m *MockDatabase) DeleteArtifact(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Artifacts, id)
	return nil
}

// SearchArtifacts implements Database interface
func (m *MockDatabase) SearchArtifacts(query string, opts database.SearchOptions) ([]database.ArtifactMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []database.ArtifactMetadata
	for _, a := range m.Artifacts {
		if opts.ArtifactType != "" && a.ArtifactType != opts.ArtifactType {
			continue
		}
		if opts.RegistryID != "" && a.RegistryID != opts.RegistryID {
			continue
		}
		if query != "" {
			// Simple string matching
			if a.ArtifactName != query && a.Namespace != query {
				continue
			}
		}
		results = append(results, *a)
	}

	// Apply offset and limit
	start := opts.Offset
	if start >= len(results) {
		return []database.ArtifactMetadata{}, nil
	}
	end := start + opts.Limit
	if end > len(results) {
		end = len(results)
	}
	return results[start:end], nil
}

// SearchCount implements Database interface
func (m *MockDatabase) SearchCount(query string, opts database.SearchOptions) (int, error) {
	results, _ := m.SearchArtifacts(query, opts)
	return len(results), nil
}

// CountArtifacts implements Database interface
func (m *MockDatabase) CountArtifacts(registryID string, opts database.ListOptions) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, a := range m.Artifacts {
		if opts.ArtifactType != "" && a.ArtifactType != opts.ArtifactType {
			continue
		}
		count++
	}
	return count, nil
}

// SaveRegistry implements Database interface
func (m *MockDatabase) SaveRegistry(registry *database.RegistryConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Registries[registry.ID] = registry
	return nil
}

// GetRegistry implements Database interface
func (m *MockDatabase) GetRegistry(id string) (*database.RegistryConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	registry, ok := m.Registries[id]
	if !ok {
		return nil, nil
	}
	return registry, nil
}

// ListRegistries implements Database interface
func (m *MockDatabase) ListRegistries() ([]database.RegistryConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]database.RegistryConfig, 0, len(m.Registries))
	for _, r := range m.Registries {
		result = append(result, *r)
	}
	return result, nil
}

// DeleteRegistry implements Database interface
func (m *MockDatabase) DeleteRegistry(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Registries, id)
	return nil
}

// SaveUser implements Database interface
func (m *MockDatabase) SaveUser(user *database.UserRepository) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Users[user.UserID] = user
	return nil
}

// GetUserByAccessKey implements Database interface
func (m *MockDatabase) GetUserByAccessKey(keyHash string) (*database.UserRepository, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, k := range m.AccessKeys {
		if k.KeyHash == keyHash && k.IsActive {
			return m.Users[k.UserID], nil
		}
	}
	return nil, nil
}

// ListUsers implements Database interface
func (m *MockDatabase) ListUsers() ([]database.UserRepository, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]database.UserRepository, 0, len(m.Users))
	for _, u := range m.Users {
		result = append(result, *u)
	}
	return result, nil
}

// DeleteUser implements Database interface
func (m *MockDatabase) DeleteUser(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Users, id)
	return nil
}

// UpdateUserLastLogin implements Database interface
func (m *MockDatabase) UpdateUserLastLogin(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if user, ok := m.Users[userID]; ok {
		now := time.Now()
		user.LastLogin = &now
	}
	return nil
}

// ListRoles implements Database interface
func (m *MockDatabase) ListRoles() ([]database.Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]database.Role, 0, len(m.Roles))
	for _, r := range m.Roles {
		result = append(result, *r)
	}
	return result, nil
}

// GetRole implements Database interface
func (m *MockDatabase) GetRole(id string) (*database.Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	role, ok := m.Roles[id]
	if !ok {
		return nil, nil
	}
	return role, nil
}

// GetPermission implements Database interface
func (m *MockDatabase) GetPermission(id string) (*database.Permission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	perm, ok := m.Permissions[id]
	if !ok {
		return nil, nil
	}
	return perm, nil
}

// ListPermissions implements Database interface
func (m *MockDatabase) ListPermissions() ([]database.Permission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]database.Permission, 0, len(m.Permissions))
	for _, p := range m.Permissions {
		result = append(result, *p)
	}
	return result, nil
}

// CreateAuditLog implements Database interface
func (m *MockDatabase) CreateAuditLog(log *database.AuditLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	log.ID = "log-" + generateHex(8)
	log.CreatedAt = time.Now()
	m.AuditLogs = append(m.AuditLogs, log)
	return nil
}

// ListAuditLogs implements Database interface
func (m *MockDatabase) ListAuditLogs(opts database.ListOptions) ([]database.AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	start := opts.Offset
	if start >= len(m.AuditLogs) {
		return []database.AuditLog{}, nil
	}
	end := start + opts.Limit
	if end > len(m.AuditLogs) {
		end = len(m.AuditLogs)
	}
	return m.AuditLogs[start:end], nil
}

// GetRoleByUserID implements Database interface
func (m *MockDatabase) GetRoleByUserID(userID string) ([]database.Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.Users[userID]
	if !ok {
		return []database.Role{}, nil
	}

	var roles []database.Role
	for _, roleID := range user.Roles {
		if r, ok := m.Roles[roleID]; ok {
			roles = append(roles, *r)
		}
	}
	return roles, nil
}

// GetAccessKeyByID implements Database interface
func (m *MockDatabase) GetAccessKeyByID(keyID string) (*database.AccessKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok := m.AccessKeys[keyID]
	if !ok {
		return nil, nil
	}
	return key, nil
}

// MockStorageAdapter is a mock implementation of StorageAdapter
type MockStorageAdapter struct {
	Artifacts map[string][]byte
	mu        sync.RWMutex
}

// NewMockStorageAdapter creates a new MockStorageAdapter
func NewMockStorageAdapter() *MockStorageAdapter {
	return &MockStorageAdapter{
		Artifacts: make(map[string][]byte),
	}
}

// Connect implements StorageAdapter
func (m *MockStorageAdapter) Connect() error { return nil }

// Disconnect implements StorageAdapter
func (m *MockStorageAdapter) Disconnect() error { return nil }

// SaveArtifact implements StorageAdapter
func (m *MockStorageAdapter) SaveArtifact(registryID, namespace, artifactName, version string, data []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := registryID + "/" + namespace + "/" + artifactName + "/" + version
	m.Artifacts[key] = data
	return key, nil
}

// GetArtifact implements StorageAdapter
func (m *MockStorageAdapter) GetArtifact(registryID, namespace, artifactName, version string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := registryID + "/" + namespace + "/" + artifactName + "/" + version
	data, ok := m.Artifacts[key]
	if !ok {
		return nil, nil
	}
	return data, nil
}

// DeleteArtifact implements StorageAdapter
func (m *MockStorageAdapter) DeleteArtifact(registryID, namespace, artifactName, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := registryID + "/" + namespace + "/" + artifactName + "/" + version
	delete(m.Artifacts, key)
	return nil
}

// ArtifactExists implements StorageAdapter
func (m *MockStorageAdapter) ArtifactExists(registryID, namespace, artifactName, version string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := registryID + "/" + namespace + "/" + artifactName + "/" + version
	_, ok := m.Artifacts[key]
	return ok, nil
}

// MockRBAC is a mock implementation of RBAC manager
type MockRBAC struct {
	Roles     map[string]*rbac.Role
	Perms     map[string]*rbac.Permission
	RolePerms map[string][]string
}

// NewMockRBAC creates a new MockRBAC
func NewMockRBAC() *MockRBAC {
	rbac := &MockRBAC{
		Roles:     make(map[string]*rbac.Role),
		Perms:     make(map[string]*rbac.Permission),
		RolePerms: make(map[string][]string),
	}
	rbac.initDefaults()
	return rbac
}

func (m *MockRBAC) initDefaults() {
	// Default roles
	m.Roles["admin"] = &rbac.Role{ID: "admin", Name: "Administrator", Description: "Full admin access", IsSystem: true}
	m.Roles["developer"] = &rbac.Role{ID: "developer", Name: "Developer", Description: "Standard developer access", IsSystem: true}
	m.Roles["viewer"] = &rbac.Role{ID: "viewer", Name: "Viewer", Description: "Read-only access", IsSystem: true}

	// Default permissions
	m.Perms["artifact:read"] = &rbac.Permission{ID: "artifact:read", Name: "Read Artifacts", Resource: "artifact", Action: "read"}
	m.Perms["artifact:write"] = &rbac.Permission{ID: "artifact:write", Name: "Write Artifacts", Resource: "artifact", Action: "write"}
	m.Perms["artifact:delete"] = &rbac.Permission{ID: "artifact:delete", Name: "Delete Artifacts", Resource: "artifact", Action: "delete"}
	m.Perms["artifact:search"] = &rbac.Permission{ID: "artifact:search", Name: "Search Artifacts", Resource: "artifact", Action: "search"}

	// Role permissions
	m.RolePerms["admin"] = []string{"artifact:read", "artifact:write", "artifact:delete", "artifact:search"}
	m.RolePerms["developer"] = []string{"artifact:read", "artifact:write", "artifact:search"}
	m.RolePerms["viewer"] = []string{"artifact:read", "artifact:search"}
}

// HasPermission implements RBAC
func (m *MockRBAC) HasPermission(userID, permission string) bool {
	return true // Simplified for testing
}

// GetRole implements RBAC
func (m *MockRBAC) GetRole(id string) *rbac.Role {
	return m.Roles[id]
}

// ListRoles implements RBAC
func (m *MockRBAC) ListRoles() []*rbac.Role {
	roles := make([]*rbac.Role, 0, len(m.Roles))
	for _, r := range m.Roles {
		roles = append(roles, r)
	}
	return roles
}

// GetPermission implements RBAC
func (m *MockRBAC) GetPermission(id string) *rbac.Permission {
	return m.Perms[id]
}

// ListPermissions implements RBAC
func (m *MockRBAC) ListPermissions() []*rbac.Permission {
	perms := make([]*rbac.Permission, 0, len(m.Perms))
	for _, p := range m.Perms {
		perms = append(perms, p)
	}
	return perms
}

// MockVulnerabilityScanner is a mock implementation of VulnerabilityScanner
type MockVulnerabilityScanner struct{}

// NewMockVulnerabilityScanner creates a new MockVulnerabilityScanner
func NewMockVulnerabilityScanner() *MockVulnerabilityScanner {
	return &MockVulnerabilityScanner{}
}

// ScanArtifact implements VulnerabilityScanner
func (m *MockVulnerabilityScanner) ScanArtifact(artifact *database.ArtifactMetadata, data []byte) (*vulnerability.ScanResult, error) {
	return &vulnerability.ScanResult{
		ArtifactID:      artifact.ID,
		ArtifactType:    artifact.ArtifactType,
		RegistryID:      artifact.RegistryID,
		Namespace:       artifact.Namespace,
		ArtifactName:    artifact.ArtifactName,
		Version:         artifact.Version,
		ScanTime:        time.Now(),
		Severity:        "none",
		Vulnerabilities: []vulnerability.Vulnerability{},
		ScannedBy:       "mock",
	}, nil
}

// IsScannable implements VulnerabilityScanner
func (m *MockVulnerabilityScanner) IsScannable(artifactType string) bool {
	return artifactType == "docker" || artifactType == "npm" || artifactType == "maven" || artifactType == "pypi"
}

// MockCache is a mock implementation of Cache
type MockCache struct {
	Data  map[string][]byte
	Hits  int
	Misses int
	mu    sync.RWMutex
}

// NewMockCache creates a new MockCache
func NewMockCache() *MockCache {
	return &MockCache{
		Data: make(map[string][]byte),
	}
}

// Get implements Cache
func (m *MockCache) Get(key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if data, ok := m.Data[key]; ok {
		m.Hits++
		if val, ok := value.(*[]byte); ok {
			*val = data
		}
		return nil
	}
	m.Misses++
	return cache.ErrCacheMiss
}

// Set implements Cache
func (m *MockCache) Set(key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if data, ok := value.([]byte); ok {
		m.Data[key] = data
	}
}

// Delete implements Cache
func (m *MockCache) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Data, key)
}

// Close implements Cache
func (m *MockCache) Close() error { return nil }

// Stats returns mock cache stats
func (m *MockCache) Stats() cache.CacheStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cache.CacheStats{
		Hits:      m.Hits,
		Misses:    m.Misses,
		SetCalls:  len(m.Data),
	}
}

// MockHTTPHandler creates a mock HTTP handler for testing
func MockHTTPHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	})
}

// NewTestServer creates a new test server with a mock handler
func NewTestServer(handler http.Handler) *httptest.Server {
	return httptest.NewServer(handler)
}

// MockRequest creates a new HTTP request for testing
func MockRequest(method, url string, body []byte) *http.Request {
	req, _ := http.NewRequest(method, url, nil)
	if body != nil {
		req.Body = http.NoBody
		req.ContentLength = int64(len(body))
	}
	return req
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
