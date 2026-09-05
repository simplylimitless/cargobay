package conan

import (
	"bytes"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *ConanProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &ConanProxy{db: db, storage: adapter, cache: c}
}

func newTestRouter(p *ConanProxy, registries []database.RegistryConfig) http.Handler {
	return NewConanProxy(p.db, p.storage, p.cache, rbac.New(p.db), registries)
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// seedRegistryWithHost saves a registry bound to a virtual host, so
// resolveTarget/ResolveRegistry picks it deterministically instead of
// falling back to whatever registry (if any) other concurrently-run tests
// left as the DB-wide default for artifactType "conan".
func seedRegistryWithHost(t *testing.T, db *database.Database, host string, proxyURL string, proxyEnabled bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("conan-host-reg"),
		Name:     "conan-host-reg",
		URL:      proxyURL,
		Type:     "conan",
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

func TestNewConanProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	registries := []database.RegistryConfig{{ID: "conan", Name: "Conan", Type: "conan", Proxy: true, Enabled: true}}

	router := NewConanProxy(db, nil, c, rbac.New(db), registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

func TestRecipeNamespace(t *testing.T) {
	assert.Equal(t, "myuser/stable", recipeNamespace("myuser", "stable"))
}

func TestFileVersionKey(t *testing.T) {
	assert.Equal(t, "1.0.0::conanfile.py", fileVersionKey("1.0.0", "conanfile.py"))
}

func TestPackageVersionKey(t *testing.T) {
	assert.Equal(t, "1.0.0::abc123::conaninfo.txt", packageVersionKey("1.0.0", "abc123", "conaninfo.txt"))
}

func TestBaseURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "cargobay.example.com"
	assert.Equal(t, "http://cargobay.example.com", baseURL(req))

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Host = "cargobay.example.com"
	req2.Header.Set("X-Forwarded-Proto", "https")
	assert.Equal(t, "https://cargobay.example.com", baseURL(req2))
}

// TestHandleRecipeDownloadURLsNothingCachedAdvertisesStandardSet covers the
// case where none of the recipe files are yet in storage: the handler still
// advertises the standard file set, so the client requests them and
// triggers upstream fetch-and-cache.
func TestHandleRecipeDownloadURLsNothingCachedAdvertisesStandardSet(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("zlib")
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/1.2.11/user/stable/download_urls", name), nil)
	req.Host = "cargobay.example.com"
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", "1.2.11")
	rctx.URLParams.Add("user", "user")
	rctx.URLParams.Add("channel", "stable")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleRecipeDownloadURLs(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var urls map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&urls))
	for _, f := range recipeFiles {
		assert.Contains(t, urls, f)
	}
}

// TestHandleRecipeDownloadURLsCachedFilesListed covers the case where some
// recipe files are already cached in storage: only those are advertised.
func TestHandleRecipeDownloadURLsCachedFilesListed(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("openssl")
	version := "3.1.0"
	namespace := recipeNamespace("user", "stable")
	key := fileVersionKey(version, "conanfile.py")
	_, err := p.storage.SaveArtifactStream("conan", namespace, name, key, bytes.NewReader([]byte("recipe content")))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/%s/user/stable/download_urls", name, version), nil)
	req.Host = "cargobay.example.com"
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", version)
	rctx.URLParams.Add("user", "user")
	rctx.URLParams.Add("channel", "stable")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleRecipeDownloadURLs(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var urls map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&urls))
	assert.Contains(t, urls, "conanfile.py")
	assert.Contains(t, urls["conanfile.py"], fmt.Sprintf("/v1/conans/%s/%s/user/stable/files/conanfile.py", name, version))
}

func TestHandlePackageDownloadURLsNothingCachedAdvertisesStandardSet(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("zlib")
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/1.2.11/user/stable/packages/abc123/download_urls", name), nil)
	req.Host = "cargobay.example.com"
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", "1.2.11")
	rctx.URLParams.Add("user", "user")
	rctx.URLParams.Add("channel", "stable")
	rctx.URLParams.Add("packageID", "abc123")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackageDownloadURLs(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var urls map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&urls))
	for _, f := range packageFiles {
		assert.Contains(t, urls, f)
	}
}

// TestHandleRecipeFileServedFromStorageStream covers the fast path where a
// recipe file is already cached in storage via the new streaming save.
func TestHandleRecipeFileServedFromStorageStream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("fmt")
	version := "9.0.0"
	namespace := recipeNamespace("user", "stable")
	key := fileVersionKey(version, "conanmanifest.txt")
	data := []byte("fake manifest stream content")
	_, err := p.storage.SaveArtifactStream("conan", namespace, name, key, bytes.NewReader(data))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/%s/user/stable/files/conanmanifest.txt", name, version), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", version)
	rctx.URLParams.Add("user", "user")
	rctx.URLParams.Add("channel", "stable")
	rctx.URLParams.Add("fileName", "conanmanifest.txt")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleRecipeFile(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "conanmanifest.txt")
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandleRecipeFileOldBufferedArtifactStillServable is the critical
// backward-compatibility test: a recipe file written by the OLD,
// pre-refactor buffered storage.SaveArtifact call (simulating a recipe
// already cached on disk before this streaming refactor ships) must still
// be served correctly now that the handler calls GetArtifactStream.
func TestHandleRecipeFileOldBufferedArtifactStillServable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("boost")
	version := "1.82.0"
	namespace := recipeNamespace("user", "stable")
	key := fileVersionKey(version, "conan_export.tgz")
	data := []byte("legacy pre-refactor buffered recipe export bytes")

	_, err := p.storage.SaveArtifact("conan", namespace, name, key, data)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/%s/user/stable/files/conan_export.tgz", name, version), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", version)
	rctx.URLParams.Add("user", "user")
	rctx.URLParams.Add("channel", "stable")
	rctx.URLParams.Add("fileName", "conan_export.tgz")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleRecipeFile(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandleRecipeFileNoUpstreamConfigured covers the not-in-storage path
// with no target registry resolvable from context: the real response is
// 502.
func TestHandleRecipeFileNoUpstreamConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("missing-recipe")
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/1.0.0/user/stable/files/conanfile.py", name), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", "1.0.0")
	rctx.URLParams.Add("user", "user")
	rctx.URLParams.Add("channel", "stable")
	rctx.URLParams.Add("fileName", "conanfile.py")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleRecipeFile(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandlePackageFileServedFromStorageStream covers the fast path where a
// binary package file is already cached in storage via the new streaming
// save.
func TestHandlePackageFileServedFromStorageStream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("fmt")
	version := "9.0.0"
	packageID := "abc123"
	namespace := recipeNamespace("user", "stable")
	key := packageVersionKey(version, packageID, "conan_package.tgz")
	data := []byte("fake package tgz stream content")
	// handlePackageFile's storage lookups use t.Label, which falls back to
	// the literal artifactType "conan" outside RequireReadAccess middleware
	// (unlike handlePackageDownloadURLs, which hardcodes "conan-pkg" for its
	// own advertised-file-presence check).
	_, err := p.storage.SaveArtifactStream("conan", namespace, name, key, bytes.NewReader(data))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/%s/user/stable/packages/%s/files/conan_package.tgz", name, version, packageID), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", version)
	rctx.URLParams.Add("user", "user")
	rctx.URLParams.Add("channel", "stable")
	rctx.URLParams.Add("packageID", packageID)
	rctx.URLParams.Add("fileName", "conan_package.tgz")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackageFile(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "conan_package.tgz")
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandlePackageFileOldBufferedArtifactStillServable is the critical
// backward-compatibility test for binary package files: a file written by
// the OLD, pre-refactor buffered storage.SaveArtifact call must still be
// served correctly now that the handler calls GetArtifactStream.
func TestHandlePackageFileOldBufferedArtifactStillServable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("boost")
	version := "1.82.0"
	packageID := "def456"
	namespace := recipeNamespace("user", "stable")
	key := packageVersionKey(version, packageID, "conan_package.tgz")
	data := []byte("legacy pre-refactor buffered package tgz bytes")

	_, err := p.storage.SaveArtifact("conan", namespace, name, key, data)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/%s/user/stable/packages/%s/files/conan_package.tgz", name, version, packageID), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", version)
	rctx.URLParams.Add("user", "user")
	rctx.URLParams.Add("channel", "stable")
	rctx.URLParams.Add("packageID", packageID)
	rctx.URLParams.Add("fileName", "conan_package.tgz")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackageFile(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, data, rec.Body.Bytes())
}

func TestHandlePackageFileNoUpstreamConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("missing-pkg")
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/1.0.0/user/stable/packages/abc/files/conaninfo.txt", name), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", "1.0.0")
	rctx.URLParams.Add("user", "user")
	rctx.URLParams.Add("channel", "stable")
	rctx.URLParams.Add("packageID", "abc")
	rctx.URLParams.Add("fileName", "conaninfo.txt")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackageFile(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestServeBlobStreamOnlyCountsLargeBlobs verifies serveBlobStream's
// download-counter behavior: only files in largeBlobs trigger the count
// callback (matching how other proxies only count the real package blob,
// not small manifest/info files).
func TestServeBlobStreamOnlyCountsLargeBlobs(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	var counted bool
	countFn := func() error { counted = true; return nil }

	rec := httptest.NewRecorder()
	p.serveBlobStream(rec, "name", "1.0.0", "conaninfo.txt", bytes.NewReader([]byte("info")), countFn)
	assert.False(t, counted, "conaninfo.txt is not a large blob")

	rec2 := httptest.NewRecorder()
	p.serveBlobStream(rec2, "name", "1.0.0", "conan_package.tgz", bytes.NewReader([]byte("pkg")), countFn)
	assert.True(t, counted, "conan_package.tgz is a large blob")
}

func TestFetchStreamFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	resp, err := p.fetchStreamFromUpstream(nil, "v1/conans/name/1.0.0/user/stable/files/conanfile.py")
	assert.Error(t, err)
	assert.Nil(t, resp)

	reg := &database.RegistryConfig{ID: "conan", Proxy: false}
	resp, err = p.fetchStreamFromUpstream(reg, "v1/conans/name/1.0.0/user/stable/files/conanfile.py")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestFetchStreamFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte("upstream recipe file bytes")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/conans/name/1.0.0/user/stable/files/conanfile.py", r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "conan", URL: upstream.URL, Proxy: true}
	resp, err := p.fetchStreamFromUpstream(reg, "v1/conans/name/1.0.0/user/stable/files/conanfile.py")
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

// TestHandleRecipeFileFetchesFromUpstreamViaRouter drives the full router
// (so RequireReadAccess resolves a real Target with a live registry) on a
// cache miss, proving the upstream fetch -> SaveArtifactStream -> serve
// pipeline works end to end.
func TestHandleRecipeFileFetchesFromUpstreamViaRouter(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("catch2")
	version := "3.4.0"
	body := []byte("real upstream conanfile.py content")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, fmt.Sprintf("/v1/conans/%s/%s/user/stable/files/conanfile.py", name, version), r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	host := uniqueID("conan-upstream-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, upstream.URL, true)

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/%s/user/stable/files/conanfile.py", name, version), nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, rec.Body.Bytes())
	namespace := recipeNamespace("user", "stable")
	key := fileVersionKey(version, "conanfile.py")
	t.Cleanup(func() { p.storage.DeleteArtifact(reg.ID, namespace, name, key) })

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/conans/%s/%s/user/stable/files/conanfile.py", name, version), nil)
	req2.Host = host
	router.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, body, rec2.Body.Bytes())
}

func TestConanProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("private-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("private-reg"),
		Name:     "private-reg",
		Type:     "conan",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/conans/name/1.0.0/user/stable/download_urls", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestConanProxyRoutesPublicDownloadURLs(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("public-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("public-reg"),
		Name:     "public-reg",
		Type:     "conan",
		Enabled:  true,
		Priority: 100,
		Private:  false,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/conans/name/1.0.0/user/stable/download_urls", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
