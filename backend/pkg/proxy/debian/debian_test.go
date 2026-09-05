package debian

import (
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *DebianProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &DebianProxy{db: db, storage: adapter, cache: c}
}

func newTestRouter(p *DebianProxy) http.Handler {
	return NewDebianProxy(p.db, p.storage, p.cache, rbac.New(p.db), nil)
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// seedRegistryWithHost saves a registry bound to a virtual host, so
// ResolveRegistry picks it deterministically instead of falling back to
// whatever registry (if any) other concurrently-run test suites left as the
// DB-wide default for artifactType "debian".
func seedRegistryWithHost(t *testing.T, db *database.Database, host, upstreamURL string, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("debian-host-reg"),
		Name:     "debian-host-reg",
		URL:      upstreamURL,
		Type:     "debian",
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

func newPoolRequest(fileName string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/pool/"+fileName, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("fileName", fileName)
	return req.WithContext(withRouteCtx(req, rctx))
}

func TestNewDebianProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	router := newTestRouter(p)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

func TestHandleRelease(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	dist := uniqueID("stable")
	req := httptest.NewRequest(http.MethodGet, "/dists/"+dist+"/Release", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("dist", dist)
	req = req.WithContext(withRouteCtx(req, rctx))
	rec := httptest.NewRecorder()

	p.handleRelease(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, "Origin: Cargobay")
	assert.Contains(t, body, "Suite: "+dist)
	assert.Contains(t, body, "Codename: "+dist)
	assert.Contains(t, body, "Components: main")
}

// TestHandlePackages verifies the plaintext Packages index is generated from
// cached artifact metadata for the given architecture.
func TestHandlePackages(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	arch := "amd64"
	pkgName := uniqueID("curl")
	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("debian:debian:%s:%s:1.0.0", arch, pkgName),
		RegistryID:      "debian",
		ArtifactType:    "debian",
		Namespace:       arch,
		ArtifactName:    pkgName,
		Version:         "1.0.0",
		Digest:          "sha256:abcdef",
		DigestAlgorithm: "sha256",
		Size:            2048,
		Metadata:        map[string]interface{}{},
		Tags:            []string{"1.0.0"},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("debian", arch, pkgName, "1.0.0") })

	req := httptest.NewRequest(http.MethodGet, "/dists/stable/main/binary-"+arch+"/Packages", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("dist", "stable")
	rctx.URLParams.Add("arch", arch)
	req = req.WithContext(withRouteCtx(req, rctx))
	rec := httptest.NewRecorder()

	p.handlePackages(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, "Package: "+pkgName)
	assert.Contains(t, body, "Version: 1.0.0")
	assert.Contains(t, body, "Architecture: "+arch)
	assert.Contains(t, body, fmt.Sprintf("Filename: pool/%s_1.0.0_%s.deb", pkgName, arch))
	assert.Contains(t, body, "Size: 2048")
	assert.Contains(t, body, "SHA256: abcdef")
}

// TestHandlePackagesGz verifies the gzip-compressed variant decompresses to
// the same content as the plaintext index.
func TestHandlePackagesGz(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	arch := "arm64"
	pkgName := uniqueID("wget")
	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("debian:debian:%s:%s:2.0.0", arch, pkgName),
		RegistryID:   "debian",
		ArtifactType: "debian",
		Namespace:    arch,
		ArtifactName: pkgName,
		Version:      "2.0.0",
		Size:         512,
		Metadata:     map[string]interface{}{},
		Tags:         []string{"2.0.0"},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("debian", arch, pkgName, "2.0.0") })

	req := httptest.NewRequest(http.MethodGet, "/dists/stable/main/binary-"+arch+"/Packages.gz", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("dist", "stable")
	rctx.URLParams.Add("arch", arch)
	req = req.WithContext(withRouteCtx(req, rctx))
	rec := httptest.NewRecorder()

	p.handlePackagesGz(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/gzip", rec.Header().Get("Content-Type"))

	gr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	decompressed, err := io.ReadAll(gr)
	require.NoError(t, err)
	assert.Contains(t, string(decompressed), "Package: "+pkgName)
	assert.Contains(t, string(decompressed), "Version: 2.0.0")
}

// TestHandlePoolCacheHit covers handlePool's fast path when the .deb has
// already been streamed into storage.
func TestHandlePoolCacheHit(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("nginx")
	version := "1.24.0"
	arch := "amd64"
	fileName := fmt.Sprintf("%s_%s_%s.deb", pkgName, version, arch)
	data := []byte("fake deb package content")

	_, err := p.storage.SaveArtifactStream("debian", arch, pkgName, version, bytes.NewReader(data))
	require.NoError(t, err)

	req := newPoolRequest(fileName)
	rec := httptest.NewRecorder()

	p.handlePool(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/vnd.debian.binary-package", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), fileName)
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandlePoolOldBufferedArtifactStillServable simulates a .deb that was
// already cached on disk via the pre-streaming-refactor buffered
// SaveArtifact call, before this deploy. handlePool now reads via
// GetArtifactStream, but LocalAdapter's stream/buffered paths share the same
// on-disk file layout, so an artifact saved the old way must still be
// servable correctly after the upgrade.
func TestHandlePoolOldBufferedArtifactStillServable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("openssl")
	version := "3.0.2"
	arch := "arm64"
	fileName := fmt.Sprintf("%s_%s_%s.deb", pkgName, version, arch)
	data := []byte("old-style pre-refactor buffered deb bytes")

	// Simulate a pre-existing on-disk artifact saved by the old, buffered
	// SaveArtifact API (i.e. what was on disk before this deploy).
	_, err := p.storage.SaveArtifact("debian", arch, pkgName, version, data)
	require.NoError(t, err)

	req := newPoolRequest(fileName)
	rec := httptest.NewRecorder()

	p.handlePool(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/vnd.debian.binary-package", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), fileName)
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandlePoolCacheMissFetchesUpstream exercises the full router (so
// TargetFromContext resolves a real registry with a proxy URL) on a cache
// miss: the fake upstream server serves the .deb, it gets streamed into
// storage, recorded as metadata, and served back.
func TestHandlePoolCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("vim")
	version := "9.0"
	arch := "amd64"
	fileName := fmt.Sprintf("%s_%s_%s.deb", pkgName, version, arch)
	data := []byte("fake upstream deb payload")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/pool/"+fileName, r.URL.Path)
		w.Write(data)
	}))
	defer upstream.Close()

	host := uniqueID("debian-miss-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, upstream.URL, true)
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, arch, pkgName, version) })

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/pool/"+fileName, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, data, rec.Body.Bytes())

	stored, err := p.storage.GetArtifact(reg.ID, arch, pkgName, version)
	require.NoError(t, err)
	assert.Equal(t, data, stored)
}

// TestHandlePoolBadFileName covers a filename that doesn't parse as
// "name_version_arch.deb".
func TestHandlePoolBadFileName(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := newPoolRequest("not-a-valid-filename.deb")
	rec := httptest.NewRecorder()

	p.handlePool(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandlePoolUpstreamFailureNoProxyConfigured covers a cache miss with no
// upstream registry resolvable (direct handler call has no Target.Reg), so
// fetchStreamFromUpstream fails and the real response is 502.
func TestHandlePoolUpstreamFailureNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	pkgName := uniqueID("never-cached")
	fileName := fmt.Sprintf("%s_1.0.0_amd64.deb", pkgName)
	req := newPoolRequest(fileName)
	rec := httptest.NewRecorder()

	p.handlePool(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestFetchStreamFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	reg := &database.RegistryConfig{ID: "debian", Proxy: false}
	resp, err := p.fetchStreamFromUpstream(reg, "nginx_1.0.0_amd64.deb")
	assert.Error(t, err)
	assert.Nil(t, resp)

	resp, err = p.fetchStreamFromUpstream(nil, "nginx_1.0.0_amd64.deb")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestFetchStreamFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte("upstream deb bytes")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/pool/nginx_1.0.0_amd64.deb", r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "debian", URL: upstream.URL, Proxy: true}
	resp, err := p.fetchStreamFromUpstream(reg, "nginx_1.0.0_amd64.deb")
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

	reg := &database.RegistryConfig{ID: "debian", URL: upstream.URL, Proxy: true}
	resp, err := p.fetchStreamFromUpstream(reg, "missing.deb")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestSplitDebFileName(t *testing.T) {
	tests := []struct {
		name        string
		fileName    string
		wantName    string
		wantVersion string
		wantArch    string
		wantOK      bool
	}{
		{"basic", "nginx_1.24.0_amd64.deb", "nginx", "1.24.0", "amd64", true},
		{"too few parts", "nginx_1.24.0.deb", "", "", "", false},
		{"too many parts", "nginx_1.24.0_amd64_extra.deb", "", "", "", false},
		// splitDebFileName only trims a ".deb" suffix if present (no-op
		// otherwise) before splitting on "_" -- it does not itself verify
		// the file actually had a .deb extension, so a 3-part name lacking
		// it still parses "successfully", with the extension folded into
		// the last (arch) segment.
		{"missing .deb extension still parses", "nginx_1.24.0_amd64.tar.gz", "nginx", "1.24.0", "amd64.tar.gz", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, version, arch, ok := splitDebFileName(tt.fileName)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantName, name)
				assert.Equal(t, tt.wantVersion, version)
				assert.Equal(t, tt.wantArch, arch)
			}
		})
	}
}

// TestDebianProxyRoutesPublicRelease verifies the full router (including the
// RequireReadAccess middleware) serves the Release file end to end for a
// public, Host-bound registry.
func TestDebianProxyRoutesPublicRelease(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("debian-public-host") + ".test"
	seedRegistryWithHost(t, db, host, "https://upstream.example.com", false)

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/dists/stable/Release", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Suite: stable")
}

// TestDebianProxyRoutesPrivateRequiresAuth verifies the RequireReadAccess
// middleware mounted by NewDebianProxy enforces access control end to end:
// an anonymous request against a private, Host-bound registry is rejected
// before it ever reaches a handler.
func TestDebianProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("debian-private-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("debian-private-reg"),
		Name:     "debian-private-reg",
		Type:     "debian",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/dists/stable/Release", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
