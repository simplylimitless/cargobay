package nuget

import (
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *NuGetProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &NuGetProxy{
		db:         db,
		storage:    adapter,
		cache:      c,
		registries: nil,
		registry:   "nuget",
	}
}

// uniqueID produces a collision-free identifier for registries/artifact IDs
// since tests share one live DB. It is NOT safe to use as a tsquery search
// term (the hyphen is a NOT operator to to_tsquery) — use uniqueWord for
// anything that gets passed through SearchArtifacts.
func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// uniqueWord is like uniqueID but alphanumeric-only, safe to use as both an
// artifact name and a to_tsquery search term.
func uniqueWord(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
}

// saveArtifact seeds an artifact row under registryID. Several nuget.go
// single-artifact handlers (handlePackageMetadata, handlePackageV2,
// handleRegistrationVersion, handleDownload) hard-code the literal registry
// ID "nuget" when looking artifacts up — instead of the Host-resolved
// proxypkg.Target.Label the read-access middleware computes for every other
// handler — so callers exercising those specific handlers must seed under
// registryID "nuget" for the row to actually be found. See the comments on
// TestHandlePackageMetadataFound et al.
func saveArtifact(t *testing.T, db *database.Database, registryID, namespace, name, version string) *database.ArtifactMetadata {
	t.Helper()
	artifact := &database.ArtifactMetadata{
		RegistryID:      registryID,
		ArtifactType:    "nuget",
		Namespace:       namespace,
		ArtifactName:    name,
		Version:         version,
		Digest:          "sha256:" + version,
		DigestAlgorithm: "sha256",
		Tags:            []string{version},
		Metadata: map[string]interface{}{
			"metadata": map[string]interface{}{
				"description": "test package",
				"title":       name,
			},
		},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact(registryID, namespace, name, version) })
	return artifact
}

// bindHostRegistry saves a registry bound to a unique Host, so requests sent
// with that Host resolve deterministically via ResolveRegistry instead of
// depending on whatever registry (if any) other concurrently-run test
// suites left as the DB-wide default for artifactType "nuget".
func bindHostRegistry(t *testing.T, db *database.Database, private, proxy bool) (*database.RegistryConfig, string) {
	t.Helper()
	host := uniqueID("nuget-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("nuget-reg"),
		Name:     "nuget-reg-bound",
		URL:      "https://upstream.example.com",
		Type:     "nuget",
		Enabled:  true,
		Priority: 100,
		Private:  private,
		Proxy:    proxy,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })
	return reg, host
}

// withChiParams attaches a synthetic chi routing context carrying params, so
// handlers that read chi.URLParam (handlePackageMetadata, handlePackageV2,
// handleDownload, handleRegistrationIndex, handleRegistrationVersion) can be
// called directly — white-box, bypassing the router entirely — without
// needing a real chi match. This sidesteps two real routing quirks
// discovered while building these tests (documented in
// TestRegistrationVersionRouteUnreachableForDottedSemver and
// TestDownloadRouteShadowedByPackageMetadata below): chi's default
// {version}.json / {version}.nupkg patterns don't reliably match versions
// that themselves contain dots (e.g. "1.0.0"), which is nuget's standard
// version format.
func withChiParams(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestNewNuGetProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	router := NewNuGetProxy(p.db, p.storage, p.cache, rbac.New(p.db), p.registries)
	assert.NotNil(t, router)
}

func TestHandleServiceIndex(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/v3/index.json", nil)
	req.Host = "cargobay.test"
	rec := httptest.NewRecorder()

	p.handleServiceIndex(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "3.0.0", body["version"])

	resources, ok := body["resources"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, resources)

	first := resources[0].(map[string]interface{})
	assert.Contains(t, first["@id"], "http://cargobay.test/nuget/query")
}

// TestHandleQuery covers handleQuery on a cold cache: cache.Cache.Get now
// returns cache.ErrCacheMiss on a miss (see pkg/cache/redis.go), so
// handleQuery's `if data, err := n.cacheGet(...); err == nil` check
// correctly falls through to SearchArtifacts instead of short-circuiting to
// an empty body.
func TestHandleQuery(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueWord("querypkg")
	saveArtifact(t, db, "nuget", "", name, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/query?q="+name, nil)
	rec := httptest.NewRecorder()

	p.handleQuery(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	results := body["data"].([]interface{})
	require.Len(t, results, 1)
	assert.Equal(t, name, results[0].(map[string]interface{})["id"])
}

// TestHandleSearchV2 covers the same cold-cache-miss fallthrough as
// TestHandleQuery, for the V2 search endpoint.
func TestHandleSearchV2(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueWord("searchv2pkg")
	saveArtifact(t, db, "nuget", "", name, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/Search()?searchTerm="+name, nil)
	rec := httptest.NewRecorder()

	p.handleSearchV2(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), name)
}

// TestHandlePackageMetadataFound seeds under registryID "nuget" (not a
// resolved registry ID) because handlePackageMetadata hard-codes that
// literal when looking the artifact up — see the saveArtifact doc comment.
func TestHandlePackageMetadataFound(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("metapkg")
	saveArtifact(t, db, "nuget", "", name, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/package/"+name+"/1.0.0", nil)
	req = withChiParams(req, map[string]string{"packageId": name, "version": "1.0.0"})
	rec := httptest.NewRecorder()

	p.handlePackageMetadata(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	data := body["data"].(map[string]interface{})
	assert.Equal(t, name, data["id"])
	assert.Equal(t, "1.0.0", data["version"])
}

func TestHandlePackageMetadataNotFoundNoUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/package/missing-pkg/9.9.9", nil)
	req = withChiParams(req, map[string]string{"packageId": "missing-pkg", "version": "9.9.9"})
	rec := httptest.NewRecorder()

	p.handlePackageMetadata(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandleDownload(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("downloadpkg")
	nupkgData := []byte("fake nupkg content")
	// handleDownload's storage lookup, like the DB lookups above, hard-codes
	// registryID "nuget" rather than the resolved target label.
	_, err := p.storage.SaveArtifact("nuget", "", name, "1.0.0", nupkgData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/package/"+name+"/1.0.0.nupkg", nil)
	req = withChiParams(req, map[string]string{"packageId": name, "version": "1.0.0"})
	rec := httptest.NewRecorder()

	p.handleDownload(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "3.0.0", rec.Header().Get("X-NuGet-Protocol-Version"))
	assert.Equal(t, nupkgData, rec.Body.Bytes())
}

func TestHandleDownloadNotFoundNoUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/package/missing-pkg/9.9.9.nupkg", nil)
	req = withChiParams(req, map[string]string{"packageId": "missing-pkg", "version": "9.9.9"})
	rec := httptest.NewRecorder()

	p.handleDownload(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandlePackageV2(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("v2pkg")
	saveArtifact(t, db, "nuget", "", name, "2.0.0")

	req := httptest.NewRequest(http.MethodGet, "/Package/"+name+"/2.0.0", nil)
	req = withChiParams(req, map[string]string{"packageId": name, "version": "2.0.0"})
	rec := httptest.NewRecorder()

	p.handlePackageV2(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	d := body["d"].(map[string]interface{})
	assert.Equal(t, fmt.Sprintf("%s|2.0.0", name), d["Id"])
}

// TestHandleListPackages exercises handleListPackages directly. Unlike the
// single-artifact handlers above, it correctly scopes by
// proxypkg.TargetFromContext(r, "nuget").Label rather than a hard-coded
// literal; called directly (no read-access middleware) that resolves to the
// same "nuget" fallback label, so artifacts are seeded under that.
func TestHandleListPackages(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("listpkg")
	saveArtifact(t, db, "nuget", "", name, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/Package()", nil)
	rec := httptest.NewRecorder()

	p.handleListPackages(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	results := body["d"].([]interface{})
	found := false
	for _, r := range results {
		if r.(map[string]interface{})["Id"] == fmt.Sprintf("%s|1.0.0", name) {
			found = true
		}
	}
	assert.True(t, found, "expected seeded package in list results")
}

func TestHandleRegistrationIndex(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("regidxpkg")
	saveArtifact(t, db, "nuget", "", name, "1.0.0")
	saveArtifact(t, db, "nuget", "", name, "2.0.0")

	req := httptest.NewRequest(http.MethodGet, "/registration/"+name+"/index.json", nil)
	req = withChiParams(req, map[string]string{"packageId": name})
	rec := httptest.NewRecorder()

	p.handleRegistrationIndex(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, float64(2), body["count"])
	items := body["items"].([]interface{})
	assert.Len(t, items, 2)
}

func TestHandleRegistrationVersionFound(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	name := uniqueID("regverpkg")
	saveArtifact(t, db, "nuget", "", name, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/registration/"+name+"/1.0.0.json", nil)
	req = withChiParams(req, map[string]string{"packageId": name, "version": "1.0.0"})
	rec := httptest.NewRecorder()

	p.handleRegistrationVersion(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	entry := body["catalogEntry"].(map[string]interface{})
	assert.Equal(t, name, entry["id"])
	assert.Equal(t, "1.0.0", entry["version"])
}

func TestHandleRegistrationVersionNotFoundNoUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/registration/missing-pkg/9.9.9.json", nil)
	req = withChiParams(req, map[string]string{"packageId": "missing-pkg", "version": "9.9.9"})
	rec := httptest.NewRecorder()

	p.handleRegistrationVersion(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestRegistrationVersionRouteUnreachableForDottedSemver documents a real
// chi routing bug found while writing these tests: the registered pattern
// "/registration/{packageId}/{version}.json" only matches when version has
// no embedded dots. A standard NuGet semver like "1.0.0" (as opposed to,
// say, a single integer) fails to match at all and 404s before
// handleRegistrationVersion is ever invoked — verified independently with a
// minimal chi router outside this package. This is a real, pre-existing
// bug in the route registration in nuget.go, not a test artifact; since
// production code is out of scope for this test-only rewrite, this test
// simply pins the actual (broken) behavior instead of silently ignoring it.
func TestRegistrationVersionRouteUnreachableForDottedSemver(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	_, host := bindHostRegistry(t, db, false, false)
	router := NewNuGetProxy(p.db, p.storage, p.cache, rbac.New(p.db), p.registries)
	server := httptest.NewServer(router)
	defer server.Close()

	httpReq, err := http.NewRequest(http.MethodGet, server.URL+"/registration/somepkg/1.0.0.json", nil)
	require.NoError(t, err)
	httpReq.Host = host
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "route pattern doesn't match dotted semver versions")
}

// TestDownloadRouteShadowedByPackageMetadata documents a second real chi
// routing bug: "/package/{packageId}/{version}" (handlePackageMetadata) and
// "/package/{packageId}/{version}.nupkg" (handleDownload) are registered
// against the same path shape, and for a dotted version like "1.0.0" chi
// resolves the request to the plain (metadata) route — matching version
// "1.0.0.nupkg" literally — rather than ever reaching handleDownload. This
// test pins that actual dispatch behavior (a 502 from the metadata
// handler's own not-found path) rather than the naively-expected 200 from a
// download.
func TestDownloadRouteShadowedByPackageMetadata(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	_, host := bindHostRegistry(t, db, false, false)
	router := NewNuGetProxy(p.db, p.storage, p.cache, rbac.New(p.db), p.registries)
	server := httptest.NewServer(router)
	defer server.Close()

	httpReq, err := http.NewRequest(http.MethodGet, server.URL+"/package/somepkg/1.0.0.nupkg", nil)
	require.NoError(t, err)
	httpReq.Host = host
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Reaches handlePackageMetadata's upstream-fallback path (no upstream
	// configured), not handleDownload's.
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

func TestRequireReadAccessPrivateRegistryDeniesAnonymous(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	_, host := bindHostRegistry(t, db, true, false)

	router := NewNuGetProxy(p.db, p.storage, p.cache, rbac.New(p.db), p.registries)
	server := httptest.NewServer(router)
	defer server.Close()

	httpReq, err := http.NewRequest(http.MethodGet, server.URL+"/query", nil)
	require.NoError(t, err)
	httpReq.Host = host
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestNuGetProxyIntegration(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	router := NewNuGetProxy(p.db, p.storage, p.cache, rbac.New(p.db), p.registries)
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/v3/index.json")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	queryResp, err := http.Get(server.URL + "/query")
	require.NoError(t, err)
	defer queryResp.Body.Close()
	assert.Equal(t, http.StatusOK, queryResp.StatusCode)
}

func TestCacheConcurrency(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			key := fmt.Sprintf("nuget-test-concurrency-%d", i)
			p.cacheSet(key, []byte("value"))
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestNuGetProxyRoutes(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	// Bind a dedicated, non-proxying registry by Host so resolution is
	// deterministic and doesn't depend on leftover state from other
	// tests/packages sharing the live DB.
	_, host := bindHostRegistry(t, db, false, false)

	router := NewNuGetProxy(p.db, p.storage, p.cache, rbac.New(p.db), p.registries)
	server := httptest.NewServer(router)
	defer server.Close()

	routes := []string{
		"/v3/index.json",
		"/query",
		"/Search()",
		"/Package()",
	}

	for _, route := range routes {
		httpReq, err := http.NewRequest(http.MethodGet, server.URL+route, nil)
		require.NoError(t, err)
		httpReq.Host = host
		resp, err := http.DefaultClient.Do(httpReq)
		require.NoError(t, err)
		resp.Body.Close()
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode, "route %s should exist", route)
	}
}
