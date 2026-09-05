package pub

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
	proxypkg "github.com/simplylimitless/cargobay/backend/pkg/proxy"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *PubProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &PubProxy{db: db, storage: adapter, cache: c}
}

func seedRegistry(t *testing.T, db *database.Database, id string, private, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       id,
		Name:     id,
		URL:      "https://upstream.example.com",
		Type:     "dart",
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
// suites left as the DB-wide default for artifactType "dart".
func seedRegistryWithHost(t *testing.T, db *database.Database, host, upstreamURL string, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("dart-host-reg"),
		Name:     "dart-host-reg",
		URL:      upstreamURL,
		Type:     "dart",
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

func TestNewPubProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	registries := []database.RegistryConfig{{ID: "dart", Name: "Dart Registry", Type: "dart", Proxy: true, Enabled: true}}

	router := NewPubProxy(db, nil, c, rbac.New(db), registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

// TestHandlePackageInfoFromDB covers handlePackageInfo when versions are
// already recorded in the database — no upstream registry is needed since
// versions were found. Called directly (no context target set),
// TargetFromContext falls back to Target{Label: "dart"}, so the artifact
// must be seeded under registry ID "dart".
func TestHandlePackageInfoFromDB(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("http")
	artifact := &database.ArtifactMetadata{
		RegistryID:   "dart",
		ArtifactType: "dart",
		ArtifactName: pkgName,
		Version:      "1.0.0",
		Tags:         []string{"1.0.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("dart", "", pkgName, "1.0.0") })

	req := httptest.NewRequest(http.MethodGet, "/api/packages/"+pkgName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", pkgName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackageInfo(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, pkgName, body["name"])
	versions, ok := body["versions"].([]interface{})
	require.True(t, ok)
	require.Len(t, versions, 1)
}

// TestHandlePackageInfoNoVersionsUpstreamFails covers the no-versions-in-DB
// path: with no upstream registry resolvable from context in this direct
// handler call, fetchInfoFromUpstream fails and the handler reports 404.
func TestHandlePackageInfoNoVersionsUpstreamFails(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("never-cached-pkg")
	req := httptest.NewRequest(http.MethodGet, "/api/packages/"+pkgName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", pkgName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackageInfo(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleArchiveFromOldBufferedStorage proves an archive already saved to
// disk via the old buffered storage.SaveArtifact call (as any artifact
// cached before this streaming refactor would have been) is still served
// correctly now that handleArchive reads it back via GetArtifactStream.
func TestHandleArchiveFromOldBufferedStorage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("http")
	version := "1.0.0"
	archiveData := []byte("fake pub archive content")
	_, err := p.storage.SaveArtifact("dart", "", pkgName, version, archiveData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/packages/"+pkgName+"/versions/"+version+".tar.gz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", pkgName)
	rctx.URLParams.Add("*", version+".tar.gz")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleArchive(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/gzip", rec.Header().Get("Content-Type"))
	assert.Equal(t, archiveData, rec.Body.Bytes())
}

func TestHandleArchiveNoUpstreamConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("missing-pkg")
	req := httptest.NewRequest(http.MethodGet, "/api/packages/"+pkgName+"/versions/1.0.0.tar.gz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", pkgName)
	rctx.URLParams.Add("*", "1.0.0.tar.gz")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleArchive(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleArchiveCacheMissFetchesUpstream drives handleArchive through the
// real RequireReadAccess middleware and the real chi router (so the
// Host-bound registry resolves into context exactly as it would in
// production), fetches from a fake upstream server on a genuine cache miss,
// and verifies the archive is both served and durably cached (a second
// request succeeds even after the fake upstream is shut down).
func TestHandleArchiveCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	pkgName := uniqueID("archive-pkg")
	version := "2.0.0"
	archiveData := []byte("upstream archive bytes")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, fmt.Sprintf("/api/packages/%s/versions/%s.tar.gz", pkgName, version), r.URL.Path)
		w.Write(archiveData)
	}))
	defer upstream.Close()

	host := uniqueID("archive-host") + ".test"
	seedRegistryWithHost(t, db, host, upstream.URL, true)

	p := newTestProxy(t, db, c)
	router := chi.NewRouter()
	router.Use(proxypkg.RequireReadAccess(db, rbac.New(db), nil, "dart"))
	router.Get("/api/packages/{name}/versions/*", p.handleArchive)

	get := func() *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/api/packages/"+pkgName+"/versions/"+version+".tar.gz", nil)
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

func TestFetchInfoFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	reg := &database.RegistryConfig{ID: "dart", Proxy: false}
	info, err := p.fetchInfoFromUpstream(reg, "http")
	assert.Error(t, err)
	assert.Nil(t, info)

	info, err = p.fetchInfoFromUpstream(nil, "http")
	assert.Error(t, err)
	assert.Nil(t, info)
}

func TestFetchInfoFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := map[string]interface{}{"name": "http", "latest": map[string]interface{}{"version": "1.0.0"}}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/packages/http", r.URL.Path)
		json.NewEncoder(w).Encode(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "dart", URL: upstream.URL, Proxy: true}
	info, err := p.fetchInfoFromUpstream(reg, "http")
	require.NoError(t, err)
	assert.Equal(t, "http", info["name"])
}

func TestFetchStreamFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	resp, err := p.fetchStreamFromUpstream(&database.RegistryConfig{Proxy: false}, "api/packages/http/versions/1.0.0.tar.gz")
	assert.Error(t, err)
	assert.Nil(t, resp)

	resp, err = p.fetchStreamFromUpstream(nil, "api/packages/http/versions/1.0.0.tar.gz")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestFetchStreamFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte("stream me")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{URL: upstream.URL, Proxy: true}
	resp, err := p.fetchStreamFromUpstream(reg, "some/path")
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

// TestFetchFromUpstreamBuffered exercises the buffered fetchFromUpstream
// helper directly. It isn't wired into any handler in pub.go (handleArchive
// uses the streaming variant instead), but it remains a callable exported
// method worth covering directly.
func TestFetchFromUpstreamBuffered(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte(`{"name":"http"}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{URL: upstream.URL, Proxy: true}
	data, err := p.fetchFromUpstream(reg, "api/packages/http")
	require.NoError(t, err)
	assert.Equal(t, body, data)

	data, err = p.fetchFromUpstream(nil, "api/packages/http")
	assert.Error(t, err)
	assert.Nil(t, data)
}

func TestPubProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("private-dart-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("private-dart-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	router := NewPubProxy(db, nil, c, rbac.New(db), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/packages/http", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
