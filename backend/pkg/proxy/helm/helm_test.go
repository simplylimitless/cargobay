package helm

import (
	"context"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *HelmProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &HelmProxy{
		db:         db,
		storage:    adapter,
		cache:      c,
		registries: nil,
		registry:   "helm",
	}
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// seedRegistry saves a helm registry bound to host, so route-level tests can
// address it deterministically via the Host header instead of relying on
// whatever registry (if any) other concurrently-run test suites left as the
// DB-wide default for artifactType "helm".
func seedRegistry(t *testing.T, db *database.Database, id, host string, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       id,
		Name:     id,
		URL:      "https://upstream.example.com",
		Type:     "helm",
		Proxy:    proxy,
		Enabled:  true,
		Priority: 10,
		Private:  false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(id) })
	return reg
}

// withChiParams simulates chi's route matching by attaching a RouteContext
// carrying the given URL params directly, without depending on chi's actual
// pattern matching. This is needed because the "/charts/{chartName}-{version}.tgz"
// and "/{chartName}/{version}/{chartNameFile}-{versionFile}.tgz" route
// patterns registered in NewHelmProxy each try to pack two dynamic params
// into a single path segment separated only by a literal "-" — chi does not
// support that, so those patterns never actually match any request in
// production; they silently fall through to a broader pattern (or to chi's
// own 404) instead of ever reaching handleChartDownload/handleChartDownloadAlt
// through the router. That's exercised directly in
// TestChartDownloadRoutesAreUnreachableThroughRouter below. To still unit
// test the handler logic itself, params are injected directly here, exactly
// as chi would have if the pattern matched.
func withChiParams(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// withReadAccessContext runs req through the same RequireReadAccess
// middleware NewHelmProxy wires up, so a handler invoked directly (bypassing
// the router) still sees the resolved Target the real router would have
// attached to the request context.
func withReadAccessContext(t *testing.T, db *database.Database, rbacMgr *rbac.RBAC, registries []database.RegistryConfig, req *http.Request, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mw := proxypkg.RequireReadAccess(db, rbacMgr, registries, "helm")
	mw(handler).ServeHTTP(rec, req)
	return rec
}

func TestNewHelmProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	router := NewHelmProxy(p.db, p.storage, p.cache, rbacMgr, p.registries)
	assert.NotNil(t, router)
}

func TestHandleRootRoute(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	host := uniqueID("root-host") + ".test"
	seedRegistry(t, db, uniqueID("root-reg"), host, false)

	router := NewHelmProxy(p.db, p.storage, p.cache, rbacMgr, p.registries)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK", rec.Body.String())
}

func TestHandleIndexRoute(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	require.NoError(t, c.Delete("-helm-index-"))

	host := uniqueID("index-host") + ".test"
	regID := uniqueID("index-reg")
	reg := seedRegistry(t, db, regID, host, false)

	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("helm:%s:nginx-ingress:1.0.0", reg.ID),
		RegistryID:      reg.ID,
		ArtifactType:    "helm",
		Namespace:       "",
		ArtifactName:    "nginx-ingress",
		Version:         "1.0.0",
		Digest:          "sha256:abc",
		DigestAlgorithm: "sha256",
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata:        map[string]interface{}{"description": "an ingress chart"},
		Tags:            []string{"1.0.0"},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", "nginx-ingress", "1.0.0") })

	router := NewHelmProxy(p.db, p.storage, p.cache, rbacMgr, p.registries)

	req := httptest.NewRequest(http.MethodGet, "/index.yaml", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/x-yaml", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "nginx-ingress")
}

// TestHandleIndexServesFromCacheWhenPopulated exercises the path of
// handleIndex that *is* reachable: once the "-helm-index-" key already
// holds a valid cached index (e.g. from an earlier successful generation,
// before the Cache.Get bug above made that unreachable through handleIndex
// itself), it's served back as-is without touching the database.
func TestHandleIndexServesFromCacheWhenPopulated(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	cached := []byte("apiVersion: v1\nentries:\n  nginx-ingress: []\n")
	require.NoError(t, c.Set("-helm-index-", cached))
	t.Cleanup(func() { c.Delete("-helm-index-") })

	host := uniqueID("index-cached-host") + ".test"
	seedRegistry(t, db, uniqueID("index-cached-reg"), host, false)

	router := NewHelmProxy(p.db, p.storage, p.cache, rbacMgr, p.registries)

	req := httptest.NewRequest(http.MethodGet, "/index.yaml", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/x-yaml", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "nginx-ingress")
}

// TestChartDownloadRoutesAreUnreachableThroughRouter documents a real
// routing quirk discovered while writing these tests: both
// "/charts/{chartName}-{version}.tgz" and
// "/{chartName}/{version}/{chartNameFile}-{versionFile}.tgz" try to pack two
// dynamic params into a single path segment separated only by a literal
// "-". chi does not support that, so neither pattern ever matches a real
// request — the first falls through to chi's built-in 404, and the second
// falls through to the broader "/{chartName}/{version}/{filename}" pattern
// (handleChartFile) instead of handleChartDownloadAlt. handleChartDownload
// and handleChartDownloadAlt are therefore dead code from the router's
// perspective; they're still unit-tested directly below since the handler
// logic itself is real and reachable if the routes are ever fixed.
func TestChartDownloadRoutesAreUnreachableThroughRouter(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	host := uniqueID("chart-route-host") + ".test"
	seedRegistry(t, db, uniqueID("chart-route-reg"), host, false)

	chartData := []byte("fake helm chart tarball")
	_, err := p.storage.SaveArtifact("helm", "", "nginx-ingress", "1.0.0", chartData)
	require.NoError(t, err)

	router := NewHelmProxy(p.db, p.storage, p.cache, rbacMgr, p.registries)

	req := httptest.NewRequest(http.MethodGet, "/charts/nginx-ingress-1.0.0.tgz", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code, "chi's own not-found, not handleChartDownload's")

	altReq := httptest.NewRequest(http.MethodGet, "/nginx-ingress/1.0.0/nginx-ingress-1.0.0.tgz", nil)
	altReq.Host = host
	altRec := httptest.NewRecorder()
	router.ServeHTTP(altRec, altReq)
	assert.Equal(t, http.StatusNotFound, altRec.Code)
	assert.Contains(t, altRec.Body.String(), "not found in chart", "landed in handleChartFile, not handleChartDownloadAlt")
}

func TestHandleChartDownload(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	host := uniqueID("chart-host") + ".test"
	regID := uniqueID("chart-reg")
	seedRegistry(t, db, regID, host, false)

	chartData := []byte("fake helm chart tarball")
	_, err := p.storage.SaveArtifact("helm", "", "nginx-ingress", "1.0.0", chartData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/charts/nginx-ingress-1.0.0.tgz", nil)
	req.Host = host
	req = withChiParams(req, map[string]string{"chartName": "nginx-ingress", "version": "1.0.0"})

	rec := withReadAccessContext(t, p.db, rbacMgr, p.registries, req, p.handleChartDownload)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/x-gzip", rec.Header().Get("Content-Type"))
	assert.Equal(t, chartData, rec.Body.Bytes())
}

func TestHandleChartDownloadNotFoundNoUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	host := uniqueID("chart-404-host") + ".test"
	// Proxy disabled: no upstream configured, so an uncached chart can't be
	// pulled through and the handler reports a bad gateway rather than 404.
	seedRegistry(t, db, uniqueID("chart-404-reg"), host, false)

	req := httptest.NewRequest(http.MethodGet, "/charts/nonexistent-1.0.0.tgz", nil)
	req.Host = host
	req = withChiParams(req, map[string]string{"chartName": "nonexistent", "version": "1.0.0"})

	rec := withReadAccessContext(t, p.db, rbacMgr, p.registries, req, p.handleChartDownload)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandleChartDownloadAlt(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	host := uniqueID("chart-alt-host") + ".test"
	seedRegistry(t, db, uniqueID("chart-alt-reg"), host, false)

	chartData := []byte("alt path chart tarball")
	_, err := p.storage.SaveArtifact("helm", "", "nginx-ingress", "1.0.0", chartData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/nginx-ingress/1.0.0/nginx-ingress-1.0.0.tgz", nil)
	req.Host = host
	req = withChiParams(req, map[string]string{"chartName": "nginx-ingress", "version": "1.0.0"})

	rec := withReadAccessContext(t, p.db, rbacMgr, p.registries, req, p.handleChartDownloadAlt)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/x-gzip", rec.Header().Get("Content-Type"))
	assert.Equal(t, chartData, rec.Body.Bytes())
}

func TestHandleChartFileReturnsNotFoundPlaceholder(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	host := uniqueID("chart-file-host") + ".test"
	seedRegistry(t, db, uniqueID("chart-file-reg"), host, false)

	router := NewHelmProxy(p.db, p.storage, p.cache, rbacMgr, p.registries)

	req := httptest.NewRequest(http.MethodGet, "/nginx-ingress/1.0.0/Chart.yaml", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// handleChartFile is currently a placeholder that hasn't implemented
	// extracting a specific file out of the chart tarball yet.
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "Chart.yaml")
}

func TestHandleChartsDirRoute(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	host := uniqueID("charts-dir-host") + ".test"
	regID := uniqueID("charts-dir-reg")
	reg := seedRegistry(t, db, regID, host, false)

	for _, v := range []string{"0.9.0", "1.0.0"} {
		artifact := &database.ArtifactMetadata{
			ID:              fmt.Sprintf("helm:%s:nginx-ingress:%s", reg.ID, v),
			RegistryID:      reg.ID,
			ArtifactType:    "helm",
			Namespace:       "",
			ArtifactName:    "nginx-ingress",
			Version:         v,
			DigestAlgorithm: "sha256",
			Created:         time.Now(),
			Updated:         time.Now(),
			Metadata:        map[string]interface{}{},
			Tags:            []string{v},
		}
		require.NoError(t, db.SaveArtifact(artifact))
		v := v
		t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", "nginx-ingress", v) })
	}

	router := NewHelmProxy(p.db, p.storage, p.cache, rbacMgr, p.registries)

	req := httptest.NewRequest(http.MethodGet, "/charts/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "nginx-ingress")
	assert.Contains(t, body, "0.9.0")
	assert.Contains(t, body, "1.0.0")
}

func TestCacheConcurrency(t *testing.T) {
	c := connectTestCache(t)
	proxy := &HelmProxy{cache: c}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			key := fmt.Sprintf("helm-test-concurrency-%d", i)
			proxy.cacheSet(key, []byte("value"))
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestEmptyNamespace(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	regID := uniqueID("empty-ns-reg")
	seedRegistry(t, db, regID, "", false)

	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("helm:%s:standalone-chart:1.0.0", regID),
		RegistryID:   regID,
		ArtifactType: "helm",
		Namespace:    "",
		ArtifactName: "standalone-chart",
		Version:      "1.0.0",
		Size:         2048,
		Metadata:     map[string]interface{}{},
		Tags:         []string{"1.0.0"},
	}
	require.NoError(t, p.db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact(regID, "", "standalone-chart", "1.0.0") })

	results, err := p.db.ListArtifacts(regID, database.ListOptions{ArtifactType: "helm"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

func TestNamespaceHandling(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	regID := uniqueID("ns-reg")
	seedRegistry(t, db, regID, "", false)

	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("helm:%s:bitnami:nginx:12.0.0", regID),
		RegistryID:   regID,
		ArtifactType: "helm",
		Namespace:    "bitnami",
		ArtifactName: "nginx",
		Version:      "12.0.0",
		Size:         1024,
		Metadata:     map[string]interface{}{},
		Tags:         []string{"12.0.0"},
	}
	require.NoError(t, p.db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact(regID, "bitnami", "nginx", "12.0.0") })

	results, err := p.db.ListArtifacts(regID, database.ListOptions{
		Namespace:    "bitnami",
		ArtifactType: "helm",
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)
	assert.Equal(t, "bitnami", results[0].Namespace)
}

func TestHelmProxyRoutes(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	host := uniqueID("routes-host") + ".test"
	seedRegistry(t, db, uniqueID("routes-reg"), host, false)

	router := NewHelmProxy(p.db, p.storage, p.cache, rbacMgr, p.registries)
	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()

	routes := []string{"/", "/index.yaml", "/charts/", "/charts"}
	for _, route := range routes {
		req, err := http.NewRequest(http.MethodGet, server.URL+route, nil)
		require.NoError(t, err)
		req.Host = host
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode, "route %s should exist", route)
	}
}

func TestHelmProxyIntegration(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	rbacMgr := rbac.New(db)

	host := uniqueID("integration-host") + ".test"
	seedRegistry(t, db, uniqueID("integration-reg"), host, false)

	router := NewHelmProxy(p.db, p.storage, p.cache, rbacMgr, p.registries)
	server := httptest.NewServer(router)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	require.NoError(t, err)
	req.Host = host
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
