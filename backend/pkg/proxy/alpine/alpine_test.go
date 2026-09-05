package alpine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *AlpineProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &AlpineProxy{db: db, storage: adapter, cache: c}
}

func newTestRouter(p *AlpineProxy, registries []database.RegistryConfig) http.Handler {
	return NewAlpineProxy(p.db, p.storage, p.cache, rbac.New(p.db), registries)
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// seedRegistryWithHost saves a registry bound to a virtual host, so
// resolveTarget/ResolveRegistry picks it deterministically instead of
// falling back to whatever registry (if any) other concurrently-run tests
// left as the DB-wide default for artifactType "alpine".
func seedRegistryWithHost(t *testing.T, db *database.Database, host string, proxyURL string, proxyEnabled bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("alpine-host-reg"),
		Name:     "alpine-host-reg",
		URL:      proxyURL,
		Type:     "alpine",
		Proxy:    proxyEnabled,
		Enabled:  true,
		Priority: 100,
		Private:  false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })
	return reg
}

func withChiRouteContext(r *http.Request, rctx *chi.Context) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestNewAlpineProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	registries := []database.RegistryConfig{{ID: "alpine", Name: "Alpine", Type: "alpine", Proxy: true, Enabled: true}}

	router := NewAlpineProxy(db, nil, c, rbac.New(db), registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

// TestSplitAPKFileName covers the "{name}-{version}-r{release}.apk" parsing
// convention: version segments start with a digit, and everything up to the
// last such segment is the package name.
func TestSplitAPKFileName(t *testing.T) {
	tests := []struct {
		fileName    string
		wantName    string
		wantVersion string
	}{
		{"curl-8.5.0-r0.apk", "curl", "8.5.0-r0"},
		{"my-package-1.2.3-r1.apk", "my-package", "1.2.3-r1"},
		{"a-1.0.apk", "a", "1.0"},
		{"noversion.apk", "", ""},
		{"-1.0.apk", "", "1.0"},
	}
	for _, tt := range tests {
		name, version := splitAPKFileName(tt.fileName)
		assert.Equal(t, tt.wantName, name, "fileName=%s", tt.fileName)
		assert.Equal(t, tt.wantVersion, version, "fileName=%s", tt.fileName)
	}
}

// TestBuildAPKIndex verifies the generated APKINDEX.tar.gz contains the
// expected plain-text "K:V" records, in the same format apk's own tooling
// generates.
func TestBuildAPKIndex(t *testing.T) {
	artifacts := []database.ArtifactMetadata{
		{ArtifactName: "curl", Version: "8.5.0-r0", Namespace: "x86_64", Size: 1234, Digest: "sha256:abcdef"},
	}

	data, err := buildAPKIndex(artifacts)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	gr, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	require.NoError(t, err)
	assert.Equal(t, "APKINDEX", hdr.Name)

	content, err := io.ReadAll(tr)
	require.NoError(t, err)
	body := string(content)
	assert.Contains(t, body, "P:curl\n")
	assert.Contains(t, body, "V:8.5.0-r0\n")
	assert.Contains(t, body, "A:x86_64\n")
	assert.Contains(t, body, "S:1234\n")
	assert.Contains(t, body, "C:abcdef\n")
}

func TestHandleIndexCacheHit(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	arch := uniqueID("x86_64")
	cacheKey := fmt.Sprintf("alpine:index:%s", arch)
	fakeIndex := []byte("fake-gzip-index-bytes")
	require.NoError(t, c.Set(cacheKey, fakeIndex))

	req := httptest.NewRequest(http.MethodGet, "/"+arch+"/APKINDEX.tar.gz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", arch)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleIndex(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/gzip", rec.Header().Get("Content-Type"))
	assert.Equal(t, fakeIndex, rec.Body.Bytes())
}

// TestHandleIndexBuildsFromDatabase covers a cache miss: the index is built
// fresh from artifacts already recorded in the database.
func TestHandleIndexBuildsFromDatabase(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	arch := uniqueID("aarch64")
	pkgName := uniqueID("curl")
	artifact := &database.ArtifactMetadata{
		RegistryID:   "alpine",
		ArtifactType: "alpine",
		Namespace:    arch,
		ArtifactName: pkgName,
		Version:      "8.5.0-r0",
		Tags:         []string{"8.5.0-r0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("alpine", arch, pkgName, "8.5.0-r0") })

	req := httptest.NewRequest(http.MethodGet, "/"+arch+"/APKINDEX.tar.gz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", arch)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleIndex(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	gr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	tr := tar.NewReader(gr)
	_, err = tr.Next()
	require.NoError(t, err)
	content, err := io.ReadAll(tr)
	require.NoError(t, err)
	assert.Contains(t, string(content), "P:"+pkgName)
}

func TestHandlePackageMissingAPKSuffix(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/x86_64/curl-8.5.0-r0.tar.gz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", "x86_64")
	rctx.URLParams.Add("fileName", "curl-8.5.0-r0.tar.gz")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlePackageUnparsableFileName(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/x86_64/noversion.apk", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", "x86_64")
	rctx.URLParams.Add("fileName", "noversion.apk")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandlePackageServedFromStorageStream covers the fast path where the
// .apk file is already cached in storage via the new streaming save.
func TestHandlePackageServedFromStorageStream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	arch := "x86_64"
	pkgName := uniqueID("curl")
	version := "8.5.0-r0"
	fileName := fmt.Sprintf("%s-%s.apk", pkgName, version)
	data := []byte("fake apk stream content")

	_, err := p.storage.SaveArtifactStream("alpine", arch, pkgName, version, bytes.NewReader(data))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/"+arch+"/"+fileName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", arch)
	rctx.URLParams.Add("fileName", fileName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/vnd.alpine.apk", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), fileName)
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandlePackageOldBufferedArtifactStillServable is the critical
// backward-compatibility test: an artifact written by the OLD, pre-refactor
// buffered storage.SaveArtifact call (simulating a package already cached on
// disk before this streaming refactor ships) must still be served correctly
// by the handler now that it internally calls GetArtifactStream.
func TestHandlePackageOldBufferedArtifactStillServable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	arch := "x86_64"
	pkgName := uniqueID("openssl")
	version := "3.1.0-r2"
	fileName := fmt.Sprintf("%s-%s.apk", pkgName, version)
	data := []byte("legacy pre-refactor buffered apk bytes")

	// Simulate an artifact that was cached on disk by the old code path.
	_, err := p.storage.SaveArtifact("alpine", arch, pkgName, version, data)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/"+arch+"/"+fileName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", arch)
	rctx.URLParams.Add("fileName", fileName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/vnd.alpine.apk", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), fileName)
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandlePackageNoUpstreamConfigured covers the not-in-storage path: with
// no target registry resolvable from context in this direct handler call,
// fetchStreamFromUpstream fails closed and the real response is 502.
func TestHandlePackageNoUpstreamConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("missing")
	fileName := fmt.Sprintf("%s-1.0.0-r0.apk", pkgName)
	req := httptest.NewRequest(http.MethodGet, "/x86_64/"+fileName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", "x86_64")
	rctx.URLParams.Add("fileName", fileName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestFetchStreamFromUpstreamNoProxyConfigured covers fetchStreamFromUpstream's
// closed-fail behavior for a nil or non-proxying registry.
func TestFetchStreamFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	resp, err := p.fetchStreamFromUpstream(nil, "x86_64", "curl-8.5.0-r0.apk")
	assert.Error(t, err)
	assert.Nil(t, resp)

	reg := &database.RegistryConfig{ID: "alpine", Proxy: false}
	resp, err = p.fetchStreamFromUpstream(reg, "x86_64", "curl-8.5.0-r0.apk")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestFetchStreamFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte("upstream apk bytes")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/x86_64/curl-8.5.0-r0.apk", r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "alpine", URL: upstream.URL, Proxy: true}
	resp, err := p.fetchStreamFromUpstream(reg, "x86_64", "curl-8.5.0-r0.apk")
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

// TestHandlePackageFetchesFromUpstreamViaRouter drives the full router (so
// RequireReadAccess resolves a real Target with a live registry) on a cache
// miss, proving the upstream fetch -> SaveArtifactStream -> serve pipeline
// works end to end.
func TestHandlePackageFetchesFromUpstreamViaRouter(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("wget")
	version := "1.21.0-r0"
	fileName := fmt.Sprintf("%s-%s.apk", pkgName, version)
	body := []byte("real upstream apk content for wget")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/x86_64/"+fileName, r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	host := uniqueID("alpine-upstream-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, upstream.URL, true)

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, "/x86_64/"+fileName, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, rec.Body.Bytes())
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "x86_64", pkgName, version) })

	// A second request should now be servable straight from storage.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/x86_64/"+fileName, nil)
	req2.Host = host
	router.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, body, rec2.Body.Bytes())
}

func TestCacheGetAndSet(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	key := uniqueID("test-key")
	data := []byte("hello alpine")
	p.cacheSet(key, data)

	retrieved, err := p.cacheGet(key)
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)
}

func TestCacheGetMissReturnsErrCacheMiss(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	retrieved, err := p.cacheGet(uniqueID("never-set-key"))
	require.ErrorIs(t, err, cache.ErrCacheMiss)
	assert.Nil(t, retrieved)
}

// TestAlpineProxyRoutesPrivateRequiresAuth verifies the RequireReadAccess
// middleware mounted by NewAlpineProxy enforces access control end to end.
func TestAlpineProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("private-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("private-reg"),
		Name:     "private-reg",
		Type:     "alpine",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, "/x86_64/APKINDEX.tar.gz", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAlpineProxyRoutesPublicIndex(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("public-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("public-reg"),
		Name:     "public-reg",
		Type:     "alpine",
		Enabled:  true,
		Priority: 100,
		Private:  false,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, "/x86_64/APKINDEX.tar.gz", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
