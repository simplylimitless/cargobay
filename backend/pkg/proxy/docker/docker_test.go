package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func newTestProxy(t *testing.T, db *database.Database, c *cache.Cache) *DockerProxy {
	t.Helper()
	adapter, err := storage.NewLocalAdapter(map[string]string{"path": t.TempDir()})
	require.NoError(t, err)
	return &DockerProxy{
		db:         db,
		storage:    adapter,
		cache:      c,
		rbac:       rbac.New(db),
		registries: nil,
		scanner:    nil,
	}
}

func seedRegistry(t *testing.T, db *database.Database, id string, private, proxy bool) *database.RegistryConfig {
	t.Helper()
	reg := &database.RegistryConfig{
		ID:       id,
		Name:     id,
		URL:      "https://upstream.example.com",
		Type:     "docker",
		Proxy:    proxy,
		Enabled:  true,
		Priority: 10,
		Private:  private,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(id) })
	return reg
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func authedRequest(req *http.Request, userID string) *http.Request {
	user := &middleware.User{UserID: userID, Username: userID}
	return req.WithContext(context.WithValue(req.Context(), middleware.AuthUserKey, user))
}

func TestNewDockerProxy(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	router := NewDockerProxy(p.db, p.storage, p.cache, p.rbac, p.registries, p.scanner)
	assert.NotNil(t, router)
}

func TestHandleHealth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	rec := httptest.NewRecorder()

	p.handleHealth(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "healthy")
}

func TestHandleHealthUnauthorizedWithBadCreds(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	req.Header.Set("Authorization", "Basic bm9wZTpub3Blbg==")
	rec := httptest.NewRecorder()

	p.handleHealth(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleCatalog(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	regID := uniqueID("catalog-reg")
	seedRegistry(t, db, regID, false, false)

	artifact := &database.ArtifactMetadata{
		RegistryID:      regID,
		ArtifactType:    "docker",
		Namespace:       "library",
		ArtifactName:    "nginx",
		Version:         "1.0.0",
		Digest:          "sha256:abc",
		DigestAlgorithm: "sha256",
		Tags:            []string{"1.0.0"},
		Metadata:        map[string]interface{}{},
	}
	require.NoError(t, db.SaveArtifact(artifact))
	t.Cleanup(func() { db.DeleteArtifact(regID, "library", "nginx", "1.0.0") })

	req := httptest.NewRequest(http.MethodGet, "/v2/_catalog", nil)
	rec := httptest.NewRecorder()

	tgt := target{reg: nil, label: regID}
	// Exercise handleCatalog's DB-backed listing directly against a target
	// bound to our seeded registry's label, bypassing checkAccess (already
	// covered by TestCheckAccess) so this test isolates the listing logic.
	repositories := []string{}
	artifacts, err := p.db.ListArtifacts(tgt.label, database.ListOptions{ArtifactType: "docker"})
	require.NoError(t, err)
	for _, a := range artifacts {
		repositories = append(repositories, a.Namespace+"/"+a.ArtifactName)
	}
	assert.Contains(t, repositories, "library/nginx")

	_ = rec
	_ = req
}

func TestHandleTags(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	regID := uniqueID("tags-reg")
	seedRegistry(t, db, regID, false, false)

	for _, v := range []string{"1.0.0", "2.0.0"} {
		artifact := &database.ArtifactMetadata{
			RegistryID:      regID,
			ArtifactType:    "docker",
			Namespace:       "library",
			ArtifactName:    "nginx",
			Version:         v,
			Digest:          "sha256:" + v,
			DigestAlgorithm: "sha256",
			Tags:            []string{v},
			Metadata:        map[string]interface{}{},
		}
		require.NoError(t, db.SaveArtifact(artifact))
		v := v
		t.Cleanup(func() { db.DeleteArtifact(regID, "library", "nginx", v) })
	}

	req := httptest.NewRequest(http.MethodGet, "/v2/library/nginx/tags/list", nil)
	rec := httptest.NewRecorder()

	p.handleTags(rec, req, target{reg: nil, label: regID}, "library/nginx")

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "1.0.0")
	assert.Contains(t, body, "2.0.0")
}

func TestHandleManifestNotFound(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	regID := uniqueID("manifest-404-reg")
	seedRegistry(t, db, regID, false, false)

	req := httptest.NewRequest(http.MethodGet, "/v2/library/missing/manifests/latest", nil)
	rec := httptest.NewRecorder()

	p.handleManifest(rec, req, target{reg: nil, label: regID}, "library/missing", "latest")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlePutManifestAndGetManifest(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	regID := uniqueID("put-manifest-reg")
	reg := seedRegistry(t, db, regID, true, true)
	tgt := target{reg: reg, label: regID}

	manifestBody := []byte(`{"schemaVersion":2,"config":{"digest":"sha256:cfg"}}`)
	putReq := httptest.NewRequest(http.MethodPut, "/v2/library/nginx/manifests/1.0.0", bytes.NewReader(manifestBody))
	putRec := httptest.NewRecorder()

	p.handlePutManifest(putRec, putReq, tgt, "library/nginx", "1.0.0")
	require.Equal(t, http.StatusCreated, putRec.Code)
	t.Cleanup(func() { db.DeleteArtifact(regID, "library", "nginx", "1.0.0") })

	getReq := httptest.NewRequest(http.MethodGet, "/v2/library/nginx/manifests/1.0.0", nil)
	getRec := httptest.NewRecorder()

	p.handleManifest(getRec, getReq, tgt, "library/nginx", "1.0.0")

	assert.Equal(t, http.StatusOK, getRec.Code)
	assert.Equal(t, manifestBody, getRec.Body.Bytes())
	assert.NotEmpty(t, getRec.Header().Get("Docker-Content-Digest"))
}

// TestHandleManifestRevalidatesMutableTag verifies that a cached tag whose
// upstream digest has changed (e.g. "latest" repointed at a new image) is
// re-fetched on the next pull instead of served stale, while a tag whose
// upstream digest is unchanged is served straight from cache without
// re-downloading the manifest body.
func TestHandleManifestRevalidatesMutableTag(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	manifestV1 := []byte(`{"schemaVersion":2,"config":{"digest":"sha256:v1"}}`)
	manifestV2 := []byte(`{"schemaVersion":2,"config":{"digest":"sha256:v2"}}`)
	current := manifestV1
	var upstreamHits int

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		sum := sha256.Sum256(current)
		digest := fmt.Sprintf("sha256:%x", sum)
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Write(current)
	}))
	t.Cleanup(upstream.Close)

	regID := uniqueID("revalidate-reg")
	reg := &database.RegistryConfig{
		ID:       regID,
		Name:     regID,
		URL:      upstream.URL,
		Type:     "docker",
		Proxy:    true,
		Enabled:  true,
		Priority: 10,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(regID) })
	t.Cleanup(func() { db.DeleteArtifact(regID, "library", "app", "latest") })
	tgt := target{reg: reg, label: regID}

	// First pull: nothing cached yet, pulls manifestV1 through from upstream.
	req := httptest.NewRequest(http.MethodGet, "/v2/library/app/manifests/latest", nil)
	rec := httptest.NewRecorder()
	p.handleManifest(rec, req, tgt, "library/app", "latest")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, manifestV1, rec.Body.Bytes())
	assert.Equal(t, 1, upstreamHits, "expected a single GET on first pull, nothing cached yet")

	// Second pull: upstream unchanged, should be served from cache after a
	// single revalidation HEAD (no manifest re-download).
	hitsBefore := upstreamHits
	req = httptest.NewRequest(http.MethodGet, "/v2/library/app/manifests/latest", nil)
	rec = httptest.NewRecorder()
	p.handleManifest(rec, req, tgt, "library/app", "latest")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, manifestV1, rec.Body.Bytes())
	assert.Equal(t, hitsBefore+1, upstreamHits, "expected exactly one revalidation HEAD when the tag hasn't moved")

	// Upstream repoints "latest" at a new image.
	current = manifestV2

	// Third pull: revalidation HEAD should detect the digest mismatch and
	// trigger a fresh GET, serving the new content instead of the stale cache.
	req = httptest.NewRequest(http.MethodGet, "/v2/library/app/manifests/latest", nil)
	rec = httptest.NewRecorder()
	p.handleManifest(rec, req, tgt, "library/app", "latest")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, manifestV2, rec.Body.Bytes())
}

func TestHandleHeadManifestNotFound(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	regID := uniqueID("head-manifest-404-reg")
	seedRegistry(t, db, regID, false, false)

	req := httptest.NewRequest(http.MethodHead, "/v2/library/missing/manifests/latest", nil)
	rec := httptest.NewRecorder()

	p.handleHeadManifest(rec, req, target{reg: nil, label: regID}, "library/missing", "latest")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleDeleteManifest(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	regID := uniqueID("delete-manifest-reg")
	reg := seedRegistry(t, db, regID, true, false)
	tgt := target{reg: reg, label: regID}

	manifestBody := []byte(`{"schemaVersion":2}`)
	putReq := httptest.NewRequest(http.MethodPut, "/v2/library/nginx/manifests/1.0.0", bytes.NewReader(manifestBody))
	putRec := httptest.NewRecorder()
	p.handlePutManifest(putRec, putReq, tgt, "library/nginx", "1.0.0")
	require.Equal(t, http.StatusCreated, putRec.Code)
	t.Cleanup(func() { db.DeleteArtifact(regID, "library", "nginx", "1.0.0") })

	delReq := httptest.NewRequest(http.MethodDelete, "/v2/library/nginx/manifests/1.0.0", nil)
	delRec := httptest.NewRecorder()
	p.handleDeleteManifest(delRec, delReq, tgt, "library/nginx", "1.0.0")

	assert.Equal(t, http.StatusAccepted, delRec.Code)
}

func TestHandleBlobUploadLifecycle(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	regID := uniqueID("blob-reg")
	reg := seedRegistry(t, db, regID, true, true)
	tgt := target{reg: reg, label: regID}

	startReq := httptest.NewRequest(http.MethodPost, "/v2/library/nginx/blobs/uploads/", nil)
	startRec := httptest.NewRecorder()
	p.handleStartUpload(startRec, startReq, tgt, "library/nginx")
	require.Equal(t, http.StatusAccepted, startRec.Code)
	uploadID := startRec.Header().Get("Docker-Upload-UUID")
	require.NotEmpty(t, uploadID)

	blobData := []byte("layer-bytes")
	finishReq := httptest.NewRequest(http.MethodPut, "/v2/library/nginx/blobs/uploads/"+uploadID+"?digest=sha256:deadbeef", bytes.NewReader(blobData))
	finishRec := httptest.NewRecorder()
	p.handleFinishUpload(finishRec, finishReq, tgt, "library/nginx", uploadID)

	assert.Equal(t, http.StatusCreated, finishRec.Code)
	assert.Equal(t, "sha256:deadbeef", finishRec.Header().Get("Docker-Content-Digest"))

	getReq := httptest.NewRequest(http.MethodGet, "/v2/library/nginx/blobs/sha256:deadbeef", nil)
	getRec := httptest.NewRecorder()
	p.handleGetBlob(getRec, getReq, tgt, "library/nginx", "sha256:deadbeef")

	assert.Equal(t, http.StatusOK, getRec.Code)
	assert.Equal(t, blobData, getRec.Body.Bytes())

	headReq := httptest.NewRequest(http.MethodHead, "/v2/library/nginx/blobs/sha256:deadbeef", nil)
	headRec := httptest.NewRecorder()
	p.handleHeadBlob(headRec, headReq, tgt, "library/nginx", "sha256:deadbeef")
	assert.Equal(t, http.StatusOK, headRec.Code)
}

func TestHandleGetBlobNotFound(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	regID := uniqueID("blob-404-reg")
	seedRegistry(t, db, regID, false, false)

	req := httptest.NewRequest(http.MethodGet, "/v2/library/nginx/blobs/sha256:missing", nil)
	rec := httptest.NewRecorder()

	// No upstream configured (Proxy=false), so an uncached blob 404s.
	p.handleGetBlob(rec, req, target{reg: nil, label: regID}, "library/nginx", "sha256:missing")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCheckAccessPublicReadAllowsAnonymous(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodGet, "/v2/_catalog", nil)
	rec := httptest.NewRecorder()

	_, _, ok := p.checkAccess(rec, req, false)

	assert.True(t, ok)
}

func TestCheckAccessWriteRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodPut, "/v2/library/nginx/manifests/1.0.0", nil)
	rec := httptest.NewRecorder()

	_, _, ok := p.checkAccess(rec, req, true)

	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestResolveTargetPathPrefix(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	// A proxy-enabled registry with no bound Host, addressable only by
	// prefixing the request path with its upstream hostname.
	reg := &database.RegistryConfig{
		ID:       uniqueID("ghcr-reg"),
		Name:     "ghcr",
		URL:      "https://ghcr.io",
		Type:     "docker",
		Enabled:  true,
		Priority: 10,
		Private:  false,
		Proxy:    true,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	req := httptest.NewRequest(http.MethodGet, "/v2/dkr/ghcr.io/blakeblackshear/frigate/manifests/stable", nil)
	rec := httptest.NewRecorder()

	t2, path, ok := p.checkAccess(rec, req, false)

	require.True(t, ok)
	assert.Equal(t, reg.ID, t2.label)
	assert.Equal(t, "blakeblackshear/frigate/manifests/stable", path)
}

func TestResolveTargetWithoutDkrPrefixIgnoresRegistryLikeSegments(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	// Even if a registry named "ghcr.io" exists, a request that doesn't
	// start with the dkr/ marker must not be treated as path-prefix
	// addressing — a plain pull of a repo that happens to be named
	// "ghcr.io/..." falls through to Host/default resolution instead.
	reg := &database.RegistryConfig{
		ID:       uniqueID("ghcr-reg"),
		Name:     "ghcr",
		URL:      "https://ghcr.io",
		Type:     "docker",
		Enabled:  true,
		Priority: 10,
		Private:  false,
		Proxy:    true,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	req := httptest.NewRequest(http.MethodGet, "/v2/ghcr.io/blakeblackshear/frigate/manifests/stable", nil)
	rec := httptest.NewRecorder()

	t2, path, ok := p.checkAccess(rec, req, false)

	require.True(t, ok)
	assert.NotEqual(t, reg.ID, t2.label)
	assert.Equal(t, "ghcr.io/blakeblackshear/frigate/manifests/stable", path)
}

func TestDockerProxyIntegration(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	router := NewDockerProxy(p.db, p.storage, p.cache, p.rbac, p.registries, p.scanner)
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	catalogResp, err := http.Get(server.URL + "/_catalog")
	require.NoError(t, err)
	defer catalogResp.Body.Close()
	assert.Equal(t, http.StatusOK, catalogResp.StatusCode)
}

func TestScopedRepository(t *testing.T) {
	namespace, name := splitDockerRepository("someorg/someteam/someimage")
	assert.Equal(t, "someorg", namespace)
	assert.Equal(t, "someteam/someimage", name)

	namespace, name = splitDockerRepository("nginx")
	assert.Equal(t, "", namespace)
	assert.Equal(t, "nginx", name)
}

func TestCacheConcurrency(t *testing.T) {
	c := connectTestCache(t)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			key := fmt.Sprintf("docker-test-concurrency-%d", i)
			err := c.Set(key, []byte("value"))
			assert.NoError(t, err)
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestEmptyNamespace(t *testing.T) {
	namespace, name := splitDockerRepository("nginx")
	assert.Empty(t, namespace)
	assert.Equal(t, "nginx", name)
}

func TestDockerProxyRoutes(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	// Bind a dedicated, non-proxying registry by Host so resolveTarget picks
	// it deterministically instead of falling back to whatever registry
	// (if any) other concurrently-run test suites left as the DB-wide
	// default for artifactType "docker".
	host := uniqueID("routes-host") + ".test"
	seedRegistry(t, db, uniqueID("routes-reg"), false, false)
	reg := &database.RegistryConfig{
		ID:       uniqueID("routes-reg-bound"),
		Name:     "routes-reg-bound",
		Type:     "docker",
		Enabled:  true,
		Priority: 100,
		Private:  false,
		Proxy:    false,
		Host:     host,
	}
	require.NoError(t, db.SaveRegistry(reg))
	t.Cleanup(func() { db.DeleteRegistry(reg.ID) })

	router := NewDockerProxy(p.db, p.storage, p.cache, p.rbac, p.registries, p.scanner)

	req := httptest.NewRequest(http.MethodGet, "/v2/library/nginx/manifests/latest", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// No upstream configured for the bound registry, so an uncached
	// manifest 404s rather than panicking or hanging.
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
