package rpm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache, artifactType string) *RPMProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &RPMProxy{db: db, storage: adapter, cache: c, artifactType: artifactType}
}

func seedRegistry(t *testing.T, db *database.Database, id string, private, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       id,
		Name:     id,
		URL:      "https://upstream.example.com",
		Type:     "rpm",
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
// suites left as the DB-wide default for artifactType "rpm".
func seedRegistryWithHost(t *testing.T, db *database.Database, host, upstreamURL string, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("rpm-host-reg"),
		Name:     "rpm-host-reg",
		URL:      upstreamURL,
		Type:     "rpm",
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

func gunzip(t *testing.T, data []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer r.Close()
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return out
}

func TestNewRPMProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	registries := []database.RegistryConfig{{ID: "rpm", Name: "RPM Registry", Type: "rpm", Proxy: true, Enabled: true}}

	router := NewRPMProxy(db, nil, c, rbac.New(db), registries, "rpm")
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

func TestHandleRepomd(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, "rpm")

	req := httptest.NewRequest(http.MethodGet, "/x86_64/repodata/repomd.xml", nil)
	rec := httptest.NewRecorder()

	p.handleRepomd(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/xml", rec.Header().Get("Content-Type"))

	var doc repomdXML
	require.NoError(t, xml.Unmarshal(rec.Body.Bytes(), &doc))
	require.Len(t, doc.Data, 1)
	assert.Equal(t, "primary", doc.Data[0].Type)
	assert.Equal(t, "repodata/primary.xml.gz", doc.Data[0].Location.Href)
}

func TestHandlePrimaryEmpty(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, "rpm")

	req := httptest.NewRequest(http.MethodGet, "/x86_64/repodata/primary.xml.gz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", uniqueID("nonexistent-arch"))
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePrimary(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/gzip", rec.Header().Get("Content-Type"))

	var doc primaryXML
	require.NoError(t, xml.Unmarshal(gunzip(t, rec.Body.Bytes()), &doc))
	assert.Empty(t, doc.Packages)
}

// TestHandlePrimaryWithArtifacts covers handlePrimary listing packages
// recorded in the database for a given arch. Called directly (no context
// target set), TargetFromContext falls back to Target{Label: "rpm"}, so the
// artifact must be seeded under registry ID "rpm".
func TestHandlePrimaryWithArtifacts(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, "rpm")

	arch := uniqueID("x86_64")
	pkgName := uniqueID("nginx")
	artifact := &database.ArtifactMetadata{
		RegistryID:      "rpm",
		ArtifactType:    "rpm",
		Namespace:       arch,
		ArtifactName:    pkgName,
		Version:         "1.24.0",
		Size:            2048,
		DigestAlgorithm: "sha256",
		Tags:            []string{"1.24.0"},
		Metadata:        map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("rpm", arch, pkgName, "1.24.0") })

	req := httptest.NewRequest(http.MethodGet, "/"+arch+"/repodata/primary.xml.gz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", arch)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePrimary(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var doc primaryXML
	require.NoError(t, xml.Unmarshal(gunzip(t, rec.Body.Bytes()), &doc))
	require.Len(t, doc.Packages, 1)
	assert.Equal(t, pkgName, doc.Packages[0].Name)
	assert.Equal(t, "1.24.0", doc.Packages[0].Version.Ver)
	assert.Equal(t, arch, doc.Packages[0].Arch)
	assert.Equal(t, int64(2048), doc.Packages[0].Size.Package)
}

func TestSplitRPMFileNameValid(t *testing.T) {
	name, version, ok := splitRPMFileName("nginx-1.24.0.x86_64.rpm", "x86_64")
	require.True(t, ok)
	assert.Equal(t, "nginx", name)
	assert.Equal(t, "1.24.0", version)
}

func TestSplitRPMFileNameNoarch(t *testing.T) {
	name, version, ok := splitRPMFileName("filesystem-3.16.noarch.rpm", "noarch")
	require.True(t, ok)
	assert.Equal(t, "filesystem", name)
	assert.Equal(t, "3.16", version)
}

func TestSplitRPMFileNameWrongArchSuffix(t *testing.T) {
	_, _, ok := splitRPMFileName("nginx-1.24.0.x86_64.rpm", "aarch64")
	assert.False(t, ok)
}

func TestSplitRPMFileNameNoHyphen(t *testing.T) {
	_, _, ok := splitRPMFileName("nginx.x86_64.rpm", "x86_64")
	assert.False(t, ok)
}

func TestSplitRPMFileNameNotRPMExtension(t *testing.T) {
	_, _, ok := splitRPMFileName("nginx-1.24.0.x86_64.tar.gz", "x86_64")
	assert.False(t, ok)
}

// TestHandlePackageFromOldBufferedStorage proves an RPM package already
// saved to disk via the old buffered storage.SaveArtifact call (as any
// package cached before this streaming refactor would have been) is still
// served correctly now that handlePackage reads it back via
// GetArtifactStream.
func TestHandlePackageFromOldBufferedStorage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, "rpm")

	arch := uniqueID("x86_64")
	pkgName := uniqueID("nginx")
	version := "1.24.0"
	fileName := fmt.Sprintf("%s-%s.%s.rpm", pkgName, version, arch)
	data := []byte("fake rpm package content")
	_, err := p.storage.SaveArtifact("rpm", arch, pkgName, version, data)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/"+arch+"/"+fileName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", arch)
	rctx.URLParams.Add("fileName", fileName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/x-rpm", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), fileName)
	assert.Equal(t, data, rec.Body.Bytes())
}

func TestHandlePackageInvalidFileName(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, "rpm")

	req := httptest.NewRequest(http.MethodGet, "/x86_64/not-an-rpm-file.txt", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", "x86_64")
	rctx.URLParams.Add("fileName", "not-an-rpm-file.txt")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlePackageNotFoundNoUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, "rpm")

	arch := "x86_64"
	fileName := fmt.Sprintf("%s-1.0.0.%s.rpm", uniqueID("missing-pkg"), arch)
	req := httptest.NewRequest(http.MethodGet, "/"+arch+"/"+fileName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("arch", arch)
	rctx.URLParams.Add("fileName", fileName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandlePackageCacheMissFetchesUpstream drives the router end to end
// (so the Host-bound registry resolves via RequireReadAccess), fetches from
// a fake upstream server on a genuine cache miss, and verifies the package
// is both served and durably cached (a second request succeeds even after
// the fake upstream is shut down).
func TestHandlePackageCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	arch := uniqueID("x86_64")
	pkgName := uniqueID("nginx")
	version := "1.24.0"
	fileName := fmt.Sprintf("%s-%s.%s.rpm", pkgName, version, arch)
	pkgData := []byte("upstream rpm bytes")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/"+arch+"/"+fileName, r.URL.Path)
		w.Write(pkgData)
	}))
	defer upstream.Close()

	host := uniqueID("rpm-pkg-host") + ".test"
	seedRegistryWithHost(t, db, host, upstream.URL, true)

	p := newTestProxy(t, db, c, "rpm")
	router := NewRPMProxy(p.db, p.storage, p.cache, rbac.New(db), nil, "rpm")
	server := httptest.NewServer(router)
	defer server.Close()

	get := func() *http.Response {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/"+arch+"/"+fileName, nil)
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
	assert.Equal(t, pkgData, body)

	upstream.Close()
	resp2 := get()
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	assert.Equal(t, pkgData, body2)
}

func TestFetchStreamFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, "rpm")

	resp, err := p.fetchStreamFromUpstream(&database.RegistryConfig{Proxy: false}, "x86_64", "nginx-1.0.0.x86_64.rpm")
	assert.Error(t, err)
	assert.Nil(t, resp)

	resp, err = p.fetchStreamFromUpstream(nil, "x86_64", "nginx-1.0.0.x86_64.rpm")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestFetchStreamFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, "rpm")

	body := []byte("some rpm bytes")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/x86_64/nginx-1.0.0.x86_64.rpm", r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{URL: upstream.URL, Proxy: true}
	resp, err := p.fetchStreamFromUpstream(reg, "x86_64", "nginx-1.0.0.x86_64.rpm")
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestRPMProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("private-rpm-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("private-rpm-reg"),
		Name:     "private-rpm-reg",
		Type:     "rpm",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := NewRPMProxy(db, nil, c, rbac.New(db), nil, "rpm")

	req := httptest.NewRequest(http.MethodGet, "/x86_64/repodata/repomd.xml", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRPMProxyRoutesPublicRepomd(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("public-rpm-host") + ".test"
	seedRegistryWithHost(t, db, host, "https://upstream.example.com", false)

	router := NewRPMProxy(db, nil, c, rbac.New(db), nil, "rpm")

	req := httptest.NewRequest(http.MethodGet, "/x86_64/repodata/repomd.xml", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestNewRPMProxyYumArtifactType verifies the same implementation backs the
// "yum" registry type, parameterized purely by artifactType.
func TestNewRPMProxyYumArtifactType(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c, "yum")
	assert.Equal(t, "yum", p.artifactType)

	router := NewRPMProxy(db, nil, c, rbac.New(db), nil, "yum")
	assert.NotNil(t, router)
}

// TestSeedRegistryHelper exercises the seedRegistry helper directly so it
// isn't dead code within this file, mirroring how other proxy test files
// use it for RBAC/listing-oriented tests.
func TestSeedRegistryHelper(t *testing.T) {
	db := connectTestDB(t)
	reg := seedRegistry(t, db, uniqueID("rpm-seed-reg"), false, true)
	assert.True(t, reg.Proxy)
	assert.Equal(t, "rpm", reg.Type)
}
