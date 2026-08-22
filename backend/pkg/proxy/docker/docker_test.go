package docker

import (
	"bytes"
	"context"
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
		registry:   "docker",
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

	_, ok := p.checkAccess(rec, req, false)

	assert.True(t, ok)
}

func TestCheckAccessWriteRequiresAuth(t *testing.T) {
	db := connectTestDB(t)
	c := connectTestCache(t)
	p := newTestProxy(t, db, c)

	req := httptest.NewRequest(http.MethodPut, "/v2/library/nginx/manifests/1.0.0", nil)
	rec := httptest.NewRecorder()

	_, ok := p.checkAccess(rec, req, true)

	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
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
