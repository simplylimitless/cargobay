package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/auth"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/simplylimitless/cargobay/backend/pkg/vulnerability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectTestDB connects to the docker-compose Postgres instance used by
// this repo's test suite, skipping the test if it isn't reachable.
func connectTestDB(t *testing.T) *database.Database {
	t.Helper()
	db := database.New("postgres://cargobay:password@localhost:5432/cargobay")
	if err := db.Connect(); err != nil {
		t.Skipf("no postgres available: %v", err)
	}
	t.Cleanup(func() { db.Disconnect() })
	return db
}

// newTestServer builds a real *Server against a live (but otherwise empty)
// database, a real RBAC manager, a real vulnerability scanner, and a real
// local storage adapter rooted at a temp dir.
func newTestServer(t *testing.T, db *database.Database) *Server {
	t.Helper()
	rbacMgr := rbac.New(db)
	scanner := vulnerability.New(db, "", false)
	storageAdapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return NewServer(db, rbacMgr, scanner, storageAdapter, nil, nil, nil, nil, nil)
}

// seedUser creates a real user row (optionally with roles) and schedules
// its cleanup.
func seedUser(t *testing.T, db *database.Database, roles ...string) *database.UserRepository {
	t.Helper()
	hash, err := auth.HashPassword("test-password-123")
	require.NoError(t, err)
	username := fmt.Sprintf("test-user-%d", time.Now().UnixNano())
	user, err := db.CreateUser(username, username+"@example.com", hash, roles)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Exec(context.Background(), "DELETE FROM users WHERE user_id = $1", user.UserID)
	})
	return user
}

// authedRequest returns req with the given user injected into its context,
// mirroring what middleware.NewAuthMiddleware would have done — the auth
// middleware itself is wired only in cmd/server/main.go, outside the
// router this package tests directly.
func authedRequest(req *http.Request, user *database.UserRepository, roles, permissions []string) *http.Request {
	authUser := &middleware.User{
		UserID:      user.UserID,
		Username:    user.Username,
		Email:       user.Email,
		Roles:       roles,
		Permissions: permissions,
	}
	ctx := context.WithValue(req.Context(), middleware.AuthUserKey, authUser)
	return req.WithContext(ctx)
}

func seedRegistry(t *testing.T, db *database.Database, id string, private bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:      id,
		Name:    id,
		URL:     "https://example.com/" + id,
		Type:    "docker",
		Enabled: true,
		Private: private,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() {
		db.Exec(context.Background(), "DELETE FROM registries WHERE id = $1", id)
	})
	return reg
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), v))
}

// --- Public, no-DB endpoints ---

func TestAPIHealthCheck(t *testing.T) {
	s := newTestServer(t, database.New("postgres://localhost:5432/test"))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	decodeJSON(t, w, &body)
	assert.Equal(t, "ok", body["status"])
}

func TestAPIVersion(t *testing.T) {
	s := newTestServer(t, database.New("postgres://localhost:5432/test"))

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	decodeJSON(t, w, &body)
	assert.Equal(t, "1.0.0", body["version"])
}

func TestAPIMetrics(t *testing.T) {
	s := newTestServer(t, database.New("postgres://localhost:5432/test"))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "cargobay_")
}

func TestAPIReplicationStatus(t *testing.T) {
	s := newTestServer(t, database.New("postgres://localhost:5432/test"))

	req := httptest.NewRequest(http.MethodGet, "/replication/status", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]interface{}
	decodeJSON(t, w, &body)
	assert.Equal(t, "running", body["status"])
}

func TestAPIListRegions(t *testing.T) {
	s := newTestServer(t, database.New("postgres://localhost:5432/test"))

	req := httptest.NewRequest(http.MethodGet, "/replication/regions", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string][]string
	decodeJSON(t, w, &body)
	assert.NotEmpty(t, body["regions"])
}

// --- RBAC read endpoints (static, no DB) ---

func TestAPIListRoles(t *testing.T) {
	s := newTestServer(t, database.New("postgres://localhost:5432/test"))

	req := httptest.NewRequest(http.MethodGet, "/roles", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string][]rbac.RoleDefinition
	decodeJSON(t, w, &body)
	assert.GreaterOrEqual(t, len(body["roles"]), 3)
}

func TestAPIGetRole(t *testing.T) {
	s := newTestServer(t, database.New("postgres://localhost:5432/test"))

	req := httptest.NewRequest(http.MethodGet, "/roles/admin", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/roles/does-not-exist", nil)
	w = httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPIListPermissions(t *testing.T) {
	s := newTestServer(t, database.New("postgres://localhost:5432/test"))

	req := httptest.NewRequest(http.MethodGet, "/permissions", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string][]rbac.PermissionDefinition
	decodeJSON(t, w, &body)
	assert.GreaterOrEqual(t, len(body["permissions"]), 15)
}

func TestAPIGetPermission(t *testing.T) {
	s := newTestServer(t, database.New("postgres://localhost:5432/test"))

	req := httptest.NewRequest(http.MethodGet, "/permissions/artifact:read", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/permissions/does-not-exist", nil)
	w = httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- Live-DB endpoints ---

func TestAPISetupStatus(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)

	req := httptest.NewRequest(http.MethodGet, "/setup/status", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]bool
	decodeJSON(t, w, &body)
	// Some other test/seed data may already exist, so we only assert the
	// field is present and well-formed, not its value.
	_, ok := body["needsSetup"]
	assert.True(t, ok)
}

func TestAPIListArtifacts(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)

	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body PaginationResponse
	decodeJSON(t, w, &body)
}

func TestAPISearchArtifacts(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)

	// Missing 'q' is a 400.
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// A real query returns a bare JSON array.
	req = httptest.NewRequest(http.MethodGet, "/search?q=nginx", nil)
	w = httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var results []database.ArtifactMetadata
	decodeJSON(t, w, &results)
}

func TestAPIListRegistries(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)
	seedRegistry(t, db, fmt.Sprintf("test-registry-%d", time.Now().UnixNano()), false)

	req := httptest.NewRequest(http.MethodGet, "/registries", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string][]database.RegistryConfig
	decodeJSON(t, w, &body)
	assert.NotEmpty(t, body["registries"])
}

func TestAPIListUsers(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)
	seedUser(t, db)

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string][]database.UserRepository
	decodeJSON(t, w, &body)
	assert.NotEmpty(t, body["users"])
}

func TestAPIGetUserRoles(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)
	user := seedUser(t, db, "viewer")

	req := httptest.NewRequest(http.MethodGet, "/users/"+user.UserID+"/roles", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string][]string
	decodeJSON(t, w, &body)
	assert.Contains(t, body["roles"], "viewer")
}

func TestAPIGetUserPermissions(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)
	user := seedUser(t, db, "viewer")

	req := httptest.NewRequest(http.MethodGet, "/users/"+user.UserID+"/permissions", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string][]string
	decodeJSON(t, w, &body)
	assert.Contains(t, body["permissions"], "artifact:read")
}

func TestAPIListAuditLogs(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)
	user := seedUser(t, db, "admin")

	req := httptest.NewRequest(http.MethodGet, "/audit/logs", nil)
	req = authedRequest(req, user, []string{"admin"}, []string{"user:admin"})
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]interface{}
	decodeJSON(t, w, &body)
	_, ok := body["logs"]
	assert.True(t, ok)
}

func TestAPIListAuditLogsRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)

	req := httptest.NewRequest(http.MethodGet, "/audit/logs", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- Mutating endpoints: auth/permission gating ---

func TestAPICreateArtifactRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)

	req := httptest.NewRequest(http.MethodPost, "/artifacts", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPICreateArtifactRequiresPermission(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)
	user := seedUser(t, db, "viewer") // no artifact:write

	body := `{"artifactName":"nginx","version":"1.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/artifacts", strings.NewReader(body))
	req = authedRequest(req, user, []string{"viewer"}, []string{"artifact:read"})
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAPICreateAndDeleteArtifact(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)
	registry := seedRegistry(t, db, fmt.Sprintf("test-registry-%d", time.Now().UnixNano()), false)
	user := seedUser(t, db, "publisher") // publisher has artifact:write

	artifactBody := fmt.Sprintf(`{"registryId":%q,"artifactType":"docker","namespace":"library","artifactName":"nginx","version":"1.0.0","tags":[]}`, registry.ID)
	req := httptest.NewRequest(http.MethodPost, "/artifacts", strings.NewReader(artifactBody))
	req = authedRequest(req, user, []string{"publisher"}, []string{"artifact:write", "artifact:read"})
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var created database.ArtifactMetadata
	decodeJSON(t, w, &created)
	require.NotEmpty(t, created.ID)
	t.Cleanup(func() {
		db.Exec(context.Background(), "DELETE FROM artifacts WHERE id = $1", created.ID)
	})

	// Fetching it back should succeed.
	req = httptest.NewRequest(http.MethodGet, "/artifacts/"+created.ID, nil)
	w = httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Deleting without auth is rejected.
	req = httptest.NewRequest(http.MethodDelete, "/artifacts/"+created.ID, nil)
	w = httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// The original uploader may delete it even without artifact:delete.
	req = httptest.NewRequest(http.MethodDelete, "/artifacts/"+created.ID, nil)
	req = authedRequest(req, user, []string{"publisher"}, []string{"artifact:write", "artifact:read"})
	w = httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPICreateRegistryRequiresPermission(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)
	user := seedUser(t, db, "viewer")

	req := httptest.NewRequest(http.MethodPost, "/registries", strings.NewReader(`{"id":"r1","name":"r1"}`))
	req = authedRequest(req, user, []string{"viewer"}, []string{"artifact:read"})
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAPICreateRegistry(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)
	user := seedUser(t, db, "admin")
	id := fmt.Sprintf("test-registry-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		db.Exec(context.Background(), "DELETE FROM registries WHERE id = $1", id)
	})

	body := fmt.Sprintf(`{"id":%q,"name":"test","url":"https://example.com","type":"docker","enabled":true}`, id)
	req := httptest.NewRequest(http.MethodPost, "/registries", strings.NewReader(body))
	req = authedRequest(req, user, []string{"admin"}, []string{"registry:write"})
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestAPIReplicationSyncAlwaysForbidden(t *testing.T) {
	// "replication:sync" is not among the static RBAC permissions, so no
	// real role — including admin — ever grants it; this endpoint is
	// effectively unreachable via the current RBAC configuration.
	db := connectTestDB(t)
	s := newTestServer(t, db)
	user := seedUser(t, db, "admin")

	req := httptest.NewRequest(http.MethodPost, "/replication/sync", nil)
	req = authedRequest(req, user, []string{"admin"}, rbac.New(db).GetRolePermissions("admin"))
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAPIScanArtifactRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	s := newTestServer(t, db)

	req := httptest.NewRequest(http.MethodPost, "/artifacts/some-id/scan", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
