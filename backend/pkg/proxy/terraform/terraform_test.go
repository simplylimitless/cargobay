package terraform

import (
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *TerraformProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &TerraformProxy{db: db, storage: adapter, cache: c}
}

func seedRegistry(t *testing.T, db *database.Database, id string, private, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       id,
		Name:     id,
		URL:      "https://upstream.example.com",
		Type:     "terraform",
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
// suites left as the DB-wide default for artifactType "terraform".
func seedRegistryWithHost(t *testing.T, db *database.Database, host, upstreamURL string, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       uniqueID("terraform-host-reg"),
		Name:     "terraform-host-reg",
		URL:      upstreamURL,
		Type:     "terraform",
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

func TestNewTerraformProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	registries := []database.RegistryConfig{{ID: "terraform", Name: "Terraform Registry", Type: "terraform", Proxy: true, Enabled: true}}

	router := NewTerraformProxy(db, nil, c, rbac.New(db), registries)
	assert.NotNil(t, router)
	assert.IsType(t, chi.NewRouter(), router)
}

func TestHandleServiceDiscovery(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/terraform.json", nil)
	rec := httptest.NewRecorder()

	p.handleServiceDiscovery(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "/terraform/providers/v1/", body["providers.v1"])
	assert.Equal(t, "/terraform/modules/v1/", body["modules.v1"])
}

// TestServiceDiscoveryUnauthenticatedOnPrivateRegistry verifies the
// documented behavior that .well-known/terraform.json sits outside the
// RequireReadAccess middleware group, so it's reachable even against a
// private, Host-bound registry with no authenticated user.
func TestServiceDiscoveryUnauthenticatedOnPrivateRegistry(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("private-tf-discovery-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("private-tf-discovery-reg"),
		Name:     "private-tf-discovery-reg",
		Type:     "terraform",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := NewTerraformProxy(db, nil, c, rbac.New(db), nil)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/terraform.json", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTerraformProxyRoutesPrivateRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	host := uniqueID("private-tf-host") + ".test"
	reg := &database.RegistryConfig{
		ID:       uniqueID("private-tf-reg"),
		Name:     "private-tf-reg",
		Type:     "terraform",
		Enabled:  true,
		Priority: 100,
		Private:  true,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := NewTerraformProxy(db, nil, c, rbac.New(db), nil)

	req := httptest.NewRequest(http.MethodGet, "/providers/v1/hashicorp/aws/versions", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestHandleProviderVersions covers listing provider versions recorded in
// the database. Called directly (no context target set), TargetFromContext
// falls back to Target{Label: "terraform"}, so the artifact must be seeded
// under registry ID "terraform".
func TestHandleProviderVersions(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	namespace := uniqueID("hashicorp")
	ptype := "aws"
	artifact := &database.ArtifactMetadata{
		RegistryID:   "terraform",
		ArtifactType: "terraform",
		Namespace:    namespace,
		ArtifactName: ptype,
		Version:      "5.0.0",
		Tags:         []string{"5.0.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("terraform", namespace, ptype, "5.0.0") })

	req := httptest.NewRequest(http.MethodGet, "/providers/v1/"+namespace+"/"+ptype+"/versions", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("ptype", ptype)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleProviderVersions(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	versions, ok := body["versions"].([]interface{})
	require.True(t, ok)
	require.Len(t, versions, 1)
	v := versions[0].(map[string]interface{})
	assert.Equal(t, "5.0.0", v["version"])
}

func TestHandleProviderVersionsEmpty(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	namespace := uniqueID("nobody")
	req := httptest.NewRequest(http.MethodGet, "/providers/v1/"+namespace+"/aws/versions", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("ptype", "aws")
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleProviderVersions(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Nil(t, body["versions"])
}

// TestHandleProviderDownload verifies handleProviderDownload constructs a
// download_url that names the exact provider file it produces, so the
// address a client follows always resolves back to handleProviderFile.
func TestHandleProviderDownload(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	namespace := "hashicorp"
	ptype := "aws"
	version := "5.0.0"
	osName := "linux"
	arch := "amd64"

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/providers/v1/%s/%s/%s/download/%s/%s", namespace, ptype, version, osName, arch), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("ptype", ptype)
	rctx.URLParams.Add("version", version)
	rctx.URLParams.Add("os", osName)
	rctx.URLParams.Add("arch", arch)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleProviderDownload(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	expectedFileName := fmt.Sprintf("terraform-provider-%s_%s_%s_%s.zip", ptype, version, osName, arch)
	assert.Equal(t, expectedFileName, body["filename"])
	expectedURL := fmt.Sprintf("http://example.com/providers/v1/%s/%s/%s/files/%s", namespace, ptype, version, expectedFileName)
	assert.Equal(t, expectedURL, body["download_url"])
	assert.Equal(t, osName, body["os"])
	assert.Equal(t, arch, body["arch"])
}

// TestHandleProviderFileFromOldBufferedStorage proves a provider .zip
// already saved to disk via the old buffered storage.SaveArtifact call (as
// any provider cached before this streaming refactor would have been) is
// still served correctly now that handleProviderFile reads it back via
// GetArtifactStream.
func TestHandleProviderFileFromOldBufferedStorage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	namespace := uniqueID("hashicorp")
	ptype := "aws"
	version := "5.0.0"
	fileName := fmt.Sprintf("terraform-provider-%s_%s_linux_amd64.zip", ptype, version)
	data := []byte("fake provider zip content")
	_, err := p.storage.SaveArtifact("terraform", namespace, ptype, version, data)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/providers/v1/%s/%s/%s/files/%s", namespace, ptype, version, fileName), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("ptype", ptype)
	rctx.URLParams.Add("version", version)
	rctx.URLParams.Add("fileName", fileName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleProviderFile(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Equal(t, data, rec.Body.Bytes())
}

func TestHandleProviderFileNoUpstreamConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	namespace := uniqueID("missing")
	ptype := "aws"
	version := "1.0.0"
	fileName := fmt.Sprintf("terraform-provider-%s_%s_linux_amd64.zip", ptype, version)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/providers/v1/%s/%s/%s/files/%s", namespace, ptype, version, fileName), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("ptype", ptype)
	rctx.URLParams.Add("version", version)
	rctx.URLParams.Add("fileName", fileName)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleProviderFile(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleProviderFileCacheMissFetchesUpstream drives the router end to
// end, fetches the provider zip from a fake upstream on a genuine cache
// miss, and verifies a second request is served from the now-cached copy
// even after the fake upstream is shut down.
func TestHandleProviderFileCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	namespace := uniqueID("hashicorp")
	ptype := "aws"
	version := "5.0.0"
	fileName := fmt.Sprintf("terraform-provider-%s_%s_linux_amd64.zip", ptype, version)
	pkgData := []byte("upstream provider zip bytes")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, fmt.Sprintf("/providers/v1/%s/%s/%s/files/%s", namespace, ptype, version, fileName), r.URL.Path)
		w.Write(pkgData)
	}))
	defer upstream.Close()

	host := uniqueID("tf-provider-host") + ".test"
	seedRegistryWithHost(t, db, host, upstream.URL, true)

	p := newTestProxy(t, db, c)
	router := NewTerraformProxy(p.db, p.storage, p.cache, rbac.New(db), nil)
	server := httptest.NewServer(router)
	defer server.Close()

	get := func() *http.Response {
		req, err := http.NewRequest(http.MethodGet, server.URL+fmt.Sprintf("/providers/v1/%s/%s/%s/files/%s", namespace, ptype, version, fileName), nil)
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

// TestHandleModuleVersions covers listing module versions recorded in the
// database under the "terraform-module" artifact type and a
// "namespace/system" composite namespace.
func TestHandleModuleVersions(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	namespace := uniqueID("hashicorp")
	name := "consul"
	system := "aws"
	moduleNamespace := namespace + "/" + system
	artifact := &database.ArtifactMetadata{
		RegistryID:   "terraform",
		ArtifactType: "terraform-module",
		Namespace:    moduleNamespace,
		ArtifactName: name,
		Version:      "0.1.0",
		Tags:         []string{"0.1.0"},
		Metadata:     map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact("terraform", moduleNamespace, name, "0.1.0") })

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/modules/v1/%s/%s/%s/versions", namespace, name, system), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("system", system)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleModuleVersions(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	modules, ok := body["modules"].([]interface{})
	require.True(t, ok)
	require.Len(t, modules, 1)
	mod := modules[0].(map[string]interface{})
	versions, ok := mod["versions"].([]interface{})
	require.True(t, ok)
	require.Len(t, versions, 1)
	v := versions[0].(map[string]interface{})
	assert.Equal(t, "0.1.0", v["version"])
}

// TestHandleModuleDownloadRedirect verifies the 204 + X-Terraform-Get
// response names our own cached-archive endpoint for the exact
// namespace/name/system/version requested.
func TestHandleModuleDownloadRedirect(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	namespace := "hashicorp"
	name := "consul"
	system := "aws"
	version := "0.1.0"

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/modules/v1/%s/%s/%s/%s/download", namespace, name, system, version), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("system", system)
	rctx.URLParams.Add("version", version)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleModuleDownloadRedirect(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	expected := fmt.Sprintf("http://example.com/modules/v1/%s/%s/%s/%s/archive.tar.gz", namespace, name, system, version)
	assert.Equal(t, expected, rec.Header().Get("X-Terraform-Get"))
}

// TestHandleModuleArchiveFromOldBufferedStorage proves a module archive
// already saved to disk via the old buffered storage.SaveArtifact call (as
// any archive cached before this streaming refactor would have been) is
// still served correctly now that handleModuleArchive reads it back via
// GetArtifactStream.
func TestHandleModuleArchiveFromOldBufferedStorage(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	namespace := uniqueID("hashicorp")
	name := "consul"
	system := "aws"
	version := "0.1.0"
	moduleNamespace := namespace + "/" + system
	archiveData := []byte("fake module archive content")
	_, err := p.storage.SaveArtifact("terraform", moduleNamespace, name, version, archiveData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/modules/v1/%s/%s/%s/%s/archive.tar.gz", namespace, name, system, version), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("system", system)
	rctx.URLParams.Add("version", version)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleModuleArchive(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/gzip", rec.Header().Get("Content-Type"))
	assert.Equal(t, archiveData, rec.Body.Bytes())
}

func TestHandleModuleArchiveNoUpstreamConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	namespace := uniqueID("missing")
	name := "consul"
	system := "aws"
	version := "0.1.0"

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/modules/v1/%s/%s/%s/%s/archive.tar.gz", namespace, name, system, version), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("namespace", namespace)
	rctx.URLParams.Add("name", name)
	rctx.URLParams.Add("system", system)
	rctx.URLParams.Add("version", version)
	req = withChiRouteContext(req, rctx)
	rec := httptest.NewRecorder()

	p.handleModuleArchive(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

// TestHandleModuleArchiveCacheMissFetchesUpstream drives the router end to
// end, fetches the module archive from a fake upstream on a genuine cache
// miss, and verifies a second request is served from the now-cached copy
// even after the fake upstream is shut down.
func TestHandleModuleArchiveCacheMissFetchesUpstream(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)

	namespace := uniqueID("hashicorp")
	name := "consul"
	system := "aws"
	version := "0.1.0"
	archiveData := []byte("upstream module archive bytes")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, fmt.Sprintf("/modules/v1/%s/%s/%s/%s/archive.tar.gz", namespace, name, system, version), r.URL.Path)
		w.Write(archiveData)
	}))
	defer upstream.Close()

	host := uniqueID("tf-module-host") + ".test"
	seedRegistryWithHost(t, db, host, upstream.URL, true)

	p := newTestProxy(t, db, c)
	router := NewTerraformProxy(p.db, p.storage, p.cache, rbac.New(db), nil)
	server := httptest.NewServer(router)
	defer server.Close()

	get := func() *http.Response {
		req, err := http.NewRequest(http.MethodGet, server.URL+fmt.Sprintf("/modules/v1/%s/%s/%s/%s/archive.tar.gz", namespace, name, system, version), nil)
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
	assert.Equal(t, archiveData, body)

	upstream.Close()
	resp2 := get()
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	assert.Equal(t, archiveData, body2)
}

func TestFetchFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	data, err := p.fetchFromUpstream(&database.RegistryConfig{Proxy: false}, "modules/v1/hashicorp/consul/aws/versions")
	assert.Error(t, err)
	assert.Nil(t, data)

	data, err = p.fetchFromUpstream(nil, "modules/v1/hashicorp/consul/aws/versions")
	assert.Error(t, err)
	assert.Nil(t, data)
}

func TestFetchFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte(`{"versions":[]}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/modules/v1/hashicorp/consul/aws/versions", r.URL.Path)
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{URL: upstream.URL, Proxy: true}
	data, err := p.fetchFromUpstream(reg, "modules/v1/hashicorp/consul/aws/versions")
	require.NoError(t, err)
	assert.Equal(t, body, data)
}

func TestFetchStreamFromUpstreamNoProxyConfigured(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	resp, err := p.fetchStreamFromUpstream(&database.RegistryConfig{Proxy: false}, "providers/v1/hashicorp/aws/5.0.0/files/x.zip")
	assert.Error(t, err)
	assert.Nil(t, resp)

	resp, err = p.fetchStreamFromUpstream(nil, "providers/v1/hashicorp/aws/5.0.0/files/x.zip")
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestFetchStreamFromUpstreamSuccess(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	body := []byte("zip contents")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer upstream.Close()

	reg := &database.RegistryConfig{URL: upstream.URL, Proxy: true}
	resp, err := p.fetchStreamFromUpstream(reg, "providers/v1/hashicorp/aws/5.0.0/files/x.zip")
	require.NoError(t, err)
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestSeedRegistryHelper(t *testing.T) {
	db := connectTestDB(t)
	reg := seedRegistry(t, db, uniqueID("terraform-seed-reg"), false, true)
	assert.True(t, reg.Proxy)
	assert.Equal(t, "terraform", reg.Type)
}
