package composer

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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *ComposerProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &ComposerProxy{db: db, storage: adapter, cache: c}
}

func newTestRouter(p *ComposerProxy, registries []database.RegistryConfig) http.Handler {
	return NewComposerProxy(p.db, p.storage, p.cache, rbac.New(p.db), registries)
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// seedRegistryWithHost saves a registry bound to a virtual host, so
// resolveTarget/ResolveRegistry picks it deterministically instead of
// falling back to whatever registry (if any) other concurrently-run tests
// left as the DB-wide default for artifactType "composer".
func seedRegistryWithHost(t *testing.T, db *database.Database, host string, proxyURL string, proxyEnabled bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("composer-host-reg"),
		Name:     "composer-host-reg",
		URL:      proxyURL,
		Type:     "composer",
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

func TestNewComposerProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	registries := []database.RegistryConfig{{ID: "composer", Name: "Composer", Type: "composer", Proxy: true, Enabled: true}}

	router := NewComposerProxy(db, nil, c, rbac.New(db), registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
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

// TestHandlePackagesJSONDirect covers handlePackagesJSON's direct-call path,
// where TargetFromContext falls back to the literal "composer" label.
func TestHandlePackagesJSONDirect(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	vendor := uniqueID("acme")
	name := "widgets"
	artifact := &database.ArtifactMetadata{
		RegistryID:   "composer",
		ArtifactType: "composer",
		Namespace:    vendor,
		ArtifactName: name,
		Version:      "1.0.0",
		Tags:         []string{"1.0.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("composer", vendor, name, "1.0.0") })

	req := httptest.NewRequest(http.MethodGet, "/packages.json", nil)
	req.Host = "cargobay.example.com"
	rec := httptest.NewRecorder()

	p.handlePackagesJSON(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body struct {
		Packages map[string]map[string]composerPackage `json:"packages"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	fullName := vendor + "/" + name
	require.Contains(t, body.Packages, fullName)
	require.Contains(t, body.Packages[fullName], "1.0.0")
	pkg := body.Packages[fullName]["1.0.0"]
	assert.Equal(t, fullName, pkg.Name)
	assert.Equal(t, "zip", pkg.Dist.Type)
	assert.Equal(t, fmt.Sprintf("http://cargobay.example.com/dist/%s/%s/1.0.0.zip", vendor, name), pkg.Dist.URL)
}

// TestHandleDistServedFromStorageStream covers the fast path where the .zip
// is already cached in storage via the new streaming save.
func TestHandleDistServedFromStorageStream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	vendor := uniqueID("acme")
	name := "widgets"
	version := "1.0.0"
	data := []byte("fake zip stream content")
	_, err := p.storage.SaveArtifactStream("composer", vendor, name, version, bytes.NewReader(data))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/dist/%s/%s/%s.zip", vendor, name, version), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("vendor", vendor)
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("*", version+".zip")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleDist(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandleDistOldBufferedArtifactStillServable is the critical
// backward-compatibility test: a .zip written by the OLD, pre-refactor
// buffered storage.SaveArtifact call (simulating a package already cached
// on disk before this streaming refactor ships) must still be served
// correctly now that the handler calls GetArtifactStream.
func TestHandleDistOldBufferedArtifactStillServable(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	vendor := uniqueID("legacy-vendor")
	name := "legacy-pkg"
	version := "2.0.0"
	data := []byte("legacy pre-refactor buffered zip bytes")

	_, err := p.storage.SaveArtifact("composer", vendor, name, version, data)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/dist/%s/%s/%s.zip", vendor, name, version), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("vendor", vendor)
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("*", version+".zip")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleDist(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandleDistNoUpstreamConfigured covers the not-in-storage path with no
// target registry resolvable from context: resolveUpstreamDistURL fails
// closed and the real response is 404 (composer's error path for an
// unresolvable dist URL, not 502).
func TestHandleDistNoUpstreamConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	vendor := uniqueID("missing-vendor")
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/dist/%s/missing-pkg/1.0.0.zip", vendor), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("vendor", vendor)
	rctx.URLParams.Add("name", "missing-pkg")
	rctx.URLParams.Add("*", "1.0.0.zip")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleDist(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestResolveUpstreamDistURLNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	url, err := p.resolveUpstreamDistURL(nil, "acme", "widgets", "1.0.0")
	assert.Error(t, err)
	assert.Empty(t, url)

	reg := &database.RegistryConfig{ID: "composer", Proxy: false}
	url, err = p.resolveUpstreamDistURL(reg, "acme", "widgets", "1.0.0")
	assert.Error(t, err)
	assert.Empty(t, url)
}

func TestResolveUpstreamDistURLSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	vendor := "acme"
	name := "widgets"
	version := "1.0.0"
	distURL := "https://vcs.example.com/acme/widgets/archive/1.0.0.zip"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/packages.json", r.URL.Path)
		doc := map[string]interface{}{
			"packages": map[string]interface{}{
				vendor + "/" + name: map[string]interface{}{
					version: composerPackage{
						Name:    vendor + "/" + name,
						Version: version,
						Dist:    composerDistRef{Type: "zip", URL: distURL},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(doc)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "composer", URL: upstream.URL, Proxy: true}
	got, err := p.resolveUpstreamDistURL(reg, vendor, name, version)
	require.NoError(t, err)
	assert.Equal(t, distURL, got)
}

func TestResolveUpstreamDistURLPackageNotFound(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"packages": map[string]interface{}{}})
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{ID: "composer", URL: upstream.URL, Proxy: true}
	got, err := p.resolveUpstreamDistURL(reg, "acme", "missing", "1.0.0")
	assert.Error(t, err)
	assert.Empty(t, got)
}

func TestFetchBytesAndFetchStream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	_ = c
	_ = db

	body := []byte("hello world")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer upstream.Close()

	got, err := fetchBytes(nil, upstream.URL)
	require.NoError(t, err)
	assert.Equal(t, body, got)

	resp, err := fetchStream(nil, upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	streamed, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, streamed)
}

func TestFetchStreamUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	resp, err := fetchStream(nil, upstream.URL)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestHandleDistFetchesFromUpstreamViaRouter drives handleDist behind the
// real RequireReadAccess middleware (so a live registry is resolved into
// context exactly as it would be in production) on a cache miss, proving
// the resolve-dist-url -> fetch -> SaveArtifactStream -> serve pipeline
// works end to end, and that the real chi router actually matches
// "/dist/{vendor}/{name}/*" against a request path ending in ".zip".
func TestHandleDistFetchesFromUpstreamViaRouter(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	vendor := uniqueID("acme")
	name := "gizmo"
	version := "3.0.0"
	body := []byte("real upstream zip content")

	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/packages.json" {
			doc := map[string]interface{}{
				"packages": map[string]interface{}{
					vendor + "/" + name: map[string]interface{}{
						version: composerPackage{
							Name:    vendor + "/" + name,
							Version: version,
							Dist:    composerDistRef{Type: "zip", URL: upstream.URL + "/real.zip"},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(doc)
			return
		}
		assert.Equal(t, "/real.zip", r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	host := uniqueID("composer-upstream-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, upstream.URL, true)

	router := chi.NewRouter()
	router.Use(proxypkg.RequireReadAccess(p.db, rbac.New(p.db), nil, "composer"))
	router.Get("/dist/{vendor}/{name}/*", p.handleDist)

	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/dist/%s/%s/%s.zip", vendor, name, version), nil)
		req.Host = host
		return req
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, newReq())

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, rec.Body.Bytes())
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, vendor, name, version) })

	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, newReq())
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Equal(t, body, rec2.Body.Bytes())
}

func TestComposerProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("private-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("private-reg"),
		Name:     "private-reg",
		Type:     "composer",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, "/packages.json", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestComposerProxyRoutesPublicPackagesJSON(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("public-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("public-reg"),
		Name:     "public-reg",
		Type:     "composer",
		Enabled:  true,
		Priority: 100,
		Private:  false,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := newTestRouter(p, nil)
	req := httptest.NewRequest(http.MethodGet, "/packages.json", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
