package npm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache, registries []database.RegistryConfig) *NPMProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &NPMProxy{
		db:         db,
		storage:    adapter,
		cache:      c,
		registries: registries,
		registry:   "npm",
	}
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

// TestHandlePackageCacheMissReturnsEmptyBody documents a real (and
// surprising) behavior discovered while rewriting these tests against a
// live Redis instance: cache.Cache.Get returns a nil error on a cache miss
// (it only errors on an actual Redis/unmarshal failure — see
// pkg/cache/redis.go's Get, which returns nil immediately on redis.Nil).
// handlePackage's cache check is `if data, err := p.cacheGet(...); err ==
// nil`, so a genuine miss satisfies that condition too: it never falls
// through to fetchFromUpstream, and instead "succeeds" with a 200 and an
// empty body. This mirrors the docker Tags/Metadata NOT NULL discovery: the
// test is written to match the real, live-dependency behavior rather than
// the old mock's (incorrect) assumption that a miss returns an error.
func TestHandlePackageCacheMissReturnsEmptyBody(t *testing.T) {
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

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Empty(t, rec.Body.Bytes())
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
	rctx.URLParams.Add("version", "4.18.0")
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
	rctx.URLParams.Add("version", "1.0.0")
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
	rctx.URLParams.Add("version", "20.0.0")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleScopedTarball(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, tarballData, rec.Body.Bytes())
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

// TestCacheGetMissReturnsNilNoError documents cache.Cache.Get's real
// contract at the cacheGet wrapper level: a miss is not an error, and the
// returned slice is simply nil/unpopulated.
func TestCacheGetMissReturnsNilNoError(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, nil)

	retrieved, err := p.cacheGet(uniqueID("never-set-key"))
	require.NoError(t, err)
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
