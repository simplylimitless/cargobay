package cargo

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *CargoProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &CargoProxy{db: db, storage: adapter, cache: c}
}

func newTestRouter(p *CargoProxy, registries []database.RegistryConfig) http.Handler {
	return NewCargoProxy(p.db, p.storage, p.cache, rbac.New(p.db), registries)
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// seedRegistryWithHost saves a registry bound to a virtual host, so
// resolveTarget/ResolveRegistry picks it deterministically instead of
// falling back to whatever registry (if any) other concurrently-run tests
// left as the DB-wide default for artifactType "cargo".
func seedRegistryWithHost(t *testing.T, db *database.Database, host string, proxyURL string, proxyEnabled bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("cargo-host-reg"),
		Name:     "cargo-host-reg",
		URL:      proxyURL,
		Type:     "cargo",
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

// seedRegistry saves a plain (host-unbound) registry, used for tests that
// don't need Host-based resolution.
func seedRegistry(t *testing.T, db *database.Database, id string, private, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       id,
		Name:     id,
		URL:      "https://upstream.example.com",
		Type:     "cargo",
		Proxy:    proxy,
		Enabled:  true,
		Priority: 10,
		Private:  private,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(id) })
	return reg
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

// buildPublishBody constructs a valid cargo-publish wire body: a
// length-prefixed JSON metadata object followed by a length-prefixed .crate
// tarball, per
// https://doc.rust-lang.org/cargo/reference/registry-web-api.html#publish.
func buildPublishBody(name, version string, crate []byte) []byte {
	meta := fmt.Sprintf(`{"name":%q,"vers":%q,"deps":[]}`, name, version)
	var buf bytes.Buffer
	var lenBuf [4]byte

	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(meta)))
	buf.Write(lenBuf[:])
	buf.WriteString(meta)

	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(crate)))
	buf.Write(lenBuf[:])
	buf.Write(crate)

	return buf.Bytes()
}

func TestNewCargoProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	registries := []database.RegistryConfig{{ID: "cargo", Name: "Cargo", Type: "cargo", Proxy: true, Enabled: true}}

	router := NewCargoProxy(db, nil, c, rbac.New(db), registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

func TestHandleConfig(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/config.json", nil)
	req.Host = "cargobay.example.com"
	rec := httptest.NewRecorder()

	p.handleConfig(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "http://cargobay.example.com/cargo/api/v1/crates", body["dl"])
	assert.Equal(t, "http://cargobay.example.com/cargo", body["api"])
}

func TestHandleConfigHTTPS(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/config.json", nil)
	req.Host = "cargobay.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()

	p.handleConfig(rec, req)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "https://cargobay.example.com/cargo/api/v1/crates", body["dl"])
}

func TestSparseIndexPath(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"a", "1/a"},
		{"ab", "2/ab"},
		{"abc", "3/a/abc"},
		{"abcd", "ab/cd/abcd"},
		{"serde", "se/rd/serde"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, sparseIndexPath(tt.name), "name=%s", tt.name)
	}
}

func TestSchemeFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Equal(t, "http", schemeFor(req))

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Forwarded-Proto", "https")
	assert.Equal(t, "https", schemeFor(req2))

	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.TLS = &tls.ConnectionState{}
	assert.Equal(t, "https", schemeFor(req3))
}

func TestCargoIndexLineUsesStoredIndexLineVerbatim(t *testing.T) {
	a := database.ArtifactMetadata{
		Version:  "1.0.0",
		Metadata: map[string]interface{}{"indexLine": `{"name":"serde","vers":"1.0.0","raw":true}`},
	}
	assert.Equal(t, `{"name":"serde","vers":"1.0.0","raw":true}`, cargoIndexLine("serde", a))
}

func TestCargoIndexLineBuildsFromMetadata(t *testing.T) {
	a := database.ArtifactMetadata{
		Version: "2.0.0",
		Digest:  "sha256:deadbeef",
	}
	line := cargoIndexLine("serde", a)
	var entry map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(line), &entry))
	assert.Equal(t, "serde", entry["name"])
	assert.Equal(t, "2.0.0", entry["vers"])
	assert.Equal(t, "deadbeef", entry["cksum"])
	assert.Equal(t, false, entry["yanked"])
}

func TestHandleIndexCacheHit(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("serde")
	cacheKey := fmt.Sprintf("cargo:index:%s", name)
	fakeIndex := []byte(`{"name":"` + name + `","vers":"1.0.0"}` + "\n")
	require.NoError(t, c.Set(cacheKey, fakeIndex))

	req := httptest.NewRequest(http.MethodGet, "/1/"+name, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleIndex(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	assert.Equal(t, fakeIndex, rec.Body.Bytes())
}

// TestHandleIndexBuildsFromDatabase covers a cache miss with versions
// already recorded in the database.
func TestHandleIndexBuildsFromDatabase(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("tokio")
	artifact := &database.ArtifactMetadata{
		RegistryID:   "cargo",
		ArtifactType: "cargo",
		ArtifactName: name,
		Version:      "1.0.0",
		Digest:       "sha256:abc123",
		Tags:         []string{"1.0.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("cargo", "", name, "1.0.0") })

	req := httptest.NewRequest(http.MethodGet, "/1/"+name, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleIndex(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), name)
	assert.Contains(t, rec.Body.String(), "1.0.0")
}

// TestHandleIndexNoVersionsNoUpstreamNotFound covers the empty-index path
// with no upstream registry resolvable from context: fetchIndexFromUpstream
// fails closed and the handler reports 404.
func TestHandleIndexNoVersionsNoUpstreamNotFound(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("never-published")
	req := httptest.NewRequest(http.MethodGet, "/1/"+name, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleIndex(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestFetchIndexFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	data, err := p.fetchIndexFromUpstream(nil, "cargo", "serde")
	assert.Error(t, err)
	assert.Nil(t, data)

	reg := &database.RegistryConfig{ID: "cargo", Proxy: false}
	data, err = p.fetchIndexFromUpstream(reg, "cargo", "serde")
	assert.Error(t, err)
	assert.Nil(t, data)
}

func TestFetchIndexFromUpstreamSuccessSavesArtifacts(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("anyhow")
	body := []byte(fmt.Sprintf(`{"name":"%s","vers":"1.0.0","cksum":"abc123","deps":[],"features":{},"yanked":false}`+"\n", name))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/"+sparseIndexPath(name), r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "cargo-reg", URL: upstream.URL, Proxy: true}
	regLabel := uniqueID("cargo-reg-label")
	data, err := p.fetchIndexFromUpstream(reg, regLabel, name)
	require.NoError(t, err)
	assert.Equal(t, body, data)
	t.Cleanup(func() { db.DeleteArtifact(regLabel, "", name, "1.0.0") })

	artifacts, err := db.ListArtifacts(regLabel, database.ListOptions{ArtifactType: "cargo"})
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, name, artifacts[0].ArtifactName)
	assert.Equal(t, "1.0.0", artifacts[0].Version)
	assert.Equal(t, "sha256:abc123", artifacts[0].Digest)
}

// TestHandleDownloadServedFromStorageStream covers the fast path where a
// .crate tarball is already cached in storage via the new streaming save.
func TestHandleDownloadServedFromStorageStream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("serde")
	version := "1.0.0"
	data := []byte("fake crate stream content")
	_, err := p.storage.SaveArtifactStream("cargo", "", name, version, bytes.NewReader(data))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/crates/"+name+"/"+version+"/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", version)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleDownload(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/gzip", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), fmt.Sprintf("%s-%s.crate", name, version))
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandleDownloadOldBufferedArtifactStillServable is the critical
// backward-compatibility test: a crate tarball written by the OLD,
// pre-refactor buffered storage.SaveArtifact call (simulating a crate
// already cached on disk before this streaming refactor ships) must still
// be served correctly now that the handler calls GetArtifactStream.
func TestHandleDownloadOldBufferedArtifactStillServable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("tokio")
	version := "1.2.3"
	data := []byte("legacy pre-refactor buffered crate bytes")

	_, err := p.storage.SaveArtifact("cargo", "", name, version, data)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/crates/"+name+"/"+version+"/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", version)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleDownload(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/gzip", rec.Header().Get("Content-Type"))
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandleDownloadNoUpstreamConfigured covers the not-in-storage path with
// no target registry resolvable from context: the real response is 502.
func TestHandleDownloadNoUpstreamConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("missing-crate")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/crates/"+name+"/1.0.0/download", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("version", "1.0.0")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleDownload(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestFetchCrateStreamFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	resp, err := p.fetchCrateStreamFromUpstream(nil, "serde", "1.0.0")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestFetchCrateStreamFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte("upstream crate bytes")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/crates/serde/1.0.0/download", r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "cargo", URL: upstream.URL, Proxy: true}
	resp, err := p.fetchCrateStreamFromUpstream(reg, "serde", "1.0.0")
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

// TestHandleDownloadFetchesFromUpstreamViaRouter drives the full router (so
// RequireReadAccess resolves a real Target with a live registry) on a cache
// miss, proving the upstream fetch -> SaveArtifactStream -> serve pipeline
// works end to end.
func TestHandleDownloadFetchesFromUpstreamViaRouter(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("bytes")
	version := "1.5.0"
	body := []byte("real upstream crate content")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, fmt.Sprintf("/api/v1/crates/%s/%s/download", name, version), r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	host := uniqueID("cargo-upstream-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, upstream.URL, true)

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/crates/%s/%s/download", name, version), nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, rec.Body.Bytes())
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", name, version) })

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/crates/%s/%s/download", name, version), nil)
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
	data := []byte("hello cargo")
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

func TestCargoProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("private-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("private-reg"),
		Name:     "private-reg",
		Type:     "cargo",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, "/config.json", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCargoProxyRoutesPublicConfig(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("public-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("public-reg"),
		Name:     "public-reg",
		Type:     "cargo",
		Enabled:  true,
		Priority: 100,
		Private:  false,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, "/config.json", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandlePublishNoAuthDenied(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-noauth-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-noauth-reg"), true, false)
	reg.Host = host
	require.NoError(t, db.SaveRegistry(reg))

	router := NewCargoProxy(db, nil, c, rbac.New(db), nil)

	body := buildPublishBody(uniqueID("crate"), "1.0.0", []byte("crate bytes"))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/crates/new", bytes.NewReader(body))
	req.Host = host
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

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

	router := NewCargoProxy(db, nil, c, rbac.New(db), nil)

	body := buildPublishBody(uniqueID("crate"), "1.0.0", []byte("crate bytes"))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/crates/new", bytes.NewReader(body))
	req.Host = host
	ctx := context.WithValue(req.Context(), middleware.AuthUserKey, &middleware.User{UserID: userID, Username: userID, Scope: "read"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

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

	router := NewCargoProxy(db, nil, c, rbac.New(db), nil)

	body := buildPublishBody(uniqueID("crate"), "1.0.0", []byte("crate bytes"))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/crates/new", bytes.NewReader(body))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandlePublishSuccess(t *testing.T) {
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
	router := NewCargoProxy(db, adapter, c, rbac.New(db), nil)

	crateName := uniqueID("crate")
	version := "1.0.0"
	crateBytes := []byte("crate tarball bytes")
	body := buildPublishBody(crateName, version, crateBytes)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/crates/new", bytes.NewReader(body))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", crateName, version) })

	rc, err := adapter.GetArtifactStream(reg.ID, "", crateName, version)
	require.NoError(t, err)
	defer rc.Close()
	stored, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, crateBytes, stored)

	artifacts, err := db.ListArtifacts(reg.ID, database.ListOptions{ArtifactType: "cargo"})
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, crateName, artifacts[0].ArtifactName)
	assert.Equal(t, version, artifacts[0].Version)
}

func TestHandlePublishMalformedBody(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("publish-malformed-host") + ".test"
	reg := seedRegistry(t, db, uniqueID("publish-malformed-reg"), true, false)
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

	router := NewCargoProxy(db, nil, c, rbac.New(db), nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/crates/new", bytes.NewReader([]byte("not a valid publish body")))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

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
	router := NewCargoProxy(db, adapter, c, rbac.New(db), nil)

	crateName := uniqueID("crate")
	version := "1.0.0"
	crateBytes := []byte("group-granted crate bytes")
	body := buildPublishBody(crateName, version, crateBytes)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/crates/new", bytes.NewReader(body))
	req.Host = host
	req = authedRequest(req, userID)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", crateName, version) })

	artifacts, err := db.ListArtifacts(reg.ID, database.ListOptions{ArtifactType: "cargo"})
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, crateName, artifacts[0].ArtifactName)
}
