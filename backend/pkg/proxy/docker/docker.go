// Package docker implements the Docker Registry v2 API proxy
//
// This package provides a caching proxy for Docker images that:
//   - Implements Docker Registry v2 API
//   - Caches image manifests and layers
//   - Handles multi-arch images (manifest lists)
//   - Supports notary signatures
//
// Docker Registry v2 API: https://docs.docker.com/registry/spec/api/
package docker

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/go-chi/chi/v5"
)

// DockerProxy implements the Docker Registry v2 API proxy
type DockerProxy struct {
	db         *database.Database
	storage    storage.StorageAdapter
	cache      *cache.Cache
	rbac       *rbac.RBAC
	registries []database.RegistryConfig
	registry   string
}

// NewDockerProxy creates a new Docker proxy instance
func NewDockerProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	p := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		rbac:       rbacMgr,
		registries: registries,
		registry:   "docker",
	}

	// Docker Registry v2 API endpoints

	// Discovery endpoint
	r.Get("/", p.handleDiscovery)

	// Health check
	r.Get("/v2/", p.handleHealth)

	// List repositories
	r.Get("/v2/_catalog", p.handleCatalog)

	// Repository names routinely span multiple path segments (e.g.
	// "library/nginx", "someorg/someteam/someimage"), which chi's
	// single-segment {repository} param can't capture. Reads/writes are
	// dispatched through a wildcard and the repository/reference/digest are
	// parsed out of the raw request path instead.
	r.Get("/v2/*", p.handleV2Read)
	r.Head("/v2/*", p.handleV2Head)

	// Write operations (push/delete) require an authenticated user — anonymous
	// requests may pull, but must not be able to upload or remove artifacts.
	// Beyond that, CheckAccess (called from each dispatcher) enforces that
	// only a registry the caller may publish to accepts the write at all.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)

		r.Put("/v2/*", p.handleV2Put)
		r.Post("/v2/*", p.handleV2Post)
		r.Patch("/v2/*", p.handleV2Patch)
		r.Delete("/v2/*", p.handleV2Delete)
	})

	return r
}

// splitAtLast splits path at the last occurrence of sep, e.g.
// splitAtLast("library/nginx/manifests/latest", "/manifests/") returns
// ("library/nginx", "latest", true). Repository names never contain the
// literal API segment they're split on, so the last occurrence is always
// the correct boundary.
func splitAtLast(path, sep string) (before, after string, ok bool) {
	idx := strings.LastIndex(path, sep)
	if idx < 0 {
		return "", "", false
	}
	return path[:idx], path[idx+len(sep):], true
}

func trimSuffixSep(path, suffix string) (before string, ok bool) {
	if !strings.HasSuffix(path, suffix) {
		return "", false
	}
	return strings.TrimSuffix(path, suffix), true
}

// target bundles the registry resolved for a request (by Host header, or
// the default public proxy) with the label used to namespace its stored
// artifacts.
type target struct {
	reg   *database.RegistryConfig
	label string
}

// resolveTarget picks which registry a request addresses, purely from the
// Host header the client connected on — repository names are never
// touched, so downstream docker clients need zero reconfiguration beyond
// pointing at the right hostname for a private registry.
func (p *DockerProxy) resolveTarget(r *http.Request) target {
	reg := proxy.ResolveRegistry(p.db, p.registries, r.Host, "docker")
	label := "docker"
	if reg != nil {
		label = reg.ID
	}
	return target{reg: reg, label: label}
}

// checkAccess resolves the target registry for r and enforces read/publish
// access on it, writing a 401/403 and returning ok=false if denied.
func (p *DockerProxy) checkAccess(w http.ResponseWriter, r *http.Request, requirePublish bool) (t target, ok bool) {
	t = p.resolveTarget(r)
	if !proxy.CheckAccess(w, p.rbac, middleware.GetUser(r), t.reg, requirePublish) {
		return t, false
	}
	return t, true
}

// handleV2Read dispatches GET requests under /v2/ to the manifest, tag
// list, or blob handler based on the request path shape.
func (p *DockerProxy) handleV2Read(w http.ResponseWriter, r *http.Request) {
	t, ok := p.checkAccess(w, r, false)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v2/")

	if repo, ref, ok := splitAtLast(path, "/manifests/"); ok {
		p.handleManifest(w, r, t, repo, ref)
		return
	}
	if repo, ok := trimSuffixSep(path, "/tags/list"); ok {
		p.handleTags(w, r, t, repo)
		return
	}
	if repo, digest, ok := splitAtLast(path, "/blobs/"); ok {
		p.handleGetBlob(w, r, t, repo, digest)
		return
	}
	http.NotFound(w, r)
}

func (p *DockerProxy) handleV2Head(w http.ResponseWriter, r *http.Request) {
	t, ok := p.checkAccess(w, r, false)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v2/")

	if repo, digest, ok := splitAtLast(path, "/blobs/"); ok {
		p.handleHeadBlob(w, r, t, repo, digest)
		return
	}
	http.NotFound(w, r)
}

func (p *DockerProxy) handleV2Put(w http.ResponseWriter, r *http.Request) {
	t, ok := p.checkAccess(w, r, true)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v2/")

	if repo, uploadID, ok := splitAtLast(path, "/blobs/uploads/"); ok {
		p.handleFinishUpload(w, r, t, repo, uploadID)
		return
	}
	if repo, ref, ok := splitAtLast(path, "/manifests/"); ok {
		p.handlePutManifest(w, r, t, repo, ref)
		return
	}
	http.NotFound(w, r)
}

func (p *DockerProxy) handleV2Post(w http.ResponseWriter, r *http.Request) {
	t, ok := p.checkAccess(w, r, true)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v2/")

	if repo, ok := trimSuffixSep(path, "/blobs/uploads/"); ok {
		p.handleStartUpload(w, r, t, repo)
		return
	}
	if repo, ok := trimSuffixSep(path, "/blobs/uploads"); ok {
		p.handleStartUpload(w, r, t, repo)
		return
	}
	http.NotFound(w, r)
}

func (p *DockerProxy) handleV2Patch(w http.ResponseWriter, r *http.Request) {
	t, ok := p.checkAccess(w, r, true)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v2/")

	if repo, uploadID, ok := splitAtLast(path, "/blobs/uploads/"); ok {
		p.handlePatchUpload(w, r, t, repo, uploadID)
		return
	}
	http.NotFound(w, r)
}

func (p *DockerProxy) handleV2Delete(w http.ResponseWriter, r *http.Request) {
	t, ok := p.checkAccess(w, r, true)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v2/")

	if repo, ref, ok := splitAtLast(path, "/manifests/"); ok {
		p.handleDeleteManifest(w, r, t, repo, ref)
		return
	}
	http.NotFound(w, r)
}

// handleDiscovery handles the root discovery endpoint
func (p *DockerProxy) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Welcome to cargobay Docker Registry Proxy",
	})
}

// handleCatalog lists repositories in the registry resolved for this
// request (by Host header) — a caller only ever sees repositories in the
// registry they're actually talking to.
func (p *DockerProxy) handleCatalog(w http.ResponseWriter, r *http.Request) {
	t, ok := p.checkAccess(w, r, false)
	if !ok {
		return
	}

	repositories := []string{}

	artifacts, err := p.db.ListArtifacts(t.label, database.ListOptions{
		ArtifactType: "docker",
	})
	if err == nil {
		seen := make(map[string]bool)
		for _, a := range artifacts {
			if !seen[a.ArtifactName] {
				repositories = append(repositories, a.ArtifactName)
				seen[a.ArtifactName] = true
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"repositories": repositories,
	})
}

// handleTags lists tags for a repository
func (p *DockerProxy) handleTags(w http.ResponseWriter, r *http.Request, t target, repository string) {
	// Get tags from database
	artifacts, err := p.db.ListArtifacts(t.label, database.ListOptions{
		ArtifactType: "docker",
		Namespace:    repository,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get tags: %v", err), http.StatusBadGateway)
		return
	}

	tags := make([]string, 0, len(artifacts))
	for _, a := range artifacts {
		tags = append(tags, a.Version)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name": repository,
		"tags": tags,
	})
}

// handleManifest retrieves a manifest, pulling it through from the
// upstream registry and caching it on first request if it isn't already
// stored locally.
func (p *DockerProxy) handleManifest(w http.ResponseWriter, r *http.Request, t target, repository, reference string) {
	// Try to find artifact in database
	artifacts, err := p.db.ListArtifacts(t.label, database.ListOptions{
		ArtifactType: "docker",
		Namespace:    repository,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get manifest: %v", err), http.StatusBadGateway)
		return
	}

	// Find matching artifact
	var artifact *database.ArtifactMetadata
	for _, a := range artifacts {
		if a.Version == reference || a.Digest == reference {
			artifact = &a
			break
		}
	}

	if artifact != nil {
		w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
		w.Header().Set("Docker-Content-Digest", artifact.Digest)
		w.Header().Set("X-Docker-Size", fmt.Sprintf("%d", artifact.Size))
		w.Write(artifact.Metadata["manifest"].([]byte))
		return
	}

	// Not cached — pull the manifest through from the upstream registry, if
	// this registry has one configured.
	body, contentType, err := p.fetchManifestFromUpstream(t.reg, repository, reference)
	if err != nil {
		http.Error(w, "manifest unknown", http.StatusNotFound)
		return
	}

	digestBytes := sha256.Sum256(body)
	digest := fmt.Sprintf("sha256:%x", digestBytes)

	if _, err := p.storage.SaveArtifact("docker", repository, reference, "latest", body); err != nil {
		http.Error(w, fmt.Sprintf("Failed to cache manifest: %v", err), http.StatusInternalServerError)
		return
	}

	cached := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("docker:%s:%s:%s", t.label, repository, reference),
		RegistryID:      t.label,
		ArtifactType:    "docker",
		Namespace:       repository,
		ArtifactName:    repository,
		Version:         reference,
		Digest:          digest,
		DigestAlgorithm: "sha256",
		Size:            int64(len(body)),
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata:        map[string]interface{}{"manifest": body},
		Tags:            []string{reference},
	}
	if err := p.db.SaveArtifact(cached); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	if contentType == "" {
		contentType = "application/vnd.docker.distribution.manifest.v2+json"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("X-Docker-Size", fmt.Sprintf("%d", len(body)))
	w.Write(body)
}

// handlePutManifest puts a manifest
func (p *DockerProxy) handlePutManifest(w http.ResponseWriter, r *http.Request, t target, repository, reference string) {
	// Read manifest body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read manifest: %v", err), http.StatusBadRequest)
		return
	}

	// Parse manifest to get digest
	var manifest map[string]interface{}
	if err := json.Unmarshal(body, &manifest); err != nil {
		http.Error(w, "Invalid manifest", http.StatusBadRequest)
		return
	}

	digestBytes := sha256.Sum256(body)
	digest := fmt.Sprintf("sha256:%x", digestBytes)

	// Save manifest to storage
	if _, err := p.storage.SaveArtifact("docker", repository, reference, "latest", body); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save manifest: %v", err), http.StatusInternalServerError)
		return
	}

	// Save to database
	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("docker:%s:%s:%s", t.label, repository, reference),
		RegistryID:      t.label,
		ArtifactType:    "docker",
		Namespace:       repository,
		ArtifactName:    repository,
		Version:         reference,
		Digest:          digest,
		DigestAlgorithm: "sha256",
		Size:            int64(len(body)),
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata:        map[string]interface{}{"manifest": body},
		Tags:            []string{reference},
	}

	if err := p.db.SaveArtifact(artifact); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

// handleGetBlob retrieves a blob (layer), pulling it through from the
// upstream registry and caching it on first request if it isn't already
// stored locally.
func (p *DockerProxy) handleGetBlob(w http.ResponseWriter, r *http.Request, t target, repository, digest string) {
	blob, err := p.storage.GetArtifact("docker", repository, "blob", digest)
	if err == nil && blob != nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Docker-Content-Digest", digest)
		w.WriteHeader(http.StatusOK)
		w.Write(blob)
		return
	}

	blob, err = p.fetchBlobFromUpstream(t.reg, repository, digest)
	if err != nil {
		http.Error(w, "blob unknown", http.StatusNotFound)
		return
	}

	if _, err := p.storage.SaveArtifact("docker", repository, "blob", digest, blob); err != nil {
		http.Error(w, fmt.Sprintf("Failed to cache blob: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusOK)
	w.Write(blob)
}

// handleHeadBlob checks if a blob exists, pulling it through and caching it
// from upstream if it isn't stored locally yet (docker push probes with
// HEAD before uploading, to skip blobs the registry already has).
func (p *DockerProxy) handleHeadBlob(w http.ResponseWriter, r *http.Request, t target, repository, digest string) {
	exists, err := p.storage.ArtifactExists("docker", repository, "blob", digest)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check blob: %v", err), http.StatusInternalServerError)
		return
	}

	if !exists {
		blob, err := p.fetchBlobFromUpstream(t.reg, repository, digest)
		if err != nil {
			http.Error(w, "blob unknown", http.StatusNotFound)
			return
		}
		if _, err := p.storage.SaveArtifact("docker", repository, "blob", digest, blob); err != nil {
			http.Error(w, fmt.Sprintf("Failed to cache blob: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusOK)
}

// handleStartUpload starts a new blob upload session
func (p *DockerProxy) handleStartUpload(w http.ResponseWriter, r *http.Request, t target, repository string) {
	uploadID := fmt.Sprintf("upload-%d", time.Now().UnixNano())

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repository, uploadID))
	w.Header().Set("Docker-Upload-UUID", uploadID)
	w.WriteHeader(http.StatusAccepted)
}

// handlePatchUpload appends a chunk to an in-progress blob upload
func (p *DockerProxy) handlePatchUpload(w http.ResponseWriter, r *http.Request, t target, repository, uploadID string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read chunk: %v", err), http.StatusBadRequest)
		return
	}

	p.cache.Set(fmt.Sprintf("upload:%s:%s", repository, uploadID), body)

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repository, uploadID))
	w.Header().Set("Docker-Upload-UUID", uploadID)
	w.WriteHeader(http.StatusAccepted)
}

// handleFinishUpload completes a blob upload and stores the blob
func (p *DockerProxy) handleFinishUpload(w http.ResponseWriter, r *http.Request, t target, repository, uploadID string) {
	digest := r.URL.Query().Get("digest")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read blob: %v", err), http.StatusBadRequest)
		return
	}
	if len(body) == 0 {
		var cached []byte
		if err := p.cache.Get(fmt.Sprintf("upload:%s:%s", repository, uploadID), &cached); err == nil {
			body = cached
		}
	}

	if digest == "" {
		digest = fmt.Sprintf("sha256:%x", body)
	}

	if _, err := p.storage.SaveArtifact("docker", repository, "blob", digest, body); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save blob: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

// handleDeleteManifest deletes a manifest
func (p *DockerProxy) handleDeleteManifest(w http.ResponseWriter, r *http.Request, t target, repository, reference string) {
	if err := p.storage.DeleteArtifact("docker", repository, reference, "latest"); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete manifest: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// upstreamBase returns the base URL to pull through from for the resolved
// registry: its own configured URL if it proxies an upstream, otherwise
// Docker Hub as the default.
func upstreamBase(reg *database.RegistryConfig) string {
	if reg != nil && reg.Proxy && reg.URL != "" {
		return strings.TrimSuffix(reg.URL, "/")
	}
	return "https://registry-1.docker.io"
}

// normalizeRepository expands unqualified Docker Hub image names (e.g.
// "nginx") to their canonical "library/nginx" form.
func normalizeRepository(repository string) string {
	if !strings.Contains(repository, "/") {
		return "library/" + repository
	}
	return repository
}

// dockerAuthChallenge is a parsed "WWW-Authenticate: Bearer ..." header, as
// returned by Docker Registry v2 servers (including Docker Hub) that
// require a short-lived token even for anonymous, read-only access.
type dockerAuthChallenge struct {
	realm   string
	service string
	scope   string
}

func parseWWWAuthenticate(header string) *dockerAuthChallenge {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return nil
	}
	challenge := &dockerAuthChallenge{}
	for _, kv := range strings.Split(strings.TrimPrefix(header, prefix), ",") {
		kv = strings.TrimSpace(kv)
		key, val, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, `"`)
		switch key {
		case "realm":
			challenge.realm = val
		case "service":
			challenge.service = val
		case "scope":
			challenge.scope = val
		}
	}
	return challenge
}

// upstreamRequest performs a GET against the upstream registry, transparently
// handling the Bearer-token challenge/response flow required by Docker Hub
// and other v2 registries before retrying the request with a token.
func (p *DockerProxy) upstreamRequest(reg *database.RegistryConfig, reqURL string, headers map[string]string) (*http.Response, error) {
	doGet := func(bearer string) (*http.Response, error) {
		req, err := http.NewRequest(http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		} else {
			// Static credentials configured for a private upstream (e.g.
			// another cargobay instance, or any auth-gated v2 registry).
			// Skipped once a challenge-issued bearer token is in hand.
			proxy.ApplyUpstreamAuth(req, reg)
		}
		return http.DefaultClient.Do(req)
	}

	resp, err := doGet("")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	challenge := parseWWWAuthenticate(resp.Header.Get("WWW-Authenticate"))
	resp.Body.Close()
	if challenge == nil || challenge.realm == "" {
		return nil, fmt.Errorf("upstream returned 401 without a usable auth challenge")
	}

	tokenURL := fmt.Sprintf("%s?service=%s&scope=%s", challenge.realm, url.QueryEscape(challenge.service), url.QueryEscape(challenge.scope))
	tokenReq, err := http.NewRequest(http.MethodGet, tokenURL, nil)
	if err != nil {
		return nil, err
	}
	// Basic-auth against the token realm is how docker login authenticates
	// to a private registry's token service (Docker Registry v2 auth spec).
	proxy.ApplyUpstreamAuth(tokenReq, reg)
	tokenResp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch upstream auth token: %w", err)
	}
	defer tokenResp.Body.Close()

	var tokenData struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		return nil, fmt.Errorf("failed to decode upstream auth token: %w", err)
	}
	token := tokenData.Token
	if token == "" {
		token = tokenData.AccessToken
	}
	if token == "" {
		return nil, fmt.Errorf("upstream auth response contained no token")
	}

	return doGet(token)
}

// fetchManifestFromUpstream pulls a manifest through from the upstream
// registry for a repository that isn't cached locally yet.
func (p *DockerProxy) fetchManifestFromUpstream(reg *database.RegistryConfig, repository, reference string) ([]byte, string, error) {
	reqURL := fmt.Sprintf("%s/v2/%s/manifests/%s", upstreamBase(reg), normalizeRepository(repository), reference)
	resp, err := p.upstreamRequest(reg, reqURL, map[string]string{
		"Accept": "application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json",
	})
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// fetchBlobFromUpstream pulls a blob (image layer or config) through from
// the upstream registry for a repository that isn't cached locally yet.
func (p *DockerProxy) fetchBlobFromUpstream(reg *database.RegistryConfig, repository, digest string) ([]byte, error) {
	reqURL := fmt.Sprintf("%s/v2/%s/blobs/%s", upstreamBase(reg), normalizeRepository(repository), digest)
	resp, err := p.upstreamRequest(reg, reqURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// handleHealth is the registry v2 ping/discovery endpoint. Anonymous
// requests (no Authorization header) always succeed, so anonymous `docker
// pull` keeps working unauthenticated for public registries — private ones
// still 401 once the request reaches a read/write handler and CheckAccess
// runs. A request that *does* present credentials only succeeds if they
// resolved to a user — this is what `docker login` probes to validate a
// username/password.
func (p *DockerProxy) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" && middleware.GetUser(r) == nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="cargobay Docker Registry"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}
