package maven

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
)

// connectTestDB returns a live, connected database.Database, skipping the
// test if Postgres isn't reachable in this environment.
func connectTestDB(t *testing.T) *database.Database {
	t.Helper()
	db := database.New("postgres://cargobay:password@localhost:5432/cargobay")
	if err := db.Connect(); err != nil {
		t.Skipf("skipping: postgres not reachable: %v", err)
	}
	t.Cleanup(func() { db.Disconnect() })
	return db
}

// connectTestCache returns a live, connected cache.Cache, skipping the test
// if Redis isn't reachable in this environment.
func connectTestCache(t *testing.T) *cache.Cache {
	t.Helper()
	c, err := cache.New("redis", "redis://localhost:6379/0")
	if err != nil {
		t.Skipf("skipping: redis not reachable: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// newTestProxy builds a MavenProxy directly against real dependencies, for
// tests that invoke unexported handler methods without going through the
// chi router/middleware.
func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) (*MavenProxy, storage.StorageAdapter) {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &MavenProxy{
		db:         db,
		storage:    adapter,
		cache:      c,
		registries: nil,
		registry:   "maven",
	}, adapter
}

// newTestRouter builds the real chi router (including the RequireReadAccess
// middleware) so handler tests that depend on proxy.TargetFromContext (i.e.
// most Maven handlers, which read the resolved target from the request
// context rather than a parameter) get a correctly populated context.
func newTestRouter(t *testing.T, db *database.Database, c *cache.Cache) (chi.Router, storage.StorageAdapter) {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	router := NewMavenProxy(db, adapter, c, rbac.New(db), nil)
	return router, adapter
}

// seedRegistry saves a Maven-type registry bound to a unique host, so
// RequireReadAccess resolves it deterministically via Host header matching
// instead of falling back to whatever registry other concurrently-run tests
// (sharing this live DB) left as the DB-wide default for artifactType
// "maven".
func seedRegistry(t *testing.T, db *database.Database, id, host string, private, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       id,
		Name:     id,
		URL:      "https://upstream.example.com",
		Type:     "maven",
		Proxy:    proxy,
		Enabled:  true,
		Priority: 10,
		Private:  private,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(id) })
	return reg
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// chiRequest builds a request carrying a chi.RouteContext populated with
// params, for tests that invoke a handler directly (bypassing chi's actual
// route matching) but still need chi.URLParam(r, ...) to resolve inside the
// handler.
func chiRequest(method, path string, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	req := httptest.NewRequest(method, path, nil)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestNewMavenProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, adapter := newTestProxy(t, db, c)

	router := NewMavenProxy(p.db, adapter, p.cache, rbac.New(db), nil)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

func TestHandleRoot(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, _ := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	p.handleRoot(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "Cargobay Maven Proxy")
}

func TestHandleJARFromCache(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, adapter := newTestProxy(t, db, c)

	group := uniqueID("grp")
	artifact := "my-app"
	version := "1.0.0"

	jarData := []byte("fake jar content")
	_, err := adapter.SaveArtifact("maven", group, artifact, version, jarData)
	require.NoError(t, err)

	// handleJAR reads its target via proxy.TargetFromContext, which falls
	// back to Target{Label: "maven", Reg: nil} outside RequireReadAccess
	// middleware — so the download-increment call below targets RegistryID
	// "maven", not a registry we seed ourselves.
	artifactRow := &database.ArtifactMetadata{
		RegistryID:   "maven",
		ArtifactType: "maven",
		Namespace:    group,
		ArtifactName: artifact,
		Version:      version,
		Tags:         []string{version},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifactRow))
	t.Cleanup(func() { db.DeleteArtifact("maven", group, artifact, version) })

	req := chiRequest(http.MethodGet, "/", map[string]string{"group": group, "artifact": artifact, "version": version})
	rec := httptest.NewRecorder()

	p.handleJAR(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/java-archive", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "fake jar content")

	rows, err := db.ListArtifacts("maven", database.ListOptions{ArtifactType: "maven", Namespace: group})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(1), rows[0].Downloads)
}

// TestHandleJARUpstreamUnavailable documents a routing quirk discovered
// while rewriting this test file: the production route pattern
// "/{group}/{artifact}/{version}/{fileName}-{fileVersion}.jar" never
// actually matches a real request through chi (chi does not support two
// dynamic params followed by a static suffix within one path segment — a
// pattern like "{a}-{b}.jar" 404s for any path), so these handlers are
// exercised directly rather than through the router. Called this way, the
// resolved target always has Reg == nil (see TestHandleJARFromCache),
// so an uncached artifact always hits the "no upstream configured" path.
func TestHandleJARUpstreamUnavailable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, _ := newTestProxy(t, db, c)

	req := chiRequest(http.MethodGet, "/", map[string]string{"group": "nonexistent", "artifact": "artifact", "version": "1.0.0"})
	rec := httptest.NewRecorder()

	p.handleJAR(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandlePOMFromCache(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, adapter := newTestProxy(t, db, c)

	group := uniqueID("grp")
	artifact := "my-app"
	version := "1.0.0"

	pomData := []byte(`<?xml version="1.0"?>
<project>
	<groupId>com.example</groupId>
	<artifactId>my-app</artifactId>
	<version>1.0.0</version>
</project>`)
	_, err := adapter.SaveArtifact("maven", group, artifact, version, pomData)
	require.NoError(t, err)

	req := chiRequest(http.MethodGet, "/", map[string]string{"group": group, "artifact": artifact, "version": version})
	rec := httptest.NewRecorder()

	p.handlePOM(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "<project>")
}

func TestHandlePOMUpstreamUnavailable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, _ := newTestProxy(t, db, c)

	req := chiRequest(http.MethodGet, "/", map[string]string{"group": "nonexistent", "artifact": "artifact", "version": "1.0.0"})
	rec := httptest.NewRecorder()

	p.handlePOM(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandleWARFromCache(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, adapter := newTestProxy(t, db, c)

	group := uniqueID("grp")
	artifact := "webapp"
	version := "1.0.0"

	warData := []byte("fake war content")
	_, err := adapter.SaveArtifact("maven", group, artifact, version, warData)
	require.NoError(t, err)

	req := chiRequest(http.MethodGet, "/", map[string]string{"group": group, "artifact": artifact, "version": version})
	rec := httptest.NewRecorder()

	p.handleWAR(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
}

func TestHandleZIPFromCache(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, adapter := newTestProxy(t, db, c)

	group := uniqueID("grp")
	artifact := "archive"
	version := "1.0.0"

	zipData := []byte("fake zip content")
	_, err := adapter.SaveArtifact("maven", group, artifact, version, zipData)
	require.NoError(t, err)

	req := chiRequest(http.MethodGet, "/", map[string]string{"group": group, "artifact": artifact, "version": version})
	rec := httptest.NewRecorder()

	p.handleZIP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
}

func TestHandleTGZFromCache(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, adapter := newTestProxy(t, db, c)

	group := uniqueID("grp")
	artifact := "compressed"
	version := "1.0.0"

	tgzData := []byte("fake tgz content")
	_, err := adapter.SaveArtifact("maven", group, artifact, version, tgzData)
	require.NoError(t, err)

	req := chiRequest(http.MethodGet, "/", map[string]string{"group": group, "artifact": artifact, "version": version})
	rec := httptest.NewRecorder()

	p.handleTGZ(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/gzip", rec.Header().Get("Content-Type"))
}

func TestHandleArtifactFileUpstreamUnavailable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, _ := newTestProxy(t, db, c)

	req := chiRequest(http.MethodGet, "/", map[string]string{"group": "nonexistent", "artifact": "artifact", "version": "1.0.0"})
	rec := httptest.NewRecorder()

	p.handleWAR(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandleVersionDir(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("versiondir-reg")
	host := uniqueID("versiondir-host") + ".test"
	seedRegistry(t, db, regID, host, false, false)

	group := uniqueID("grp")
	artifact := "my-app"

	for _, v := range []string{"1.0.0", "1.0.1"} {
		row := &database.ArtifactMetadata{
			RegistryID:   regID,
			ArtifactType: "maven",
			Namespace:    group,
			ArtifactName: artifact,
			Version:      v,
			Tags:         []string{v},
			Metadata:     map[string]interface{}{},
		}
		require.NoError(t, db.SaveArtifact(row))
		v := v
		t.Cleanup(func() { db.DeleteArtifact(regID, group, artifact, v) })
	}

	path := fmt.Sprintf("/%s/%s/%s/", group, artifact, "1.0.0")
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	files, ok := body["files"].([]interface{})
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(files), 1)
}

func TestHandleArtifactDir(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("artifactdir-reg")
	host := uniqueID("artifactdir-host") + ".test"
	seedRegistry(t, db, regID, host, false, false)

	group := uniqueID("grp")
	rows := []*database.ArtifactMetadata{
		{RegistryID: regID, ArtifactType: "maven", Namespace: group, ArtifactName: "my-app", Version: "1.0.0", Tags: []string{"1.0.0"}, Metadata: map[string]interface{}{}},
		{RegistryID: regID, ArtifactType: "maven", Namespace: group, ArtifactName: "my-app", Version: "1.0.1", Tags: []string{"1.0.1"}, Metadata: map[string]interface{}{}},
		{RegistryID: regID, ArtifactType: "maven", Namespace: group, ArtifactName: "other-app", Version: "1.0.0", Tags: []string{"1.0.0"}, Metadata: map[string]interface{}{}},
	}
	for _, row := range rows {
		require.NoError(t, db.SaveArtifact(row))
		row := row
		t.Cleanup(func() { db.DeleteArtifact(row.RegistryID, row.Namespace, row.ArtifactName, row.Version) })
	}

	path := fmt.Sprintf("/%s/%s/", group, "my-app")
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	versions, ok := body["versions"].([]interface{})
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(versions), 2)
}

func TestHandleGroupDir(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("groupdir-reg")
	host := uniqueID("groupdir-host") + ".test"
	seedRegistry(t, db, regID, host, false, false)

	group := uniqueID("grp")
	rows := []*database.ArtifactMetadata{
		{RegistryID: regID, ArtifactType: "maven", Namespace: group, ArtifactName: "my-app", Version: "1.0.0", Tags: []string{"1.0.0"}, Metadata: map[string]interface{}{}},
		{RegistryID: regID, ArtifactType: "maven", Namespace: group, ArtifactName: "other-app", Version: "1.0.0", Tags: []string{"1.0.0"}, Metadata: map[string]interface{}{}},
	}
	for _, row := range rows {
		require.NoError(t, db.SaveArtifact(row))
		row := row
		t.Cleanup(func() { db.DeleteArtifact(row.RegistryID, row.Namespace, row.ArtifactName, row.Version) })
	}

	path := fmt.Sprintf("/%s/", group)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	names, ok := body["artifacts"].([]interface{})
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(names), 2)
}

// TestHandleMetadata covers maven-metadata.xml generation. handleMetadata
// (unlike the other list handlers) queries the database with the literal
// registry ID "maven" rather than the resolved target's label, so the
// seeded rows below intentionally use RegistryID "maven" to match that
// real behavior — not the registry bound to the request's Host.
func TestHandleMetadata(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	host := uniqueID("metadata-host") + ".test"
	seedRegistry(t, db, uniqueID("metadata-reg"), host, false, false)

	group := uniqueID("grp")
	artifact := "my-app"
	for _, v := range []string{"1.0.0", "1.0.1"} {
		row := &database.ArtifactMetadata{
			RegistryID:   "maven",
			ArtifactType: "maven",
			Namespace:    group,
			ArtifactName: artifact,
			Version:      v,
			Tags:         []string{v},
			Metadata:     map[string]interface{}{},
		}
		require.NoError(t, db.SaveArtifact(row))
		v := v
		t.Cleanup(func() { db.DeleteArtifact("maven", group, artifact, v) })
	}

	path := fmt.Sprintf("/%s/%s/%s/maven-metadata.xml", group, artifact, "1.0.1")
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, "<metadata>")
	assert.Contains(t, body, fmt.Sprintf("<groupId>%s</groupId>", group))
	assert.Contains(t, body, fmt.Sprintf("<artifactId>%s</artifactId>", artifact))
	assert.Contains(t, body, "<version>1.0.0</version>")
	assert.Contains(t, body, "<version>1.0.1</version>")
}

func TestGetUpstreamURL(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p, _ := newTestProxy(t, db, c)

	_, err := p.getUpstreamURL(nil, "com.example", "my-app", "1.0.0", "jar")
	assert.Error(t, err, "nil registry should have no upstream configured")

	_, err = p.getUpstreamURL(&database.RegistryConfig{Proxy: false, URL: "https://repo.example.com"}, "com.example", "my-app", "1.0.0", "jar")
	assert.Error(t, err, "proxy-disabled registry should have no upstream configured")

	url, err := p.getUpstreamURL(&database.RegistryConfig{Proxy: true, URL: "https://repo.example.com"}, "com.example", "my-app", "1.0.0", "jar")
	require.NoError(t, err)
	assert.Equal(t, "https://repo.example.com/com/example/my-app/1.0.0/my-app-1.0.0.jar", url)
}

func TestCacheConcurrency(t *testing.T) {
	c := connectTestCache(t)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			key := fmt.Sprintf("maven-test-concurrency-%d", i)
			err := c.Set(key, []byte("value"))
			assert.NoError(t, err)
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestMavenProxyIntegration(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMavenProxyPrivateRegistryRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("private-reg")
	host := uniqueID("private-host") + ".test"
	seedRegistry(t, db, regID, host, true, false)

	path := "/some/artifact/1.0.0/artifact-1.0.0.jar"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestNamespaceHandling(t *testing.T) {
	db := connectTestDB(t)

	regID := uniqueID("ns-reg")
	group := uniqueID("grp")
	artifact := &database.ArtifactMetadata{
		RegistryID:   regID,
		ArtifactType: "maven",
		Namespace:    group,
		ArtifactName: "my-lib",
		Version:      "1.0.0",
		Size:         2048,
		Tags:         []string{"1.0.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact(regID, group, "my-lib", "1.0.0") })

	results, err := db.ListArtifacts(regID, database.ListOptions{
		Namespace:    group,
		ArtifactType: "maven",
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, group, results[0].Namespace)
}

// authedRequest injects an authenticated user into req's context, mirroring
// what middleware.NewAuthMiddleware would have done in production (that
// middleware is wired only in cmd/server/main.go, outside the router these
// tests exercise directly).
func authedRequest(req *http.Request, userID string) *http.Request {
	user := &middleware.User{UserID: userID, Username: userID}
	ctx := context.WithValue(req.Context(), middleware.AuthUserKey, user)
	return req.WithContext(ctx)
}

func TestHandlePutArtifactRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("put-noauth-reg")
	host := uniqueID("put-noauth-host") + ".test"
	seedRegistry(t, db, regID, host, true, false)

	path := "/com.example/my-app/1.0.0/my-app-1.0.0.jar"
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader("jar bytes"))
	req.Host = host
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandlePutArtifactRequiresPublishGrant(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("put-noperm-reg")
	host := uniqueID("put-noperm-host") + ".test"
	seedRegistry(t, db, regID, host, true, false)

	path := "/com.example/my-app/1.0.0/my-app-1.0.0.jar"
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader("jar bytes"))
	req.Host = host
	req = authedRequest(req, uniqueID("user"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandlePutArtifactSucceedsWithPublishGrant(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("put-ok-reg")
	host := uniqueID("put-ok-host") + ".test"
	seedRegistry(t, db, regID, host, true, false)

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: regID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(regID, userID) })

	group := uniqueID("grp")
	artifact := "my-app"
	version := "1.0.0"
	jarData := []byte("real jar bytes")

	putPath := fmt.Sprintf("/%s/%s/%s/%s-%s.jar", group, artifact, version, artifact, version)
	putReq := httptest.NewRequest(http.MethodPut, putPath, strings.NewReader(string(jarData)))
	putReq.Host = host
	putReq = authedRequest(putReq, userID)
	putRec := httptest.NewRecorder()

	router.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusCreated, putRec.Code)
	t.Cleanup(func() { db.DeleteArtifact(regID, group, artifact, version) })

	getReq := httptest.NewRequest(http.MethodGet, putPath, nil)
	getReq.Host = host
	getRec := httptest.NewRecorder()

	router.ServeHTTP(getRec, getReq)

	assert.Equal(t, http.StatusOK, getRec.Code)
	assert.Equal(t, jarData, getRec.Body.Bytes())
}

// TestHandlePutArtifactStorageKeyIsolation is a regression test for the
// pre-existing bug where handleJAR/handlePOM/handleArtifactFile hardcoded
// the literal registry label "maven" instead of the resolved target's
// label. Without the fix, a JAR pushed to a non-default private registry
// would be saved under its own label but read back from (and visible
// under) the literal "maven" bucket — silently missing on pull via its own
// registry, and visible cross-registry via any other "maven"-labeled
// registry.
func TestHandlePutArtifactStorageKeyIsolation(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	privID := uniqueID("isolation-priv-reg")
	privHost := uniqueID("isolation-priv-host") + ".test"
	seedRegistry(t, db, privID, privHost, true, false)

	defaultID := uniqueID("isolation-default-reg")
	defaultHost := uniqueID("isolation-default-host") + ".test"
	seedRegistry(t, db, defaultID, defaultHost, false, false)

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: privID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(privID, userID) })

	group := uniqueID("grp")
	artifact := "isolated-app"
	version := "1.0.0"
	jarData := []byte("isolated jar bytes")

	putPath := fmt.Sprintf("/%s/%s/%s/%s-%s.jar", group, artifact, version, artifact, version)
	putReq := httptest.NewRequest(http.MethodPut, putPath, strings.NewReader(string(jarData)))
	putReq.Host = privHost
	putReq = authedRequest(putReq, userID)
	putRec := httptest.NewRecorder()

	router.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusCreated, putRec.Code)
	t.Cleanup(func() { db.DeleteArtifact(privID, group, artifact, version) })

	// Retrievable via the registry it was pushed to.
	privGetReq := httptest.NewRequest(http.MethodGet, putPath, nil)
	privGetReq.Host = privHost
	privGetRec := httptest.NewRecorder()
	router.ServeHTTP(privGetRec, privGetReq)
	assert.Equal(t, http.StatusOK, privGetRec.Code)
	assert.Equal(t, jarData, privGetRec.Body.Bytes())

	// Not visible via a different registry that also resolves to the
	// artifactType "maven" default label.
	defaultGetReq := httptest.NewRequest(http.MethodGet, putPath, nil)
	defaultGetReq.Host = defaultHost
	defaultGetRec := httptest.NewRecorder()
	router.ServeHTTP(defaultGetRec, defaultGetReq)
	assert.Equal(t, http.StatusBadGateway, defaultGetRec.Code)
}

// TestHandlePutArtifactPOMVariant proves handlePutArtifact's ext/contentType
// parameterization works for a non-JAR extension, not just the JAR case
// exercised above.
func TestHandlePutArtifactPOMVariant(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("put-pom-reg")
	host := uniqueID("put-pom-host") + ".test"
	seedRegistry(t, db, regID, host, true, false)

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: regID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(regID, userID) })

	group := uniqueID("grp")
	artifact := "my-app"
	version := "1.0.0"
	pomData := []byte(`<?xml version="1.0"?><project></project>`)

	putPath := fmt.Sprintf("/%s/%s/%s/%s-%s.pom", group, artifact, version, artifact, version)
	putReq := httptest.NewRequest(http.MethodPut, putPath, strings.NewReader(string(pomData)))
	putReq.Host = host
	putReq = authedRequest(putReq, userID)
	putRec := httptest.NewRecorder()

	router.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusCreated, putRec.Code)
	t.Cleanup(func() { db.DeleteArtifact(regID, group, artifact, version) })

	getReq := httptest.NewRequest(http.MethodGet, putPath, nil)
	getReq.Host = host
	getRec := httptest.NewRecorder()

	router.ServeHTTP(getRec, getReq)

	assert.Equal(t, http.StatusOK, getRec.Code)
	assert.Equal(t, "application/xml", getRec.Header().Get("Content-Type"))
	assert.Equal(t, pomData, getRec.Body.Bytes())
}

// TestHandlePutMetadataNoop covers the maven-metadata.xml PUT route: mvn
// deploy always uploads this file alongside the artifact, and it must be
// accepted (201) even though nothing is persisted for it, or the deploy
// fails client-side. Auth/publish-grant enforcement mirrors the artifact
// PUT routes since both sit behind the same route-group middleware and the
// same checkPublishAccess call.
func TestHandlePutMetadataNoop(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("put-meta-reg")
	host := uniqueID("put-meta-host") + ".test"
	seedRegistry(t, db, regID, host, true, false)

	group := uniqueID("grp")
	artifact := "my-app"
	version := "1.0.0"
	metaPath := fmt.Sprintf("/%s/%s/%s/maven-metadata.xml", group, artifact, version)

	// No auth -> 401.
	noAuthReq := httptest.NewRequest(http.MethodPut, metaPath, strings.NewReader("<metadata/>"))
	noAuthReq.Host = host
	noAuthRec := httptest.NewRecorder()
	router.ServeHTTP(noAuthRec, noAuthReq)
	assert.Equal(t, http.StatusUnauthorized, noAuthRec.Code)

	// Authed but no grant -> 403.
	userID := uniqueID("user")
	noGrantReq := httptest.NewRequest(http.MethodPut, metaPath, strings.NewReader("<metadata/>"))
	noGrantReq.Host = host
	noGrantReq = authedRequest(noGrantReq, userID)
	noGrantRec := httptest.NewRecorder()
	router.ServeHTTP(noGrantRec, noGrantReq)
	assert.Equal(t, http.StatusForbidden, noGrantRec.Code)

	// Granted -> 201, body discarded without error.
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: regID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(regID, userID) })

	okReq := httptest.NewRequest(http.MethodPut, metaPath, strings.NewReader("<metadata><groupId>g</groupId></metadata>"))
	okReq.Host = host
	okReq = authedRequest(okReq, userID)
	okRec := httptest.NewRecorder()
	router.ServeHTTP(okRec, okReq)
	assert.Equal(t, http.StatusCreated, okRec.Code)
}

// TestHandlePutArtifactGroupGrantSucceeds proves CanPublishRegistry's
// group-based grant path (GroupRegistryAccess.CanPublish) is sufficient to
// publish, without any per-user RegistryAccess row at all.
func TestHandlePutArtifactGroupGrantSucceeds(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("put-group-reg")
	host := uniqueID("put-group-host") + ".test"
	seedRegistry(t, db, regID, host, true, false)

	grp, err := db.CreateGroup(uniqueID("deployers"), "deployers group")
	require.NoError(t, err)
	t.Cleanup(func() { db.DeleteGroup(grp.ID) })

	userID := uniqueID("user")
	require.NoError(t, db.AddGroupMember(grp.ID, userID))
	require.NoError(t, db.GrantGroupRegistryAccess(&database.GroupRegistryAccess{
		RegistryID: regID,
		GroupID:    grp.ID,
		CanRead:    true,
		CanPublish: true,
	}))

	group := uniqueID("grp")
	artifact := "my-app"
	version := "1.0.0"
	jarData := []byte("group-granted jar bytes")

	putPath := fmt.Sprintf("/%s/%s/%s/%s-%s.jar", group, artifact, version, artifact, version)
	putReq := httptest.NewRequest(http.MethodPut, putPath, strings.NewReader(string(jarData)))
	putReq.Host = host
	putReq = authedRequest(putReq, userID)
	putRec := httptest.NewRecorder()

	router.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusCreated, putRec.Code)
	t.Cleanup(func() { db.DeleteArtifact(regID, group, artifact, version) })
}

// TestHandlePutArtifactNonPrivateRegistryDenied proves CanPublishRegistry
// denies publish to a public (non-private) registry even for a user with an
// explicit CanPublish grant recorded against it — mirroring how Docker push
// is denied against non-private registries.
func TestHandlePutArtifactNonPrivateRegistryDenied(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("put-public-reg")
	host := uniqueID("put-public-host") + ".test"
	seedRegistry(t, db, regID, host, false, false) // private=false

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: regID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(regID, userID) })

	group := uniqueID("grp")
	artifact := "my-app"
	version := "1.0.0"

	putPath := fmt.Sprintf("/%s/%s/%s/%s-%s.jar", group, artifact, version, artifact, version)
	putReq := httptest.NewRequest(http.MethodPut, putPath, strings.NewReader("jar bytes"))
	putReq.Host = host
	putReq = authedRequest(putReq, userID)
	putRec := httptest.NewRecorder()

	router.ServeHTTP(putRec, putReq)
	assert.Equal(t, http.StatusForbidden, putRec.Code)
}

// TestHandlePutArtifactReadScopeDenied proves a read-scoped credential
// (e.g. a read-only personal access token) can never publish, even with an
// otherwise-valid CanPublish grant on a private registry.
func TestHandlePutArtifactReadScopeDenied(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	router, _ := newTestRouter(t, db, c)

	regID := uniqueID("put-readscope-reg")
	host := uniqueID("put-readscope-host") + ".test"
	seedRegistry(t, db, regID, host, true, false)

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: regID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(regID, userID) })

	group := uniqueID("grp")
	artifact := "my-app"
	version := "1.0.0"

	putPath := fmt.Sprintf("/%s/%s/%s/%s-%s.jar", group, artifact, version, artifact, version)
	putReq := httptest.NewRequest(http.MethodPut, putPath, strings.NewReader("jar bytes"))
	putReq.Host = host
	ctx := context.WithValue(putReq.Context(), middleware.AuthUserKey, &middleware.User{UserID: userID, Username: userID, Scope: "read"})
	putReq = putReq.WithContext(ctx)
	putRec := httptest.NewRecorder()

	router.ServeHTTP(putRec, putReq)
	assert.Equal(t, http.StatusForbidden, putRec.Code)
}

// TestGradleAliasProxyPublishSucceeds proves NewMavenAliasProxy's write
// routes work end-to-end for a non-"maven" registry type: p.registry
// ("gradle") must thread through ResolveRegistry/RBAC/ArtifactType exactly
// like the base Maven proxy does.
func TestGradleAliasProxyPublishSucceeds(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	router := NewMavenAliasProxy(db, adapter, c, rbac.New(db), nil, "gradle")

	regID := uniqueID("gradle-reg")
	host := uniqueID("gradle-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       regID,
		Name:     regID,
		URL:      "https://upstream.example.com",
		Type:     "gradle",
		Proxy:    false,
		Enabled:  true,
		Priority: 10,
		Private:  true,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(regID) })

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: regID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(regID, userID) })

	group := uniqueID("grp")
	artifact := "gradle-app"
	version := "1.0.0"
	jarData := []byte("gradle jar bytes")

	putPath := fmt.Sprintf("/%s/%s/%s/%s-%s.jar", group, artifact, version, artifact, version)
	putReq := httptest.NewRequest(http.MethodPut, putPath, strings.NewReader(string(jarData)))
	putReq.Host = host
	putReq = authedRequest(putReq, userID)
	putRec := httptest.NewRecorder()

	router.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusCreated, putRec.Code)
	t.Cleanup(func() { db.DeleteArtifact(regID, group, artifact, version) })

	artifacts, err := db.ListArtifacts(regID, database.ListOptions{ArtifactType: "gradle", Namespace: group})
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "gradle", artifacts[0].ArtifactType)

	getReq := httptest.NewRequest(http.MethodGet, putPath, nil)
	getReq.Host = host
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	assert.Equal(t, http.StatusOK, getRec.Code)
	assert.Equal(t, jarData, getRec.Body.Bytes())
}

func TestArtifactSearchByNamespace(t *testing.T) {
	db := connectTestDB(t)

	regID := uniqueID("search-reg")
	group := uniqueID("grp")
	otherGroup := uniqueID("grp-other")

	rows := []*database.ArtifactMetadata{
		{RegistryID: regID, ArtifactType: "maven", Namespace: group, ArtifactName: "app1", Version: "1.0.0", Tags: []string{"1.0.0"}, Metadata: map[string]interface{}{}},
		{RegistryID: regID, ArtifactType: "maven", Namespace: group, ArtifactName: "app2", Version: "2.0.0", Tags: []string{"2.0.0"}, Metadata: map[string]interface{}{}},
		{RegistryID: regID, ArtifactType: "maven", Namespace: otherGroup, ArtifactName: "test-lib", Version: "1.0.0", Tags: []string{"1.0.0"}, Metadata: map[string]interface{}{}},
	}
	for _, row := range rows {
		require.NoError(t, db.SaveArtifact(row))
		row := row
		t.Cleanup(func() { db.DeleteArtifact(row.RegistryID, row.Namespace, row.ArtifactName, row.Version) })
	}

	results, err := db.ListArtifacts(regID, database.ListOptions{
		Namespace:    group,
		ArtifactType: "maven",
	})
	require.NoError(t, err)
	assert.Len(t, results, 2)
}
