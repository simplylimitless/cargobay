package swift

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *SwiftProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &SwiftProxy{db: db, storage: adapter, cache: c}
}

func seedRegistry(t *testing.T, db *database.Database, id string, private, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       id,
		Name:     id,
		URL:      "https://upstream.example.com",
		Type:     "swift",
		Proxy:    proxy,
		Enabled:  true,
		Priority: 10,
		Private:  private,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(id) })
	return reg
}

// seedRegistryWithHost saves a registry bound to a virtual host, so
// resolveTarget/ResolveRegistry picks it deterministically instead of
// falling back to whatever registry (if any) other concurrently-run test
// suites left as the DB-wide default for artifactType "swift".
func seedRegistryWithHost(t *testing.T, db *database.Database, host, upstreamURL string, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("swift-host-reg"),
		Name:     "swift-host-reg",
		URL:      upstreamURL,
		Type:     "swift",
		Proxy:    proxy,
		Enabled:  true,
		Priority: 100,
		Private:  false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })
	return reg
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// withChiRouteContext attaches rctx to r's context the way chi's router
// does, so handlers reading chi.URLParam(r, ...) work when called directly
// (bypassing the router/mux).
func withChiRouteContext(r *http.Request, rctx *chi.Context) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestNewSwiftProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	registries := []database.RegistryConfig{{ID: "swift", Name: "Swift Registry", Type: "swift", Proxy: true, Enabled: true}}

	router := NewSwiftProxy(db, nil, c, rbac.New(db), registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

// TestHandleListReleasesFromDB covers handleListReleases with a release
// recorded in the database. Called directly (no context target set),
// TargetFromContext falls back to Target{Label: "swift"}, so the artifact
// must be seeded under registry ID "swift".
func TestHandleListReleasesFromDB(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	scope := uniqueID("vapor")
	name := "vapor"
	artifact := &database.ArtifactMetadata{
		RegistryID:   "swift",
		ArtifactType: "swift",
		Namespace:    scope,
		ArtifactName: name,
		Version:      "4.0.0",
		Tags:         []string{"4.0.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("swift", scope, name, "4.0.0") })

	req := httptest.NewRequest(http.MethodGet, "/"+scope+"/"+name, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("scope", scope)
	rctx.URLParams.Add("name", name)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleListReleases(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, registryContentType, rec.Header().Get("Content-Type"))
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	releases, ok := body["releases"].(map[string]interface{})
	require.True(t, ok)
	require.Contains(t, releases, "4.0.0")
}

func TestHandleListReleasesEmpty(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	scope := uniqueID("nobody")
	name := uniqueID("nothing")
	req := httptest.NewRequest(http.MethodGet, "/"+scope+"/"+name, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("scope", scope)
	rctx.URLParams.Add("name", name)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleListReleases(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	releases, ok := body["releases"].(map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, releases)
}

func TestHandleReleaseMetadata(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	scope := "apple"
	name := "swift-nio"
	version := "2.0.0"
	req := httptest.NewRequest(http.MethodGet, "/"+scope+"/"+name+"/"+version, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("scope", scope)
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", version)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleReleaseMetadata(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, registryContentType, rec.Header().Get("Content-Type"))
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, fmt.Sprintf("%s.%s", scope, name), body["id"])
	assert.Equal(t, version, body["version"])
	resources, ok := body["resources"].([]interface{})
	require.True(t, ok)
	require.Len(t, resources, 1)
	res := resources[0].(map[string]interface{})
	assert.Equal(t, "source-archive", res["name"])
	assert.Contains(t, res["url"], version+".zip")
}

// TestHandleManifestFromStorage covers handleManifest's cache-hit path: the
// manifest (unlike the source archive) is stored/read via the buffered
// storage.SaveArtifact/GetArtifact calls, not the streaming variants.
func TestHandleManifestFromStorage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	scope := uniqueID("apple")
	name := "swift-nio"
	version := "2.0.0"
	manifestData := []byte("// swift-tools-version:5.5\nlet package = Package(name: \"swift-nio\")")
	_, err := p.storage.SaveArtifact("swift", scope, name, version, manifestData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/"+scope+"/"+name+"/"+version+"/Package.swift", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("scope", scope)
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", version)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleManifest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/x-swift", rec.Header().Get("Content-Type"))
	assert.Equal(t, manifestData, rec.Body.Bytes())
}

func TestHandleManifestNoUpstreamConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	scope := uniqueID("missing")
	name := uniqueID("pkg")
	req := httptest.NewRequest(http.MethodGet, "/"+scope+"/"+name+"/1.0.0/Package.swift", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("scope", scope)
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", "1.0.0")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleManifest(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleManifestCacheMissFetchesUpstream drives the router end to end
// so a Host-bound registry resolves, fetches the manifest text from a fake
// upstream on a genuine cache miss, and verifies a second request is served
// from the now-cached copy even after the fake upstream is shut down.
func TestHandleManifestCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	scope := uniqueID("apple")
	name := "swift-nio"
	version := "2.0.0"
	manifestData := []byte("// swift-tools-version:5.5")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, fmt.Sprintf("/%s/%s/%s/Package.swift", scope, name, version), r.URL.Path)
		w.Write(manifestData)
	}))
	defer upstream.Close()

	host := uniqueID("swift-manifest-host") + ".test"
	seedRegistryWithHost(t, db, host, upstream.URL, true)

	p := newTestProxy(t, db, c)
	router := NewSwiftProxy(p.db, p.storage, p.cache, rbac.New(db), nil)
	server := httptest.NewServer(router)
	defer server.Close()

	get := func() *http.Response {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/"+scope+"/"+name+"/"+version+"/Package.swift", nil)
		require.NoError(t, err)
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		return resp
	}

	resp := get()
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, manifestData, body)

	upstream.Close()
	resp2 := get()
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	assert.Equal(t, manifestData, body2)
}

// TestHandleSourceArchiveFromOldBufferedStorage proves a source archive
// already saved to disk via the old buffered storage.SaveArtifact call (as
// any archive cached before this streaming refactor would have been) is
// still served correctly now that handleSourceArchive reads it back via
// GetArtifactStream.
func TestHandleSourceArchiveFromOldBufferedStorage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	scope := uniqueID("vapor")
	name := "vapor"
	version := "4.0.0"
	archiveData := []byte("fake swift source archive content")
	_, err := p.storage.SaveArtifact("swift", scope, name, version, archiveData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/"+scope+"/"+name+"/"+version+".zip", nil)
	rec := httptest.NewRecorder()

	p.handleSourceArchive(rec, req, scope, name, version)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Equal(t, archiveData, rec.Body.Bytes())
}

func TestHandleSourceArchiveNoUpstreamConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	scope := uniqueID("missing")
	name := uniqueID("pkg")
	req := httptest.NewRequest(http.MethodGet, "/"+scope+"/"+name+"/1.0.0.zip", nil)
	rec := httptest.NewRecorder()

	p.handleSourceArchive(rec, req, scope, name, "1.0.0")

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleSourceArchiveCacheMissFetchesUpstream drives the source-archive
// download through the real RequireReadAccess middleware and the real chi
// router (so the Host-bound registry resolves into context exactly as it
// would in production), fetches the archive from a fake upstream on a
// genuine cache miss, and verifies a second request is served from the
// now-cached copy even after the fake upstream is shut down.
//
// A request for "/{scope}/{name}/{version}.zip" is routed by chi to the
// plain "/{scope}/{name}/{version}" pattern (handleReleaseMetadata), which
// detects the ".zip" suffix and delegates to handleSourceArchive — see
// NewSwiftProxy and handleReleaseMetadata.
func TestHandleSourceArchiveCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	scope := uniqueID("vapor")
	name := "vapor"
	version := "4.0.0"
	archiveData := []byte("upstream swift archive bytes")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, fmt.Sprintf("/%s/%s/%s.zip", scope, name, version), r.URL.Path)
		w.Write(archiveData)
	}))
	defer upstream.Close()

	host := uniqueID("swift-archive-host") + ".test"
	seedRegistryWithHost(t, db, host, upstream.URL, true)

	p := newTestProxy(t, db, c)
	router := NewSwiftProxy(p.db, p.storage, p.cache, rbac.New(db), nil)

	get := func() *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/"+scope+"/"+name+"/"+version+".zip", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Result()
	}

	resp := get()
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, archiveData, body)

	upstream.Close()
	resp2 := get()
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	assert.Equal(t, archiveData, body2)
}

func TestFetchFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	data, err := p.fetchFromUpstream(&database.RegistryConfig{Proxy: false}, "apple/swift-nio/2.0.0/Package.swift")
	assert.Error(t, err)
	assert.Nil(t, data)

	data, err = p.fetchFromUpstream(nil, "apple/swift-nio/2.0.0/Package.swift")
	assert.Error(t, err)
	assert.Nil(t, data)
}

func TestFetchFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte("// swift-tools-version:5.5")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/apple/swift-nio/2.0.0/Package.swift", r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{URL: upstream.URL, Proxy: true}
	data, err := p.fetchFromUpstream(reg, "apple/swift-nio/2.0.0/Package.swift")
	require.NoError(t, err)
	assert.Equal(t, body, data)
}

func TestFetchStreamFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	resp, err := p.fetchStreamFromUpstream(&database.RegistryConfig{Proxy: false}, "apple/swift-nio/2.0.0.zip")
	assert.Error(t, err)
	assert.Nil(t, resp)

	resp, err = p.fetchStreamFromUpstream(nil, "apple/swift-nio/2.0.0.zip")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestFetchStreamFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte("zip bytes")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{URL: upstream.URL, Proxy: true}
	resp, err := p.fetchStreamFromUpstream(reg, "apple/swift-nio/2.0.0.zip")
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestSwiftProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("private-swift-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("private-swift-reg"),
		Name:     "private-swift-reg",
		Type:     "swift",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := NewSwiftProxy(db, nil, c, rbac.New(db), nil)

	req := httptest.NewRequest(http.MethodGet, "/apple/swift-nio", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSwiftProxyRoutesPublicListReleases(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("public-swift-host") + ".test"
	seedRegistryWithHost(t, db, host, "https://upstream.example.com", false)

	router := NewSwiftProxy(db, nil, c, rbac.New(db), nil)

	req := httptest.NewRequest(http.MethodGet, "/apple/swift-nio", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSeedRegistryHelper(t *testing.T) {
	db := connectTestDB(t)
	reg := seedRegistry(t, db, uniqueID("swift-seed-reg"), false, true)
	assert.True(t, reg.Proxy)
	assert.Equal(t, "swift", reg.Type)
}
