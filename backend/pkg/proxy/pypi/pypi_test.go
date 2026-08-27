package pypi

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *PyPIProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &PyPIProxy{
		db:         db,
		storage:    adapter,
		cache:      c,
		registries: nil,
		registry:   "pypi",
	}
}

func newTestRouter(p *PyPIProxy) http.Handler {
	return NewPyPIProxy(p.db, p.storage, p.cache, rbac.New(p.db), p.registries)
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// seedRegistry saves a plain (host-unbound) registry, used for tests that
// don't need Host-based resolution.
func seedRegistry(t *testing.T, db *database.Database, id string, private, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       id,
		Name:     id,
		URL:      "https://upstream.example.com",
		Type:     "pypi",
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
// suites left as the DB-wide default for artifactType "pypi".
func seedRegistryWithHost(t *testing.T, db *database.Database, host string, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("pypi-host-reg"),
		Name:     "pypi-host-reg",
		URL:      "https://upstream.example.com",
		Type:     "pypi",
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

func TestNewPyPIProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	router := newTestRouter(p)
	assert.NotNil(t, router)
}

// TestHandleSimpleRootMissFallsThroughToDatabase exercises the "/simple/"
// index via the full router (so the target/registry label context that
// handleSimpleRoot depends on is populated by RequireReadAccess middleware,
// the same way it would be in production).
//
// On a genuine cache miss, cache.Cache.Get now returns cache.ErrCacheMiss
// (see pkg/cache/redis.go), so PyPIProxy.cacheGet correctly reports a miss
// and handleSimpleRoot falls through to query the database instead of
// serving an empty cached body.
func TestHandleSimpleRootMissFallsThroughToDatabase(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)
	require.NoError(t, c.Delete("-simple-root-"))

	host := uniqueID("simple-root-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, false)

	pkgName := uniqueID("reqs")
	artifact := &database.ArtifactMetadata{
		RegistryID:   reg.ID,
		ArtifactType: "pypi",
		Namespace:    "",
		ArtifactName: pkgName,
		Version:      "1.0.0",
		Tags:         []string{"1.0.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", pkgName, "1.0.0") })

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/simple/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), pkgName)
}

// TestHandlePackageIndexMissFallsThroughToDatabase exercises
// "/simple/{packageName}/" on a genuine cache miss: with two versions
// freshly seeded in the DB, the handler now falls through to db.ListArtifacts
// and serves them instead of an empty cached body.
func TestHandlePackageIndexMissFallsThroughToDatabase(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("pkg-index-host") + ".test"
	reg := seedRegistryWithHost(t, db, host, false)

	pkgName := uniqueID("flask")
	require.NoError(t, c.Delete(fmt.Sprintf("-simple-%s-", pkgName)))
	for _, v := range []string{"1.0.0", "2.0.0"} {
		artifact := &database.ArtifactMetadata{
			RegistryID:   reg.ID,
			ArtifactType: "pypi",
			Namespace:    "",
			ArtifactName: pkgName,
			Version:      v,
			Tags:         []string{v},
			Metadata:     map[string]interface{}{},
		}
		require.NoError(t, db.SaveArtifact(artifact))
		v := v
		t.Cleanup(func() { db.DeleteArtifact(reg.ID, "", pkgName, v) })
	}

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/simple/"+pkgName+"/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), pkgName+"-1.0.0")
	assert.Contains(t, rec.Body.String(), pkgName+"-2.0.0")
}

// TestHandlePackageVersionFound exercises "/simple/{packageName}/{version}/"
// for an artifact already in the database.
//
// Quirk: handlePackageVersion looks the artifact up via
// db.GetArtifactByParams("pypi", ...) with the artifact-type string
// literally hardcoded as the registry ID — it does NOT use the resolved
// target's label/registry. So regardless of which (Host-bound) registry
// served the request, the seeded artifact's RegistryID must be the literal
// "pypi" for the lookup to find it.
func TestHandlePackageVersionFound(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("pkg-version-host") + ".test"
	seedRegistryWithHost(t, db, host, false)

	pkgName := uniqueID("django")
	artifact := &database.ArtifactMetadata{
		RegistryID:   "pypi",
		ArtifactType: "pypi",
		Namespace:    "",
		ArtifactName: pkgName,
		Version:      "4.0.0",
		Tags:         []string{"4.0.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("pypi", "", pkgName, "4.0.0") })

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/simple/"+pkgName+"/4.0.0/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, pkgName)
	assert.Contains(t, body, "4.0.0")
}

// TestHandlePackageVersionNotFoundNoUpstream covers the not-in-DB path: with
// no matching artifact and a registry that has no upstream proxy configured
// (Proxy: false), fetchPackageFromUpstream fails and the handler reports
// StatusBadGateway (there's no 404 branch here — it's "couldn't fetch",
// not "doesn't exist").
func TestHandlePackageVersionNotFoundNoUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("pkg-version-404-host") + ".test"
	seedRegistryWithHost(t, db, host, false)

	pkgName := uniqueID("missing-pkg")
	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/simple/"+pkgName+"/9.9.9/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandlePackageFileFromStorage exercises "/packages/{pkg}/{version}/{file}"
// when the file is already present in the storage adapter.
//
// Quirk: like handlePackageVersion, handlePackageFile's storage lookup uses
// the literal "pypi" as the registry key (p.storage.GetArtifact("pypi", ...))
// rather than the resolved target's label, so we save under "pypi" too.
func TestHandlePackageFileFromStorage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("pkg-file-host") + ".test"
	seedRegistryWithHost(t, db, host, false)

	pkgName := uniqueID("numpy")
	version := "1.23.0"
	fileName := fmt.Sprintf("%s-%s-py3-none-any.whl", pkgName, version)
	data := []byte("fake wheel content")
	_, err := p.storage.SaveArtifact("pypi", "", pkgName, version, data)
	require.NoError(t, err)

	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/packages/%s/%s/%s", pkgName, version, fileName), nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), fileName)
	assert.Equal(t, data, rec.Body.Bytes())
}

// TestHandlePackageFileNotFoundNoUpstream covers a file present in neither
// storage nor upstream (registry has Proxy: false), which reports
// StatusBadGateway.
func TestHandlePackageFileNotFoundNoUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("pkg-file-404-host") + ".test"
	seedRegistryWithHost(t, db, host, false)

	pkgName := uniqueID("missing-file-pkg")
	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/packages/%s/1.0.0/%s-1.0.0.tar.gz", pkgName, pkgName), nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestHandleLegacyPackageRedirect(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("legacy-pkg-host") + ".test"
	seedRegistryWithHost(t, db, host, false)

	pkgName := uniqueID("legacy-pkg")
	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/"+pkgName+"/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, "/simple/"+pkgName+"/", rec.Header().Get("Location"))
}

func TestHandleLegacyPackageVersionRedirect(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("legacy-pkg-version-host") + ".test"
	seedRegistryWithHost(t, db, host, false)

	pkgName := uniqueID("legacy-pkg-v")
	router := newTestRouter(p)
	req := httptest.NewRequest(http.MethodGet, "/"+pkgName+"/1.2.3/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, "/simple/"+pkgName+"/1.2.3/", rec.Header().Get("Location"))
}

// TestPyPIProxyIntegration exercises the router end-to-end over a real HTTP
// server, binding a registry by Host so resolution is deterministic (see
// seedRegistryWithHost doc comment) instead of depending on leftover
// default-registry state from other tests/packages sharing the live DB.
func TestPyPIProxyIntegration(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	host := uniqueID("integration-host") + ".test"
	seedRegistryWithHost(t, db, host, false)

	router := newTestRouter(p)
	server := httptest.NewServer(router)
	defer server.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	get := func(path string) *http.Response {
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		require.NoError(t, err)
		req.Host = host
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		_, _ = io.ReadAll(resp.Body)
		return resp
	}

	resp := get("/simple/")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	pkgName := uniqueID("int-pkg")
	resp = get("/simple/" + pkgName + "/")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp = get("/" + pkgName + "/")
	assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode)
}

// TestCacheConcurrency verifies concurrent writes through a live cache
// don't race or error.
func TestCacheConcurrency(t *testing.T) {
	c := connectTestCache(t)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			key := fmt.Sprintf("pypi-test-concurrency-%d", i)
			err := c.Set(key, []byte("value"))
			assert.NoError(t, err)
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestEmptyNamespace verifies artifacts with an empty namespace (the norm
// for PyPI, which has no namespace concept) are listed correctly.
func TestEmptyNamespace(t *testing.T) {
	db := connectTestDB(t)

	regID := uniqueID("empty-ns-reg")
	seedRegistry(t, db, regID, false, false)

	pkgName := uniqueID("numpy-ns")
	artifact := &database.ArtifactMetadata{
		RegistryID:   regID,
		ArtifactType: "pypi",
		Namespace:    "",
		ArtifactName: pkgName,
		Version:      "1.23.0",
		Size:         2048,
		Tags:         []string{"1.23.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact(regID, "", pkgName, "1.23.0") })

	results, err := db.ListArtifacts(regID, database.ListOptions{ArtifactType: "pypi"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, pkgName, results[0].ArtifactName)
}

// TestNamespaceFiltering verifies ListArtifacts' Namespace filter, even
// though PyPI artifacts are conventionally saved with an empty namespace.
func TestNamespaceFiltering(t *testing.T) {
	db := connectTestDB(t)

	regID := uniqueID("ns-filter-reg")
	seedRegistry(t, db, regID, false, false)

	pkgName := uniqueID("storage-ns")
	artifact := &database.ArtifactMetadata{
		RegistryID:   regID,
		ArtifactType: "pypi",
		Namespace:    "google/cloud",
		ArtifactName: pkgName,
		Version:      "1.0.0",
		Size:         1024,
		Tags:         []string{"1.0.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact(regID, "google/cloud", pkgName, "1.0.0") })

	results, err := db.ListArtifacts(regID, database.ListOptions{
		Namespace:    "google/cloud",
		ArtifactType: "pypi",
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "google/cloud", results[0].Namespace)
}
