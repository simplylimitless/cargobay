package conda

import (
	"bytes"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *CondaProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &CondaProxy{db: db, storage: adapter, cache: c}
}

func newTestRouter(p *CondaProxy) http.Handler {
	return NewCondaProxy(p.db, p.storage, p.cache, rbac.New(p.db), nil)
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// seedRegistryWithHost saves a registry bound to a virtual host, so
// ResolveRegistry picks it deterministically instead of falling back to
// whatever registry (if any) other concurrently-run test suites left as the
// DB-wide default for artifactType "conda".
func seedRegistryWithHost(t *testing.T, db *database.Database, host, upstreamURL string, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("conda-host-reg"),
		Name:     "conda-host-reg",
		URL:      upstreamURL,
		Type:     "conda",
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

// withRouteCtx attaches rctx to r's context the way chi's router does, so
// handlers reading chi.URLParam(r, ...) work when called directly
// (bypassing the router/mux).
func withRouteCtx(r *http.Request, rctx *chi.Context) context.Context {
	return context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
}

func TestNewCondaProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	router := newTestRouter(p)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

// TestHandleRepodata verifies repodata.json is generated from cached
// artifact metadata, splitting .tar.bz2 and .conda entries into their
// respective maps and reconstructing the original filename from the encoded
// version key.
func TestHandleRepodata(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	channel := uniqueID("conda-forge")
	subdir := "linux-64"
	namespace := fmt.Sprintf("%s/%s", channel, subdir)
	pkgName := uniqueID("numpy")

	tarbz2 := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("conda:conda:%s:%s:1.23.0::0::.tar.bz2", namespace, pkgName),
		RegistryID:   "conda",
		ArtifactType: "conda",
		Namespace:    namespace,
		ArtifactName: pkgName,
		Version:      "1.23.0::0::.tar.bz2",
		Size:         1024,
		Metadata:     map[string]interface{}{},
		Tags:         []string{"1.23.0::0::.tar.bz2"},
	}
	require.NoError(t, db.SaveArtifact(tarbz2))
	t.Cleanup(func() { db.DeleteArtifact("conda", namespace, pkgName, "1.23.0::0::.tar.bz2") })

	condaExt := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("conda:conda:%s:%s:2.0.0::1::.conda", namespace, pkgName),
		RegistryID:   "conda",
		ArtifactType: "conda",
		Namespace:    namespace,
		ArtifactName: pkgName,
		Version:      "2.0.0::1::.conda",
		Size:         2048,
		Metadata:     map[string]interface{}{},
		Tags:         []string{"2.0.0::1::.conda"},
	}
	require.NoError(t, db.SaveArtifact(condaExt))
	t.Cleanup(func() { db.DeleteArtifact("conda", namespace, pkgName, "2.0.0::1::.conda") })

	req := httptest.NewRequest(http.MethodGet, "/"+channel+"/"+subdir+"/repodata.json", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", channel)
	rctx.URLParams.Add("subdir", subdir)
	req = req.WithContext(withRouteCtx(req, rctx))
	rec := httptest.NewRecorder()

	p.handleRepodata(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))

	packages, ok := body["packages"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, packages, fmt.Sprintf("%s-1.23.0-0.tar.bz2", pkgName))

	packagesConda, ok := body["packages.conda"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, packagesConda, fmt.Sprintf("%s-2.0.0-1.conda", pkgName))
}

// TestHandlePackageCacheHit covers handlePackage's fast path when the
// package has already been streamed into storage.
func TestHandlePackageCacheHit(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	channel := uniqueID("conda-forge")
	subdir := "linux-64"
	namespace := fmt.Sprintf("%s/%s", channel, subdir)
	pkgName := uniqueID("numpy")
	versionKey := "1.23.0::0::.tar.bz2"
	fileName := fmt.Sprintf("%s-1.23.0-0.tar.bz2", pkgName)
	data := []byte("fake conda package content")

	_, err := p.storage.SaveArtifactStream("conda", namespace, pkgName, versionKey, bytes.NewReader(data))
	require.NoError(t, err)

	req := newPackageRequest(channel, subdir, fileName)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), fileName)
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandlePackageOldBufferedArtifactStillServable simulates a package that
// was already cached on disk via the pre-streaming-refactor buffered
// SaveArtifact call, before this deploy. The refactored handlePackage now
// reads via GetArtifactStream, but LocalAdapter's stream/buffered paths
// share the same on-disk file layout, so an artifact saved the old way must
// still be served correctly after the upgrade.
func TestHandlePackageOldBufferedArtifactStillServable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	channel := uniqueID("conda-forge")
	subdir := "linux-64"
	namespace := fmt.Sprintf("%s/%s", channel, subdir)
	pkgName := uniqueID("scipy")
	versionKey := "1.10.0::0::.conda"
	fileName := fmt.Sprintf("%s-1.10.0-0.conda", pkgName)
	data := []byte("old-style pre-refactor buffered artifact bytes")

	// Simulate a pre-existing on-disk artifact saved by the old, buffered
	// SaveArtifact API (i.e. what was on disk before this deploy).
	_, err := p.storage.SaveArtifact("conda", namespace, pkgName, versionKey, data)
	require.NoError(t, err)

	req := newPackageRequest(channel, subdir, fileName)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), fileName)
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandlePackageCacheMissFetchesUpstream exercises the full router (so
// TargetFromContext resolves a real registry with a proxy URL) on a cache
// miss: the fake upstream server serves the package, it gets streamed into
// storage, recorded as metadata, and served back.
func TestHandlePackageCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	channel := uniqueID("conda-forge")
	subdir := "linux-64"
	pkgName := uniqueID("pandas")
	fileName := fmt.Sprintf("%s-2.0.0-0.tar.bz2", pkgName)
	data := []byte("fake upstream conda payload")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/"+channel+"/"+subdir+"/"+fileName, r.URL.Path)
		w.Write(data)
	}))
	defer upstream.Close()

	host := uniqueID("conda-miss-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, upstream.URL, true)
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, channel+"/"+subdir, pkgName, "2.0.0::0::.tar.bz2") })

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/"+channel+"/"+subdir+"/"+fileName, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, data, rec.Body.Bytes())

	stored, err := p.storage.GetArtifact(reg.ID, channel+"/"+subdir, pkgName, "2.0.0::0::.tar.bz2")
	require.NoError(t, err)
	assert.Equal(t, data, stored)
}

// TestHandlePackageBadFileName covers a filename that doesn't parse as
// "name-version-build.(tar.bz2|conda)".
func TestHandlePackageBadFileName(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := newPackageRequest("conda-forge", "linux-64", "not-a-valid-filename.zip")
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandlePackageUpstreamFailureNoProxyConfigured covers a cache miss with
// no upstream registry resolvable (direct handler call has no Target.Reg),
// so fetchStreamFromUpstream fails and the real response is 502.
func TestHandlePackageUpstreamFailureNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("never-cached")
	fileName := fmt.Sprintf("%s-1.0.0-0.tar.bz2", pkgName)
	req := newPackageRequest("conda-forge", "linux-64", fileName)
	rec := httptest.NewRecorder()

	p.handlePackage(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestFetchStreamFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	reg := &database.RegistryConfig{ID: "conda", Proxy: false}
	resp, err := p.fetchStreamFromUpstream(reg, "conda-forge/linux-64/numpy-1.0.0-0.tar.bz2")
	assert.Error(t, err)
	assert.Nil(t, resp)

	resp, err = p.fetchStreamFromUpstream(nil, "conda-forge/linux-64/numpy-1.0.0-0.tar.bz2")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestFetchStreamFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte("upstream conda bytes")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/conda-forge/linux-64/numpy-1.0.0-0.tar.bz2", r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "conda", URL: upstream.URL, Proxy: true}
	resp, err := p.fetchStreamFromUpstream(reg, "conda-forge/linux-64/numpy-1.0.0-0.tar.bz2")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestFetchStreamFromUpstreamNonOKStatus(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "conda", URL: upstream.URL, Proxy: true}
	resp, err := p.fetchStreamFromUpstream(reg, "missing.tar.bz2")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestSplitCondaFileName(t *testing.T) {
	tests := []struct {
		name        string
		fileName    string
		wantName    string
		wantVersion string
		wantOK      bool
	}{
		{"tar.bz2 basic", "numpy-1.23.0-0.tar.bz2", "numpy", "1.23.0::0::.tar.bz2", true},
		{"conda ext", "numpy-1.23.0-py310h1_0.conda", "numpy", "1.23.0::py310h1_0::.conda", true},
		{"multi-dash name", "scikit-learn-1.2.0-0.tar.bz2", "scikit-learn", "1.2.0::0::.tar.bz2", true},
		{"too few parts", "invalidfile.tar.bz2", "", "", false},
		{"unrecognized ext", "numpy-1.23.0-0.zip", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, versionKey, ok := splitCondaFileName(tt.fileName)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantName, name)
				assert.Equal(t, tt.wantVersion, versionKey)
			}
		})
	}
}

func TestSplitCondaVersionKey(t *testing.T) {
	tests := []struct {
		name        string
		versionKey  string
		wantVersion string
		wantBuild   string
		wantExt     string
	}{
		{"encoded key", "1.23.0::0::.tar.bz2", "1.23.0", "0", ".tar.bz2"},
		{"encoded conda key", "2.0.0::py310h1::.conda", "2.0.0", "py310h1", ".conda"},
		{"legacy bare version", "1.0.0", "1.0.0", "", ".tar.bz2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, build, ext := splitCondaVersionKey(tt.versionKey)
			assert.Equal(t, tt.wantVersion, version)
			assert.Equal(t, tt.wantBuild, build)
			assert.Equal(t, tt.wantExt, ext)
		})
	}
}

// TestCondaProxyRoutesPublicPackage verifies the full router (including the
// RequireReadAccess middleware) serves a cached package end to end for a
// public, Host-bound registry.
func TestCondaProxyRoutesPublicPackage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("conda-public-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, "https://upstream.example.com", false)

	channel := "conda-forge"
	subdir := "linux-64"
	namespace := fmt.Sprintf("%s/%s", channel, subdir)
	pkgName := uniqueID("requests")
	versionKey := "2.31.0::0::.tar.bz2"
	fileName := fmt.Sprintf("%s-2.31.0-0.tar.bz2", pkgName)
	data := []byte("public package bytes")

	_, err := p.storage.SaveArtifactStream(reg.ID, namespace, pkgName, versionKey, bytes.NewReader(data))
	require.NoError(t, err)

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/"+channel+"/"+subdir+"/"+fileName, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, data, rec.Body.Bytes())
}

// newPackageRequest builds a request against handlePackage's route shape
// with chi URL params attached directly (bypassing the router/mux).
func newPackageRequest(channel, subdir, fileName string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/"+channel+"/"+subdir+"/"+fileName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", channel)
	rctx.URLParams.Add("subdir", subdir)
	rctx.URLParams.Add("fileName", fileName)
	return req.WithContext(withRouteCtx(req, rctx))
}
