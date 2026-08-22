package maven

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
