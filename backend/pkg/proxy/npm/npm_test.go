package npm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache, registries []database.RegistryConfig) *NPMProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &NPMProxy{
		db:         db,
		storage:    adapter,
		cache:      c,
		rbacMgr:    rbac.New(db),
		registries: registries,
		registry:   "npm",
	}
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

func seedRegistry(t *testing.T, db *database.Database, id string, private, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       id,
		Name:     id,
		URL:      "https://upstream.example.com",
		Type:     "npm",
		Proxy:    proxy,
		Enabled:  true,
		Priority: 10,
		Private:  private,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(id) })
	return reg
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func mustMarshal(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestNewNPMProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	registries := []database.RegistryConfig{{ID: "npm", Name: "NPM Registry", Type: "npm", Proxy: true, Enabled: true}}

	router := NewNPMProxy(db, nil, c, rbac.New(db), registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

func TestHandleRoot(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	p.handleRoot(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "cargobay-npm", body["name"])
	assert.Equal(t, "0.1.0", body["version"])
	assert.Contains(t, body, "description")
}

func TestHandlePing(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	req := httptest.NewRequest(http.MethodGet, "/-/ping", nil)
	rec := httptest.NewRecorder()

	p.handlePing(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	assert.Equal(t, "OK", rec.Body.String())
}

func TestHandleUser(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	req := httptest.NewRequest(http.MethodGet, "/-/user", nil)
	rec := httptest.NewRecorder()

	p.handleUser(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Empty(t, body)
}

func TestHandleUserSync(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	req := httptest.NewRequest(http.MethodGet, "/-/user/sync", nil)
	rec := httptest.NewRecorder()

	p.handleUserSync(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, true, body["ok"])
}

// TestHandlePackageCacheHit covers the fast path where the package's
// metadata is already cached — no registry/upstream is needed, so the
// request is exercised directly against the handler with no context target
// set (TargetFromContext falls back to Target{Label: "npm"}).
func TestHandlePackageCacheHit(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	pkgName := uniqueID("express")
	mockPackage := map[string]interface{}{
		"name":      pkgName,
		"dist-tags": map[string]interface{}{"latest": "4.18.0"},
	}
	require.NoError(t, c.Set(pkgName, mustMarshal(mockPackage)))

	req := httptest.NewRequest(http.MethodGet, "/"+pkgName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pkgName", pkgName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, pkgName, body["name"])
}

// TestHandlePackageCacheMissFetchesUpstream covers handlePackage on a
// genuine cache miss: cache.Cache.Get now returns cache.ErrCacheMiss rather
// than a nil error (see pkg/cache/redis.go), so handlePackage's
// `if data, err := p.cacheGet(...); err == nil` check correctly falls
// through to fetchFromUpstream instead of "succeeding" with an empty body.
// With no upstream registry resolvable from context in this direct handler
// call, that fetch fails and the real response is 502.
func TestHandlePackageCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	pkgName := uniqueID("never-cached-pkg")
	req := httptest.NewRequest(http.MethodGet, "/"+pkgName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pkgName", pkgName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandlePackageVersionCacheHit(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	pkgName := uniqueID("express")
	mockVersion := map[string]interface{}{
		"name":    pkgName,
		"version": "4.18.0",
	}
	cacheKey := fmt.Sprintf("%s:%s", pkgName, "4.18.0")
	require.NoError(t, c.Set(cacheKey, mustMarshal(mockVersion)))

	req := httptest.NewRequest(http.MethodGet, "/"+pkgName+"/4.18.0", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pkgName", pkgName)
	rctx.URLParams.Add("version", "4.18.0")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackageVersion(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, pkgName, body["name"])
	assert.Equal(t, "4.18.0", body["version"])
}

func TestHandleScopedPackageCacheHit(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	scope := uniqueID("types")
	pkgName := "node"
	packageName := fmt.Sprintf("@%s/%s", scope, pkgName)
	mockPackage := map[string]interface{}{
		"name":      packageName,
		"dist-tags": map[string]interface{}{"latest": "20.0.0"},
	}
	require.NoError(t, c.Set(packageName, mustMarshal(mockPackage)))

	req := httptest.NewRequest(http.MethodGet, "/@"+scope+"/"+pkgName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("scope", scope)
	rctx.URLParams.Add("pkgName", pkgName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleScopedPackage(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, packageName, body["name"])
}

func TestHandleScopedPackageVersionCacheHit(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	scope := uniqueID("types")
	pkgName := "node"
	version := "20.0.0"
	packageName := fmt.Sprintf("@%s/%s", scope, pkgName)
	mockVersion := map[string]interface{}{
		"name":    packageName,
		"version": version,
	}
	cacheKey := fmt.Sprintf("%s:%s", packageName, version)
	require.NoError(t, c.Set(cacheKey, mustMarshal(mockVersion)))

	req := httptest.NewRequest(http.MethodGet, "/@"+scope+"/"+pkgName+"/"+version, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("scope", scope)
	rctx.URLParams.Add("pkgName", pkgName)
	rctx.URLParams.Add("version", version)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleScopedPackageVersion(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, packageName, body["name"])
	assert.Equal(t, version, body["version"])
}

// withChiRouteContext attaches rctx to r's context the way chi's router
// does, so handlers reading chi.URLParam(r, ...) work when called directly
// (bypassing the router/mux).
func withChiRouteContext(r *http.Request, rctx *chi.Context) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestFetchFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	reg := &database.RegistryConfig{ID: "npm", Proxy: false}
	data, err := p.fetchFromUpstream(reg, "/express")
	assert.Error(t, err)
	assert.Nil(t, data)

	data, err = p.fetchFromUpstream(nil, "/express")
	assert.Error(t, err)
	assert.Nil(t, data)
}

func TestFetchFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	body := []byte(`{"name":"express","dist-tags":{"latest":"4.18.0"}}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/express", r.URL.Path)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "npm", URL: upstream.URL, Proxy: true}
	data, err := p.fetchFromUpstream(reg, "/express")
	require.NoError(t, err)
	assert.Equal(t, body, data)
}

func TestGetTarballURLSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	tarballURL := "https://upstream.example.com/express/-/express-4.18.0.tgz"
	body := mustMarshal(map[string]interface{}{
		"name": "express",
		"dist": map[string]interface{}{"tarball": tarballURL},
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/express/4.18.0", r.URL.Path)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "npm", URL: upstream.URL, Proxy: true}
	got, err := p.getTarballURL(reg, "express", "4.18.0")
	require.NoError(t, err)
	assert.Equal(t, tarballURL, got)
}

func TestGetTarballURLMissingDistField(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	body := mustMarshal(map[string]interface{}{"name": "express"})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "npm", URL: upstream.URL, Proxy: true}
	got, err := p.getTarballURL(reg, "express", "4.18.0")
	assert.Error(t, err)
	assert.Empty(t, got)
}

func TestGetTarballURLNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	got, err := p.getTarballURL(nil, "express", "4.18.0")
	assert.Error(t, err)
	assert.Empty(t, got)
}

func TestSavePackageMetadata(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	regLabel := uniqueID("save-meta-reg")
	pkgName := uniqueID("express")
	mockData := mustMarshal(map[string]interface{}{
		"name":      pkgName,
		"dist-tags": map[string]interface{}{"latest": "4.18.0"},
		"versions":  map[string]interface{}{"4.18.0": map[string]interface{}{"name": pkgName, "version": "4.18.0"}},
	})

	p.savePackageMetadata(regLabel, pkgName, mockData)
	t.Cleanup(func() { db.DeleteArtifact(regLabel, "", pkgName, "4.18.0") })

	artifacts, err := db.ListArtifacts(regLabel, database.ListOptions{ArtifactType: "npm"})
	require.NoError(t, err)
	var found *database.ArtifactMetadata
	for i := range artifacts {
		if artifacts[i].ArtifactName == pkgName {
			found = &artifacts[i]
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, regLabel, found.RegistryID)
	assert.Equal(t, "4.18.0", found.Version)
	assert.Equal(t, "", found.Namespace)
}

func TestSavePackageVersionMetadata(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	regLabel := uniqueID("save-ver-meta-reg")
	pkgName := uniqueID("express")
	mockData := mustMarshal(map[string]interface{}{
		"name":    pkgName,
		"version": "4.18.0",
	})

	p.savePackageVersionMetadata(regLabel, pkgName, "4.18.0", mockData)
	t.Cleanup(func() { db.DeleteArtifact(regLabel, "", pkgName, "4.18.0") })

	artifacts, err := db.ListArtifacts(regLabel, database.ListOptions{ArtifactType: "npm"})
	require.NoError(t, err)
	var found *database.ArtifactMetadata
	for i := range artifacts {
		if artifacts[i].ArtifactName == pkgName {
			found = &artifacts[i]
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, "4.18.0", found.Version)
}

// TestTarballDownloadFromStorage covers handleTarball's early-return path
// when the tarball is already cached in local storage — no upstream target
// is needed since fetchIfUpdated with a nil registry fails closed and keeps
// the locally stored bytes.
func TestTarballDownloadFromStorage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	pkgName := uniqueID("express")
	tarballData := []byte("fake tarball content")
	_, err := p.storage.SaveArtifact("npm", "", pkgName, "4.18.0", tarballData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/"+pkgName+"/-/"+pkgName+"-4.18.0.tgz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pkgName", pkgName)
	rctx.URLParams.Add("*", pkgName+"-4.18.0.tgz")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleTarball(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, tarballData, rec.Body.Bytes())
}

// TestTarballNotFoundReturnsBadGateway covers handleTarball when the
// tarball isn't in local storage: it falls through to getTarballURL, which
// fails (no registry resolved from context in this direct handler call), so
// the real response is 502 — not a 404 as the old mock-based test assumed.
func TestTarballNotFoundReturnsBadGateway(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	pkgName := uniqueID("nonexistent")
	req := httptest.NewRequest(http.MethodGet, "/"+pkgName+"/-/"+pkgName+"-1.0.0.tgz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pkgName", pkgName)
	rctx.URLParams.Add("*", pkgName+"-1.0.0.tgz")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleTarball(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestScopedTarballDownloadFromStorage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	scope := uniqueID("types")
	pkgName := "node"
	tarballData := []byte("scoped tarball content")
	_, err := p.storage.SaveArtifact("npm", scope, pkgName, "20.0.0", tarballData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/@"+scope+"/"+pkgName+"/-/"+pkgName+"-20.0.0.tgz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("scope", scope)
	rctx.URLParams.Add("pkgName", pkgName)
	rctx.URLParams.Add("*", pkgName+"-20.0.0.tgz")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleScopedTarball(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, tarballData, rec.Body.Bytes())
}

// TestHandleTarballViaRouterCacheMiss drives handleTarball through the real
// chi router (verifying the "/{pkgName}/-/*" wildcard route added to work
// around chi's inability to match a literal ".tgz" suffix glued onto a
// {param} in the same segment actually dispatches real tarball requests),
// fetches package version metadata and the tarball itself from a fake
// upstream on a genuine cache miss, and confirms the tarball is both served
// and durably cached.
func TestHandleTarballViaRouterCacheMiss(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	pkgName := uniqueID("tarball-pkg")
	version := "3.1.4"
	tarballData := []byte("upstream tarball bytes")

	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case fmt.Sprintf("/%s/%s", pkgName, version):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":    pkgName,
				"version": version,
				"dist":    map[string]interface{}{"tarball": upstream.URL + "/" + pkgName + "/-/" + pkgName + "-" + version + ".tgz"},
			})
		case fmt.Sprintf("/%s/-/%s-%s.tgz", pkgName, pkgName, version):
			w.Write(tarballData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	host := uniqueID("tarball-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("tarball-reg"),
		Name:     "tarball-reg",
		URL:      upstream.URL,
		Type:     "npm",
		Proxy:    true,
		Enabled:  true,
		Priority: 100,
		Private:  false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	router := NewNPMProxy(db, adapter, c, rbac.New(db), nil)

	get := func() *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/"+pkgName+"/-/"+pkgName+"-"+version+".tgz", nil)
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
	assert.Equal(t, tarballData, body)

	upstream.Close()
	resp2 := get()
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	assert.Equal(t, tarballData, body2)
}

func TestCacheGetAndSet(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	key := uniqueID("test-key")
	data := []byte(`{"test":"data"}`)
	p.cacheSet(key, data)

	retrieved, err := p.cacheGet(key)
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)
}

// TestCacheGetMissReturnsErrCacheMiss documents cache.Cache.Get's real
// contract at the cacheGet wrapper level: a miss returns cache.ErrCacheMiss,
// distinguishing it from a genuine hit, and the returned slice is nil.
func TestCacheGetMissReturnsErrCacheMiss(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	retrieved, err := p.cacheGet(uniqueID("never-set-key"))
	require.ErrorIs(t, err, cache.ErrCacheMiss)
	assert.Nil(t, retrieved)
}

func TestCacheConcurrency(t *testing.T) {
	c := connectTestCache(t)
	db := connectTestDB(t)
	p := newTestProxy(t, db, c, nil)

	key := uniqueID("concurrent-key")
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			p.cacheSet(key, []byte(fmt.Sprintf(`{"value":%d}`, i)))
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	retrieved, err := p.cacheGet(key)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
}

func TestNPMProxyIntegrationRoot(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("integration-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("integration-reg"),
		Name:     "integration-reg",
		Type:     "npm",
		Enabled:  true,
		Priority: 100,
		Private:  false,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := NewNPMProxy(db, nil, c, rbac.New(db), nil)
	server := httptest.NewServer(router)
	defer server.Close()

	httpReq, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	require.NoError(t, err)
	httpReq.Host = host

	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "cargobay-npm", body["name"])
}

// TestNPMProxyRoutesPrivateRequiresAuth verifies the RequireReadAccess
// middleware mounted by NewNPMProxy enforces access control end to end: an
// anonymous request against a private, Host-bound registry is rejected
// before it ever reaches a handler.
func TestNPMProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("private-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("private-reg"),
		Name:     "private-reg",
		Type:     "npm",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := NewNPMProxy(db, nil, c, rbac.New(db), nil)

	req := httptest.NewRequest(http.MethodGet, "/-/ping", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestNPMProxyRoutesPublicPing verifies an anonymous request against a
// public, Host-bound registry reaches the handler successfully.
func TestNPMProxyRoutesPublicPing(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("public-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("public-reg"),
		Name:     "public-reg",
		Type:     "npm",
		Enabled:  true,
		Priority: 100,
		Private:  false,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := NewNPMProxy(db, nil, c, rbac.New(db), nil)

	req := httptest.NewRequest(http.MethodGet, "/-/ping", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK", rec.Body.String())
}

// publishPayload builds a minimal npm publish request body for pkgName@version
// with a single tarball attachment.
func publishPayload(pkgName, version string, tarball []byte) []byte {
	return mustMarshal(map[string]interface{}{
		"name":      pkgName,
		"dist-tags": map[string]interface{}{"latest": version},
		"versions": map[string]interface{}{
			version: map[string]interface{}{"name": pkgName, "version": version},
		},
		"_attachments": map[string]interface{}{
			fmt.Sprintf("%s-%s.tgz", pkgName, version): map[string]interface{}{
				"content_type": "application/octet-stream",
				"data":         base64.StdEncoding.EncodeToString(tarball),
				"length":       len(tarball),
			},
		},
	})
}

func TestHandlePublishRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-noauth-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-noauth-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	router := NewNPMProxy(db, nil, c, rbac.New(db), nil)

	pkgName := uniqueID("pkg")
	body := publishPayload(pkgName, "1.0.0", []byte("tarball bytes"))
	req := httptest.NewRequest(http.MethodPut, "/"+pkgName, strings.NewReader(string(body)))
	req.Host = host
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandlePublishRequiresPublishGrant(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-noperm-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-noperm-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	router := NewNPMProxy(db, nil, c, rbac.New(db), nil)

	pkgName := uniqueID("pkg")
	body := publishPayload(pkgName, "1.0.0", []byte("tarball bytes"))
	req := httptest.NewRequest(http.MethodPut, "/"+pkgName, strings.NewReader(string(body)))
	req.Host = host
	req = authedRequest(req, uniqueID("user"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandlePublishSucceedsWithPublishGrant(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-ok-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-ok-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: reg.ID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(reg.ID, userID) })

	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	router := NewNPMProxy(db, adapter, c, rbac.New(db), nil)

	pkgName := uniqueID("pkg")
	version := "1.0.0"
	tarball := []byte("tarball bytes")
	body := publishPayload(pkgName, version, tarball)

	req := httptest.NewRequest(http.MethodPut, "/"+pkgName, strings.NewReader(string(body)))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", pkgName, version) })

	stored, err := adapter.GetArtifact(reg.ID, "", pkgName, version)
	require.NoError(t, err)
	assert.Equal(t, tarball, stored)

	artifacts, err := db.ListArtifacts(reg.ID, database.ListOptions{ArtifactType: "npm"})
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, pkgName, artifacts[0].ArtifactName)
	assert.Equal(t, version, artifacts[0].Version)
}

func TestHandleScopedPublishSucceedsWithPublishGrant(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("scoped-publish-ok-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("scoped-publish-ok-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: reg.ID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(reg.ID, userID) })

	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	router := NewNPMProxy(db, adapter, c, rbac.New(db), nil)

	scope := uniqueID("scope")
	pkgName := "node"
	fullName := fmt.Sprintf("@%s/%s", scope, pkgName)
	version := "1.0.0"
	tarball := []byte("scoped tarball bytes")
	body := publishPayload(pkgName, version, tarball)

	req := httptest.NewRequest(http.MethodPut, "/@"+scope+"/"+pkgName, strings.NewReader(string(body)))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, scope, pkgName, version) })

	stored, err := adapter.GetArtifact(reg.ID, scope, pkgName, version)
	require.NoError(t, err)
	assert.Equal(t, tarball, stored)

	artifacts, err := db.ListArtifacts(reg.ID, database.ListOptions{ArtifactType: "npm", Namespace: scope})
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, pkgName, artifacts[0].ArtifactName)
	assert.Equal(t, scope, artifacts[0].Namespace)

	var body2 map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body2))
	assert.Equal(t, fullName, body2["id"])
}

func TestHandlePublishMalformedJSON(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-badjson-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-badjson-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: reg.ID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(reg.ID, userID) })

	router := NewNPMProxy(db, nil, c, rbac.New(db), nil)

	pkgName := uniqueID("pkg")
	req := httptest.NewRequest(http.MethodPut, "/"+pkgName, strings.NewReader("not json"))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandlePublishGroupGrantSucceeds proves CanPublishRegistry's
// group-based grant path (GroupRegistryAccess.CanPublish) is sufficient to
// publish, without any per-user RegistryAccess row at all.
func TestHandlePublishGroupGrantSucceeds(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-group-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-group-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	grp, err := db.CreateGroup(uniqueID("publishers"), "publishers group")
	require.NoError(t, err)
	t.Cleanup(func() { db.DeleteGroup(grp.ID) })

	userID := uniqueID("user")
	require.NoError(t, db.AddGroupMember(grp.ID, userID))
	require.NoError(t, db.GrantGroupRegistryAccess(&database.GroupRegistryAccess{
		RegistryID: reg.ID,
		GroupID:    grp.ID,
		CanRead:    true,
		CanPublish: true,
	}))

	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	router := NewNPMProxy(db, adapter, c, rbac.New(db), nil)

	pkgName := uniqueID("pkg")
	version := "1.0.0"
	tarball := []byte("group-granted tarball bytes")
	body := publishPayload(pkgName, version, tarball)

	req := httptest.NewRequest(http.MethodPut, "/"+pkgName, strings.NewReader(string(body)))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", pkgName, version) })
}

// TestHandlePublishNonPrivateRegistryDenied proves CanPublishRegistry
// denies publish to a public (non-private) registry even for a user with an
// explicit CanPublish grant recorded against it — mirroring how Docker push
// is denied against non-private registries.
func TestHandlePublishNonPrivateRegistryDenied(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-public-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-public-reg"), false, false) // private=false
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: reg.ID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(reg.ID, userID) })

	router := NewNPMProxy(db, nil, c, rbac.New(db), nil)

	pkgName := uniqueID("pkg")
	body := publishPayload(pkgName, "1.0.0", []byte("tarball bytes"))
	req := httptest.NewRequest(http.MethodPut, "/"+pkgName, strings.NewReader(string(body)))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestHandlePublishReadScopeDenied proves a read-scoped credential (e.g. a
// read-only personal access token) can never publish, even with an
// otherwise-valid CanPublish grant on a private registry.
func TestHandlePublishReadScopeDenied(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-readscope-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-readscope-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: reg.ID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(reg.ID, userID) })

	router := NewNPMProxy(db, nil, c, rbac.New(db), nil)

	pkgName := uniqueID("pkg")
	body := publishPayload(pkgName, "1.0.0", []byte("tarball bytes"))
	req := httptest.NewRequest(http.MethodPut, "/"+pkgName, strings.NewReader(string(body)))
	req.Host = host
	ctx := context.WithValue(req.Context(), middleware.AuthUserKey, &middleware.User{UserID: userID, Username: userID, Scope: "read"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// multiAttachmentPublishPayload builds an npm publish body with several
// tarball attachments in a single request, as npm CLI can send when
// republishing dist-tags across multiple existing versions.
func multiAttachmentPublishPayload(pkgName string, versions map[string][]byte) []byte {
	attachments := map[string]interface{}{}
	versionsMeta := map[string]interface{}{}
	for version, tarball := range versions {
		attachments[fmt.Sprintf("%s-%s.tgz", pkgName, version)] = map[string]interface{}{
			"content_type": "application/octet-stream",
			"data":         base64.StdEncoding.EncodeToString(tarball),
			"length":       len(tarball),
		}
		versionsMeta[version] = map[string]interface{}{"name": pkgName, "version": version}
	}
	return mustMarshal(map[string]interface{}{
		"name":         pkgName,
		"dist-tags":    map[string]interface{}{"latest": "2.0.0"},
		"versions":     versionsMeta,
		"_attachments": attachments,
	})
}

// TestHandlePublishMultipleVersionsInOnePayload covers a single npm publish
// call carrying attachments for more than one version, asserting each is
// stored and surfaced independently.
func TestHandlePublishMultipleVersionsInOnePayload(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-multi-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-multi-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: reg.ID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(reg.ID, userID) })

	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	router := NewNPMProxy(db, adapter, c, rbac.New(db), nil)

	pkgName := uniqueID("pkg")
	versions := map[string][]byte{
		"1.0.0": []byte("v1 tarball bytes"),
		"2.0.0": []byte("v2 tarball bytes"),
	}
	body := multiAttachmentPublishPayload(pkgName, versions)

	req := httptest.NewRequest(http.MethodPut, "/"+pkgName, strings.NewReader(string(body)))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	for version := range versions {
		version := version
		t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", pkgName, version) })
	}

	for version, tarball := range versions {
		stored, err := adapter.GetArtifact(reg.ID, "", pkgName, version)
		require.NoError(t, err)
		assert.Equal(t, tarball, stored)
	}

	artifacts, err := db.ListArtifacts(reg.ID, database.ListOptions{ArtifactType: "npm"})
	require.NoError(t, err)
	require.Len(t, artifacts, 2)
}

// TestHandlePublishRepublishOverwritesVersion covers publishing the same
// version twice: the second publish should overwrite the stored tarball
// rather than erroring or duplicating the artifact row.
func TestHandlePublishRepublishOverwritesVersion(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-republish-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-republish-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: reg.ID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(reg.ID, userID) })

	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	router := NewNPMProxy(db, adapter, c, rbac.New(db), nil)

	pkgName := uniqueID("pkg")
	version := "1.0.0"
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", pkgName, version) })

	publish := func(tarball []byte) int {
		body := publishPayload(pkgName, version, tarball)
		req := httptest.NewRequest(http.MethodPut, "/"+pkgName, strings.NewReader(string(body)))
		req.Host = host
		req = authedRequest(req, userID)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusCreated, publish([]byte("first tarball")))
	require.Equal(t, http.StatusCreated, publish([]byte("second tarball")))

	stored, err := adapter.GetArtifact(reg.ID, "", pkgName, version)
	require.NoError(t, err)
	assert.Equal(t, []byte("second tarball"), stored)

	artifacts, err := db.ListArtifacts(reg.ID, database.ListOptions{ArtifactType: "npm"})
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
}

func TestHandlePublishMissingAttachments(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-noattach-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-noattach-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	userID := uniqueID("user")
	require.NoError(t, db.GrantRegistryAccess(&database.RegistryAccess{
		RegistryID: reg.ID,
		UserID:     userID,
		CanRead:    true,
		CanPublish: true,
	}))
	t.Cleanup(func() { db.RevokeRegistryAccess(reg.ID, userID) })

	router := NewNPMProxy(db, nil, c, rbac.New(db), nil)

	pkgName := uniqueID("pkg")
	body := mustMarshal(map[string]interface{}{
		"name":      pkgName,
		"dist-tags": map[string]interface{}{"latest": "1.0.0"},
	})
	req := httptest.NewRequest(http.MethodPut, "/"+pkgName, strings.NewReader(string(body)))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
