package gomod

import (
	"bytes"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *GoModProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &GoModProxy{db: db, storage: adapter, cache: c}
}

func newTestRouter(p *GoModProxy) http.Handler {
	return NewGoModProxy(p.db, p.storage, p.cache, rbac.New(p.db), nil)
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// seedRegistryWithHost saves a registry bound to a virtual host, so
// ResolveRegistry picks it deterministically instead of falling back to
// whatever registry (if any) other concurrently-run test suites left as the
// DB-wide default for artifactType "go".
func seedRegistryWithHost(t *testing.T, db *database.Database, host, upstreamURL string, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("gomod-host-reg"),
		Name:     "gomod-host-reg",
		URL:      upstreamURL,
		Type:     "go",
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

func TestNewGoModProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	router := newTestRouter(p)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

// TestHandleListFiltersToModule covers @v/list: only versions belonging to
// the requested module are returned, sorted, one per line.
func TestHandleListFiltersToModule(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/mod")
	other := uniqueID("github.com/example/other")

	for _, v := range []string{"v1.2.0", "v1.0.0"} {
		artifact := &database.ArtifactMetadata{
			ID:           fmt.Sprintf("go:go:%s:%s", module, v),
			RegistryID:   "go",
			ArtifactType: "go",
			ArtifactName: module,
			Version:      v,
			Metadata:     map[string]interface{}{},
			Tags:         []string{v},
		}
		require.NoError(t, db.SaveArtifact(artifact))
		v := v
		t.Cleanup(func() { db.DeleteArtifact("go", "", module, v) })
	}
	otherArtifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("go:go:%s:v9.9.9", other),
		RegistryID:   "go",
		ArtifactType: "go",
		ArtifactName: other,
		Version:      "v9.9.9",
		Metadata:     map[string]interface{}{},
		Tags:         []string{"v9.9.9"},
	}
	require.NoError(t, db.SaveArtifact(otherArtifact))
	t.Cleanup(func() { db.DeleteArtifact("go", "", other, "v9.9.9") })

	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@v/list", nil)
	rec := httptest.NewRecorder()

	p.handleRequest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, "v1.0.0")
	assert.Contains(t, body, "v1.2.0")
	assert.NotContains(t, body, "v9.9.9")
	// sorted lexically: v1.0.0 before v1.2.0
	assert.True(t, indexOf(body, "v1.0.0") < indexOf(body, "v1.2.0"))
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// TestHandleLatestNoProxyConfigured covers @latest with no upstream
// resolvable from context in a direct handler call: fetchInfoFromUpstream
// fails closed and the real response is 404.
func TestHandleLatestNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/latest-mod")
	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@latest", nil)
	rec := httptest.NewRecorder()

	p.handleRequest(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleLatestFetchesUpstream exercises the full router (so
// TargetFromContext resolves a real registry with a proxy URL) for @latest.
func TestHandleLatestFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/latest-upstream")
	version := "v1.5.0"
	created := time.Now().UTC().Truncate(time.Second)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/"+module+"/@latest", r.URL.Path)
		json.NewEncoder(w).Encode(versionInfo{Version: version, Time: created})
	}))
	defer upstream.Close()

	host := uniqueID("gomod-latest-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, upstream.URL, true)
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", module, version) })

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@latest", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var info versionInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&info))
	assert.Equal(t, version, info.Version)
}

// TestHandleVersionedInfoCacheHit covers @v/{version}.info when the version
// is already known to the database. Note: handleVersioned's .info lookup
// hardcodes the registry ID as the literal "go", not the resolved target's
// label, so seeded artifacts must use RegistryID "go".
func TestHandleVersionedInfoCacheHit(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/info-mod")
	version := "v2.0.0"
	created := time.Now().UTC().Truncate(time.Second)

	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("go:go:%s:%s", module, version),
		RegistryID:   "go",
		ArtifactType: "go",
		ArtifactName: module,
		Version:      version,
		Created:      created,
		Metadata:     map[string]interface{}{},
		Tags:         []string{version},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("go", "", module, version) })

	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@v/"+version+".info", nil)
	rec := httptest.NewRecorder()

	p.handleRequest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var info versionInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&info))
	assert.Equal(t, version, info.Version)
	assert.True(t, created.Equal(info.Time))
}

// TestHandleVersionedInfoCacheMissFetchesUpstream covers @v/{version}.info
// on a miss: it falls through to the upstream fetch.
func TestHandleVersionedInfoCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/info-miss-mod")
	version := "v3.0.0"
	created := time.Now().UTC().Truncate(time.Second)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/"+module+"/@v/"+version+".info", r.URL.Path)
		json.NewEncoder(w).Encode(versionInfo{Version: version, Time: created})
	}))
	defer upstream.Close()

	host := uniqueID("gomod-info-miss-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, upstream.URL, true)
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", module, version) })

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@v/"+version+".info", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var info versionInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&info))
	assert.Equal(t, version, info.Version)
}

// TestHandleVersionedModFromStorage covers @v/{version}.mod when already
// present in local storage (this path was never converted to streaming --
// go.mod files are small text -- so it still uses the buffered
// storage.GetArtifact/SaveArtifact API directly).
func TestHandleVersionedModFromStorage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/mod-mod")
	version := "v1.0.0"
	modData := []byte("module " + module + "\n\ngo 1.21\n")

	_, err := p.storage.SaveArtifact("go", "", module, version, modData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@v/"+version+".mod", nil)
	rec := httptest.NewRecorder()

	p.handleRequest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	assert.Equal(t, modData, rec.Body.Bytes())
}

// TestHandleVersionedModCacheMissFetchesUpstream covers @v/{version}.mod on
// a miss: it falls through to the upstream fetch and saves the result.
func TestHandleVersionedModCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/mod-miss-mod")
	version := "v1.0.0"
	modData := []byte("module " + module + "\n\ngo 1.21\n")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/"+module+"/@v/"+version+".mod", r.URL.Path)
		w.Write(modData)
	}))
	defer upstream.Close()

	host := uniqueID("gomod-mod-miss-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, upstream.URL, true)

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@v/"+version+".mod", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, modData, rec.Body.Bytes())

	stored, err := p.storage.GetArtifact(reg.ID, "", module, version)
	require.NoError(t, err)
	assert.Equal(t, modData, stored)
}

// TestHandleVersionedZipCacheHit covers @v/{version}.zip when already
// streamed into storage.
func TestHandleVersionedZipCacheHit(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/zip-mod")
	version := "v1.0.0"
	zipData := []byte("fake module zip content")

	_, err := p.storage.SaveArtifactStream("go", "", module, version, bytes.NewReader(zipData))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@v/"+version+".zip", nil)
	rec := httptest.NewRecorder()

	p.handleRequest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Equal(t, zipData, rec.Body.Bytes())
}

// TestHandleVersionedZipOldBufferedArtifactStillServable simulates a module
// zip that was already cached on disk via the pre-streaming-refactor
// buffered SaveArtifact call, before this deploy. handleVersioned's .zip
// branch now reads via GetArtifactStream, but LocalAdapter's stream/buffered
// paths share the same on-disk file layout, so an artifact saved the old
// way must still be servable correctly after the upgrade.
func TestHandleVersionedZipOldBufferedArtifactStillServable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/old-zip-mod")
	version := "v2.0.0"
	zipData := []byte("old-style pre-refactor buffered module zip bytes")

	// Simulate a pre-existing on-disk artifact saved by the old, buffered
	// SaveArtifact API (i.e. what was on disk before this deploy).
	_, err := p.storage.SaveArtifact("go", "", module, version, zipData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@v/"+version+".zip", nil)
	rec := httptest.NewRecorder()

	p.handleRequest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Equal(t, zipData, rec.Body.Bytes())
}

// TestHandleVersionedZipCacheMissFetchesUpstream exercises the full router
// (so TargetFromContext resolves a real registry with a proxy URL) on a
// cache miss: the fake upstream server serves the zip, it gets streamed
// into storage, and served back.
func TestHandleVersionedZipCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/zip-miss-mod")
	version := "v1.0.0"
	zipData := []byte("fake upstream module zip payload")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/"+module+"/@v/"+version+".zip", r.URL.Path)
		w.Write(zipData)
	}))
	defer upstream.Close()

	host := uniqueID("gomod-zip-miss-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, upstream.URL, true)
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", module, version) })

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@v/"+version+".zip", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, zipData, rec.Body.Bytes())

	stored, err := p.storage.GetArtifact(reg.ID, "", module, version)
	require.NoError(t, err)
	assert.Equal(t, zipData, stored)
}

// TestHandleVersionedZipUpstreamFailureNoProxyConfigured covers a cache miss
// with no upstream registry resolvable (direct handler call has no
// Target.Reg), so fetchZipStreamFromUpstream fails and the real response is
// 502.
func TestHandleVersionedZipUpstreamFailureNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	module := uniqueID("github.com/example/never-cached")
	version := "v1.0.0"
	req := httptest.NewRequest(http.MethodGet, "/"+module+"/@v/"+version+".zip", nil)
	rec := httptest.NewRecorder()

	p.handleRequest(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleRequestUnknownPathNotFound covers a path that matches none of
// the well-known GOPROXY suffixes.
func TestHandleRequestUnknownPathNotFound(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/github.com/example/mod/unknown.txt", nil)
	rec := httptest.NewRecorder()

	p.handleRequest(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestFetchInfoFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	reg := &database.RegistryConfig{ID: "go", Proxy: false}
	info, err := p.fetchInfoFromUpstream(reg, "go", "github.com/example/mod", "v1.0.0")
	assert.Error(t, err)
	assert.Nil(t, info)

	info, err = p.fetchInfoFromUpstream(nil, "go", "github.com/example/mod", "v1.0.0")
	assert.Error(t, err)
	assert.Nil(t, info)
}

func TestFetchModFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	data, err := p.fetchModFromUpstream(nil, "github.com/example/mod", "v1.0.0")
	assert.Error(t, err)
	assert.Nil(t, data)
}

func TestFetchZipStreamFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	resp, err := p.fetchZipStreamFromUpstream(nil, "github.com/example/mod", "v1.0.0")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestEscapeModulePath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"all lowercase unchanged", "github.com/example/mod", "github.com/example/mod"},
		{"escapes uppercase", "github.com/Example/Mod", "github.com/!example/!mod"},
		{"mixed case", "github.com/BurntSushi/toml", "github.com/!burnt!sushi/toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, escapeModulePath(tt.in))
		})
	}
}

func TestUnescapeModulePath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no escapes", "github.com/example/mod", "github.com/example/mod"},
		{"unescapes", "github.com/!example/!mod", "github.com/Example/Mod"},
		{"mixed case round trip", "github.com/!burnt!sushi/toml", "github.com/BurntSushi/toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, unescapeModulePath(tt.in))
		})
	}
}

// TestModulePathEscapeRoundTrip verifies escapeModulePath/unescapeModulePath
// invert each other for a variety of module paths.
func TestModulePathEscapeRoundTrip(t *testing.T) {
	paths := []string{
		"github.com/example/mod",
		"github.com/BurntSushi/toml",
		"golang.org/x/Text",
		"UPPER/CASE/PATH",
	}
	for _, path := range paths {
		escaped := escapeModulePath(path)
		assert.Equal(t, path, unescapeModulePath(escaped))
	}
}

// TestGoModProxyRoutesPrivateRequiresAuth verifies the RequireReadAccess
// middleware mounted by NewGoModProxy enforces access control end to end: an
// anonymous request against a private, Host-bound registry is rejected
// before it ever reaches a handler.
func TestGoModProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("gomod-private-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("gomod-private-reg"),
		Name:     "gomod-private-reg",
		Type:     "go",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/github.com/example/mod/@v/list", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
