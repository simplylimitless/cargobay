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
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/simplylimitless/cargobay/backend/pkg/vulnerability"
)

// upstreamClient is used for all requests to upstream registries. It bounds
// how long we'll wait for an upstream to start responding (DNS/connect/TLS/
// headers) so a stalled or rate-limiting upstream fails fast with a clear
// error instead of hanging until the pulling client's own timeout trips.
// There is deliberately no overall request timeout: once headers arrive,
// a multi-hundred-MB blob body can legitimately take a while to stream.
var upstreamClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

// DockerProxy implements the Docker Registry v2 API proxy
type DockerProxy struct {
	db         *database.Database
	storage    storage.StorageAdapter
	cache      *cache.Cache
	rbac       *rbac.RBAC
	registries []database.RegistryConfig
	scanner    *vulnerability.VulnerabilityScanner
}

// NewDockerProxy creates a new Docker proxy instance
func NewDockerProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig, scanner *vulnerability.VulnerabilityScanner) chi.Router {
	r := chi.NewRouter()

	p := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		rbac:       rbacMgr,
		registries: registries,
		scanner:    scanner,
	}

	// Docker Registry v2 API endpoints (mounted under /v2 by the caller)

	// Health check
	r.Get("/", p.handleHealth)

	// List repositories
	r.Get("/_catalog", p.handleCatalog)

	// Repository names routinely span multiple path segments (e.g.
	// "library/nginx", "someorg/someteam/someimage"), which chi's
	// single-segment {repository} param can't capture. Reads/writes are
	// dispatched through a wildcard and the repository/reference/digest are
	// parsed out of the raw request path instead.
	r.Get("/*", p.handleV2Read)
	r.Head("/*", p.handleV2Head)

	// Write operations (push/delete) require an authenticated user — anonymous
	// requests may pull, but must not be able to upload or remove artifacts.
	// Beyond that, CheckAccess (called from each dispatcher) enforces that
	// only a registry the caller may publish to accepts the write at all.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)

		r.Put("/*", p.handleV2Put)
		r.Post("/*", p.handleV2Post)
		r.Patch("/*", p.handleV2Patch)
		r.Delete("/*", p.handleV2Delete)
	})

	return r
}

// PrefetchManifest fetches and caches a manifest for "repository:reference"
// (e.g. "library/ubuntu" + "latest") from reg's upstream, if it isn't already
// cached locally — the same path a real client pull of that reference would
// take, without waiting for a client to actually request it. Returns
// alreadyCached=true (and does not touch upstream) if a matching artifact is
// already stored. Used by the API's on-demand cache-populate endpoint (see
// api.Server.handlePrefetchArtifact).
func PrefetchManifest(db *database.Database, storageAdapter storage.StorageAdapter, cacheClient *cache.Cache, scanner *vulnerability.VulnerabilityScanner, reg *database.RegistryConfig, repository, reference string) (alreadyCached bool, err error) {
	namespace, name := splitDockerRepository(repository)

	artifacts, err := db.ListArtifacts(reg.ID, database.ListOptions{
		ArtifactType: "docker",
		Namespace:    namespace,
	})
	if err != nil {
		return false, err
	}
	for _, a := range artifacts {
		if a.ArtifactName == name && (a.Version == reference || a.Digest == reference) {
			return true, nil
		}
	}

	p := &DockerProxy{db: db, storage: storageAdapter, cache: cacheClient, scanner: scanner}
	if _, _, _, err := p.cacheManifestFromUpstream(target{reg: reg, label: reg.ID}, repository, reference); err != nil {
		return false, err
	}
	return false, nil
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

// isDigestReference reports whether reference is a content digest
// ("sha256:<hex>") rather than a human-readable tag. Docker clients resolve
// a tag to a manifest (list) digest, then fetch each platform-specific
// manifest referenced by that list via its own digest — those digest-only
// fetches are children of the tag, not additional tags in their own right,
// so they must not be persisted as separate top-level artifact versions.
func isDigestReference(reference string) bool {
	return strings.HasPrefix(reference, "sha256:")
}

// manifestAcceptHeader is sent on every upstream manifest request (full
// fetch, and the HEAD used to revalidate a cached tag) so upstream returns
// the same manifest kind either way.
const manifestAcceptHeader = "application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json"

// staleCachedTag reports whether a cached tag's digest no longer matches what
// the upstream registry currently serves for that tag. Digest references are
// immutable by definition and never revalidated. If proxying is disabled (no
// upstream to check against) or the upstream check itself fails (e.g.
// transient network error), the cached copy is treated as fresh — pulls stay
// available even when upstream is briefly unreachable, at the cost of
// possibly serving a tag that changed moments ago.
func (p *DockerProxy) staleCachedTag(t target, repository, reference, cachedDigest string) bool {
	if isDigestReference(reference) || t.reg == nil || !t.reg.Proxy {
		return false
	}
	upstreamDigest, err := p.fetchUpstreamDigest(t.reg, repository, reference)
	if err != nil {
		log.Printf("tag revalidation: failed to check %s/%s for staleness, serving cached copy: %v", repository, reference, err)
		return false
	}
	return upstreamDigest != cachedDigest
}

// manifestList mirrors the subset of the Docker manifest-list / OCI image
// index schema needed to surface each platform's digest, since cargobay's
// artifacts table stores a single digest per row and this is otherwise the
// only place that per-platform relationship is captured.
type manifestList struct {
	Manifests []struct {
		Digest   string `json:"digest"`
		Size     int64  `json:"size"`
		Platform struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
		} `json:"platform"`
	} `json:"manifests"`
}

// childManifestsFromList parses body as a manifest list / OCI index and
// returns a JSON-friendly summary of the platform-specific manifests it
// references, or nil if body isn't a manifest list (e.g. a single-platform
// manifest).
func childManifestsFromList(body []byte) []map[string]interface{} {
	var list manifestList
	if err := json.Unmarshal(body, &list); err != nil || len(list.Manifests) == 0 {
		return nil
	}
	children := make([]map[string]interface{}, 0, len(list.Manifests))
	for _, m := range list.Manifests {
		children = append(children, map[string]interface{}{
			"digest": m.Digest,
			"size":   m.Size,
			"os":     m.Platform.OS,
			"arch":   m.Platform.Architecture,
		})
	}
	return children
}

// manifestContentSize sums the config and layer blob sizes declared inside a
// single-platform image manifest — the actual bytes a client pulls for that
// manifest, as opposed to the manifest JSON document itself (a few hundred
// bytes to a few KB, and not representative of image size on its own).
func manifestContentSize(body []byte) int64 {
	var m struct {
		Config struct {
			Size int64 `json:"size"`
		} `json:"config"`
		Layers []struct {
			Size int64 `json:"size"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return 0
	}
	total := m.Config.Size
	for _, layer := range m.Layers {
		total += layer.Size
	}
	return total
}

// splitDockerRepository splits a Docker repository path like "library/nginx"
// into its namespace ("library") and image name ("nginx"), so the two are
// stored as distinct fields instead of duplicating the full path into both.
// Repositories with no namespace segment use the whole path as the name and
// leave namespace empty.
func splitDockerRepository(repository string) (namespace, name string) {
	idx := strings.Index(repository, "/")
	if idx < 0 {
		return "", repository
	}
	return repository[:idx], repository[idx+1:]
}

// target bundles the registry resolved for a request (by Host header, or
// the default public proxy) with the label used to namespace its stored
// artifacts.
type target struct {
	reg   *database.RegistryConfig
	label string
}

// dockerPathPrefixSegment marks a request as addressing a registry by path
// rather than by Host: cargobay-host/dkr/ghcr.io/org/image. Requiring this
// literal segment (rather than treating any leading path component as a
// possible registry selector) keeps the namespace unambiguous — a plain
// pull like cargobay-host/library/nginx is never mistaken for a registry
// selector, and everything under /dkr/ is legible at a glance as Docker
// registry proxying.
const dockerPathPrefixSegment = "dkr/"

// resolveTarget picks which registry a request addresses. A Host header
// bound to a specific registry wins outright; otherwise, if the path starts
// with the dockerPathPrefixSegment marker, the segment after it is matched
// against a registry (by ID or by its upstream hostname, e.g. "ghcr.io")
// and both are stripped, so a client can address any configured upstream
// through a single cargobay hostname (cargobay-host/dkr/ghcr.io/org/image)
// without per-registry DNS. Falls back to the default docker registry, path
// unchanged, if neither matches. Returns the resolved target along with the
// path handlers should parse the repository/reference/digest out of.
func (p *DockerProxy) resolveTarget(r *http.Request) (target, string) {
	path := strings.TrimPrefix(r.URL.Path, "/v2/")

	var reg *database.RegistryConfig
	if rest, ok := strings.CutPrefix(path, dockerPathPrefixSegment); ok {
		reg, path = proxy.ResolveRegistryWithPathPrefix(p.db, r.Host, rest, "docker")
	} else {
		reg = proxy.ResolveRegistry(p.db, p.registries, r.Host, "docker")
	}

	label := "docker"
	if reg != nil {
		label = reg.ID
	}
	return target{reg: reg, label: label}, path
}

// checkAccess resolves the target registry (and repository-parseable path)
// for r and enforces read/publish access on it, writing a 401/403 and
// returning ok=false if denied.
func (p *DockerProxy) checkAccess(w http.ResponseWriter, r *http.Request, requirePublish bool) (t target, path string, ok bool) {
	t, path = p.resolveTarget(r)
	if !proxy.CheckAccess(w, p.rbac, middleware.GetUser(r), t.reg, requirePublish) {
		return t, path, false
	}
	return t, path, true
}

// handleV2Read dispatches GET requests under /v2/ to the manifest, tag
// list, or blob handler based on the request path shape.
func (p *DockerProxy) handleV2Read(w http.ResponseWriter, r *http.Request) {
	t, path, ok := p.checkAccess(w, r, false)
	if !ok {
		return
	}

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
	t, path, ok := p.checkAccess(w, r, false)
	if !ok {
		return
	}

	if repo, ref, ok := splitAtLast(path, "/manifests/"); ok {
		p.handleHeadManifest(w, r, t, repo, ref)
		return
	}
	if repo, digest, ok := splitAtLast(path, "/blobs/"); ok {
		p.handleHeadBlob(w, r, t, repo, digest)
		return
	}
	http.NotFound(w, r)
}

func (p *DockerProxy) handleV2Put(w http.ResponseWriter, r *http.Request) {
	t, path, ok := p.checkAccess(w, r, true)
	if !ok {
		return
	}

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
	t, path, ok := p.checkAccess(w, r, true)
	if !ok {
		return
	}

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
	t, path, ok := p.checkAccess(w, r, true)
	if !ok {
		return
	}

	if repo, uploadID, ok := splitAtLast(path, "/blobs/uploads/"); ok {
		p.handlePatchUpload(w, r, t, repo, uploadID)
		return
	}
	http.NotFound(w, r)
}

func (p *DockerProxy) handleV2Delete(w http.ResponseWriter, r *http.Request) {
	t, path, ok := p.checkAccess(w, r, true)
	if !ok {
		return
	}

	if repo, ref, ok := splitAtLast(path, "/manifests/"); ok {
		p.handleDeleteManifest(w, r, t, repo, ref)
		return
	}
	http.NotFound(w, r)
}

// handleCatalog lists repositories in the registry resolved for this
// request (by Host header, or a leading path-prefix selector) — a caller
// only ever sees repositories in the registry they're actually talking to.
func (p *DockerProxy) handleCatalog(w http.ResponseWriter, r *http.Request) {
	t, _, ok := p.checkAccess(w, r, false)
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
			repo := a.ArtifactName
			if a.Namespace != "" {
				repo = a.Namespace + "/" + a.ArtifactName
			}
			if !seen[repo] {
				repositories = append(repositories, repo)
				seen[repo] = true
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
	namespace, name := splitDockerRepository(repository)

	// Get tags from database
	artifacts, err := p.db.ListArtifacts(t.label, database.ListOptions{
		ArtifactType: "docker",
		Namespace:    namespace,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get tags: %v", err), http.StatusBadGateway)
		return
	}

	tags := make([]string, 0, len(artifacts))
	for _, a := range artifacts {
		if a.ArtifactName == name {
			tags = append(tags, a.Version)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name": repository,
		"tags": tags,
	})
}

// cacheManifestFromUpstream pulls a manifest through from the upstream
// registry and caches it under the reference the client actually requested.
// This matters because containerd-backed Docker clients resolve a tag to a
// digest via HEAD first, then fetch content with a second GET keyed by that
// digest — if only the GET path cached artifacts, every cached row would be
// keyed by the opaque digest instead of the human-readable tag ("latest").
// Caching on the HEAD means the tag wins: the later GET-by-digest finds this
// row via its Digest match and returns it as-is, without overwriting Version.
func (p *DockerProxy) cacheManifestFromUpstream(t target, repository, reference string) (body []byte, digest, contentType string, err error) {
	namespace, name := splitDockerRepository(repository)

	body, contentType, err = p.fetchManifestFromUpstream(t.reg, repository, reference)
	if err != nil {
		return nil, "", "", err
	}

	digestBytes := sha256.Sum256(body)
	digest = fmt.Sprintf("sha256:%x", digestBytes)

	if _, err := p.storage.SaveArtifact(t.label, namespace, name, reference, body); err != nil {
		return nil, "", "", err
	}

	if contentType == "" {
		contentType = "application/vnd.docker.distribution.manifest.v2+json"
	}

	// A digest-only fetch is a client resolving one platform's manifest out
	// of a tag's manifest list, not a new tag — persist the blob so it can
	// be served again, but don't create a separate top-level artifact
	// version for it (see isDigestReference).
	if isDigestReference(reference) {
		return body, digest, contentType, nil
	}

	metadata := map[string]interface{}{
		// Stored as a string, not []byte: the metadata map is JSON-encoded
		// for the database's JSONB column, and encoding/json base64-encodes
		// []byte values, which would corrupt the manifest and desync its
		// size from what's actually served.
		"manifest":    string(body),
		"contentType": contentType,
	}

	// Calculate total size: manifest document size + the real pullable
	// content (config + layer blobs). For a manifest list, that content
	// lives in each platform's own manifest, not the list itself, so each
	// child's declared "size" (the list's summary of its manifest doc size)
	// is corrected here to its real content size before being stored.
	totalSize := int64(len(body))
	if children := childManifestsFromList(body); children != nil {
		for _, child := range children {
			if childDigest, ok := child["digest"].(string); ok && childDigest != "" {
				platformBody, _, err := p.fetchManifestFromUpstream(t.reg, repository, childDigest)
				if err == nil {
					platformSize := int64(len(platformBody)) + manifestContentSize(platformBody)
					child["size"] = platformSize
					totalSize += platformSize
				}
			}
		}
		metadata["childManifests"] = children
	} else {
		totalSize += manifestContentSize(body)
	}

	cached := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("docker:%s:%s:%s", t.label, strings.ReplaceAll(repository, "/", "_"), reference),
		RegistryID:      t.label,
		ArtifactType:    "docker",
		Namespace:       namespace,
		ArtifactName:    name,
		Version:         reference,
		Digest:          digest,
		DigestAlgorithm: "sha256",
		Size:            int64(len(body)),
		TotalSize:       totalSize,
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata:        metadata,
		Tags:            []string{reference},
	}
	if err := p.db.SaveArtifact(cached); err != nil {
		return nil, "", "", err
	}
	p.triggerAsyncScan(t.label, repository, namespace, name, reference)

	return body, digest, contentType, nil
}

// handleManifest retrieves a manifest, pulling it through from the
// upstream registry and caching it on first request if it isn't already
// stored locally.
func (p *DockerProxy) handleManifest(w http.ResponseWriter, r *http.Request, t target, repository, reference string) {
	namespace, name := splitDockerRepository(repository)

	// Try to find artifact in database
	artifacts, err := p.db.ListArtifacts(t.label, database.ListOptions{
		ArtifactType: "docker",
		Namespace:    namespace,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get manifest: %v", err), http.StatusBadGateway)
		return
	}

	// Find matching artifact
	var artifact *database.ArtifactMetadata
	for _, a := range artifacts {
		if a.ArtifactName == name && (a.Version == reference || a.Digest == reference) {
			artifact = &a
			break
		}
	}

	if artifact != nil && !p.staleCachedTag(t, repository, reference, artifact.Digest) {
		// If proxy is disabled for this registry, don't serve cached content
		// that came from upstream - return 404 so the client knows proxying
		// is not available.
		if t.reg != nil && !t.reg.Proxy {
			http.Error(w, "upstream proxy disabled", http.StatusNotFound)
			return
		}

		contentType := "application/vnd.docker.distribution.manifest.v2+json"
		if ct, ok := artifact.Metadata["contentType"].(string); ok && ct != "" {
			contentType = ct
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Docker-Content-Digest", artifact.Digest)

		manifest := artifact.Metadata["manifest"]
		var body []byte
		switch v := manifest.(type) {
		case []byte:
			body = v
		case string:
			body = []byte(v)
		default:
			http.Error(w, "internal error: invalid manifest type", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Docker-Size", fmt.Sprintf("%d", len(body)))
		if !isDigestReference(reference) {
			if err := p.db.IncrementArtifactDownloads(t.label, namespace, name, artifact.Version); err != nil {
				log.Printf("failed to record download for %s/%s:%s: %v", namespace, name, artifact.Version, err)
			}
		}
		w.Write(body)
		return
	}

	// Not cached — pull the manifest through from the upstream registry, if
	// this registry has one configured.
	body, digest, contentType, err := p.cacheManifestFromUpstream(t, repository, reference)
	if err != nil {
		http.Error(w, "manifest unknown", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("X-Docker-Size", fmt.Sprintf("%d", len(body)))
	if !isDigestReference(reference) {
		if err := p.db.IncrementArtifactDownloads(t.label, namespace, name, reference); err != nil {
			log.Printf("failed to record download for %s/%s:%s: %v", namespace, name, reference, err)
		}
	}
	w.Write(body)
}

// handleHeadManifest resolves a manifest's digest/size without returning its
// body — required by the Docker Registry v2 API, and relied on by clients
// (e.g. containerd-backed Docker Desktop) to resolve a tag to a digest
// before pulling. Without this, such clients fall back to fetching
// manifests only by digest, which never records the original tag.
func (p *DockerProxy) handleHeadManifest(w http.ResponseWriter, r *http.Request, t target, repository, reference string) {
	namespace, name := splitDockerRepository(repository)

	artifacts, err := p.db.ListArtifacts(t.label, database.ListOptions{
		ArtifactType: "docker",
		Namespace:    namespace,
	})
	if err == nil {
		for _, a := range artifacts {
			if a.ArtifactName == name && (a.Version == reference || a.Digest == reference) {
				if p.staleCachedTag(t, repository, reference, a.Digest) {
					break
				}
				// If proxy is disabled for this registry, don't serve cached content
				if t.reg != nil && !t.reg.Proxy {
					http.Error(w, "upstream proxy disabled", http.StatusNotFound)
					return
				}

				contentType := "application/vnd.docker.distribution.manifest.v2+json"
				if ct, ok := a.Metadata["contentType"].(string); ok && ct != "" {
					contentType = ct
				}

				manifest := a.Metadata["manifest"]
				var body []byte
				switch v := manifest.(type) {
				case []byte:
					body = v
				case string:
					body = []byte(v)
				default:
					http.Error(w, "internal error: invalid manifest type", http.StatusInternalServerError)
					return
				}

				w.Header().Set("Content-Type", contentType)
				w.Header().Set("Docker-Content-Digest", a.Digest)
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
				if !isDigestReference(reference) {
					if err := p.db.IncrementArtifactDownloads(t.label, namespace, name, a.Version); err != nil {
						log.Printf("failed to record download for %s/%s:%s: %v", namespace, name, a.Version, err)
					}
				}
				w.WriteHeader(http.StatusOK)
				return
			}
		}
	}

	body, digest, contentType, err := p.cacheManifestFromUpstream(t, repository, reference)
	if err != nil {
		http.Error(w, "manifest unknown", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	if !isDigestReference(reference) {
		if err := p.db.IncrementArtifactDownloads(t.label, namespace, name, reference); err != nil {
			log.Printf("failed to record download for %s/%s:%s: %v", namespace, name, reference, err)
		}
	}
	w.WriteHeader(http.StatusOK)
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

	// Split repository to get namespace and name
	namespace, name := splitDockerRepository(repository)

	// Save manifest to storage
	if _, err := p.storage.SaveArtifact(t.label, namespace, name, reference, body); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save manifest: %v", err), http.StatusInternalServerError)
		return
	}

	// Calculate total size: manifest document size + the real pullable
	// content (config + layer blobs) — see manifestContentSize.
	totalSize := int64(len(body))
	if children := childManifestsFromList(body); children != nil {
		for _, child := range children {
			if childDigest, ok := child["digest"].(string); ok && childDigest != "" {
				platformBody, _, err := p.fetchManifestFromUpstream(t.reg, repository, childDigest)
				if err == nil {
					totalSize += int64(len(platformBody)) + manifestContentSize(platformBody)
				}
			}
		}
	} else {
		totalSize += manifestContentSize(body)
	}

	// Save to database
	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("docker:%s:%s:%s", t.label, strings.ReplaceAll(repository, "/", "_"), reference),
		RegistryID:      t.label,
		ArtifactType:    "docker",
		Namespace:       namespace,
		ArtifactName:    name,
		Version:         reference,
		Digest:          digest,
		DigestAlgorithm: "sha256",
		Size:            int64(len(body)),
		TotalSize:       totalSize,
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata:        map[string]interface{}{"manifest": string(body)},
		Tags:            []string{reference},
	}

	if err := p.db.SaveArtifact(artifact); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}
	p.triggerAsyncScan(t.label, repository, namespace, name, reference)

	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

// triggerAsyncScan fires a vulnerability scan for a just-cached docker/oci
// artifact in the background, so it never adds latency to the push/pull
// request that triggered it. The scanner pulls the image itself over
// cargobay's own registry API, so no artifact data needs to be passed here.
// registryLabel and repository must match what the caller just saved the
// artifact under, or the scan result gets persisted against an artifact ID
// that never matches a real row and the vulnerability badge never shows up.
func (p *DockerProxy) triggerAsyncScan(registryLabel, repository, namespace, name, reference string) {
	if p.scanner == nil || isDigestReference(reference) {
		return
	}
	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("docker:%s:%s:%s", registryLabel, strings.ReplaceAll(repository, "/", "_"), reference),
		RegistryID:   registryLabel,
		ArtifactType: "docker",
		Namespace:    namespace,
		ArtifactName: name,
		Version:      reference,
	}
	go func() {
		result, err := p.scanner.ScanArtifact(artifact, nil)
		if err != nil {
			log.Printf("vulnerability scan-on-push: scan failed for %s: %v", artifact.ID, err)
			return
		}
		if err := p.scanner.SaveScanResult(result); err != nil {
			log.Printf("vulnerability scan-on-push: failed to save result for %s: %v", artifact.ID, err)
		}
	}()
}

// handleGetBlob retrieves a blob (layer), pulling it through from the
// upstream registry and caching it on first request if it isn't already
// stored locally.
func (p *DockerProxy) handleGetBlob(w http.ResponseWriter, r *http.Request, t target, repository, digest string) {
	// If proxy is disabled, don't fetch from upstream - return 404 for
	// uncached content since the user explicitly doesn't want proxying.
	if t.reg != nil && !t.reg.Proxy {
		http.Error(w, "upstream proxy disabled", http.StatusNotFound)
		return
	}

	rc, err := p.storage.GetArtifactStream("docker", repository, "blob", digest)
	if err != nil || rc == nil {
		if err := p.streamBlobFromUpstream(t.reg, repository, digest); err != nil {
			http.Error(w, "blob unknown", http.StatusNotFound)
			return
		}
		rc, err = p.storage.GetArtifactStream("docker", repository, "blob", digest)
		if err != nil || rc == nil {
			http.Error(w, "blob unknown", http.StatusNotFound)
			return
		}
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusOK)
	io.Copy(w, rc)
}

// handleHeadBlob checks if a blob exists, pulling it through and caching it
// from upstream if it isn't stored locally yet (docker push probes with
// HEAD before uploading, to skip blobs the registry already has).
func (p *DockerProxy) handleHeadBlob(w http.ResponseWriter, r *http.Request, t target, repository, digest string) {
	// If proxy is disabled, don't fetch from upstream - return 404 for
	// uncached content since the user explicitly doesn't want proxying.
	if t.reg != nil && !t.reg.Proxy {
		http.Error(w, "upstream proxy disabled", http.StatusNotFound)
		return
	}

	exists, err := p.storage.ArtifactExists("docker", repository, "blob", digest)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check blob: %v", err), http.StatusInternalServerError)
		return
	}

	if !exists {
		if err := p.streamBlobFromUpstream(t.reg, repository, digest); err != nil {
			http.Error(w, "blob unknown", http.StatusNotFound)
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
	namespace, name := splitDockerRepository(repository)

	if err := p.storage.DeleteArtifact(t.label, namespace, name, reference); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete manifest: %v", err), http.StatusInternalServerError)
		return
	}

	if _, err := p.db.DeleteArtifact(t.label, namespace, name, reference); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete artifact from database: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// upstreamBase returns the base URL to pull through from for the resolved
// registry. Returns an error if the registry has no upstream proxy
// configured — it never silently falls back to Docker Hub.
func upstreamBase(reg *database.RegistryConfig) (string, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return "", fmt.Errorf("no upstream proxy configured for this registry")
	}
	return strings.TrimSuffix(reg.URL, "/"), nil
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

// upstreamRequest performs a request against the upstream registry,
// transparently handling the Bearer-token challenge/response flow required
// by Docker Hub and other v2 registries before retrying the request with a
// token.
func (p *DockerProxy) upstreamRequest(reg *database.RegistryConfig, method, reqURL string, headers map[string]string) (*http.Response, error) {
	doGet := func(bearer string) (*http.Response, error) {
		req, err := http.NewRequest(method, reqURL, nil)
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
		return upstreamClient.Do(req)
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
	tokenResp, err := upstreamClient.Do(tokenReq)
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
	base, err := upstreamBase(reg)
	if err != nil {
		return nil, "", err
	}
	reqURL := fmt.Sprintf("%s/v2/%s/manifests/%s", base, normalizeRepository(repository), reference)
	resp, err := p.upstreamRequest(reg, http.MethodGet, reqURL, map[string]string{
		"Accept": manifestAcceptHeader,
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

// fetchUpstreamDigest performs a cheap HEAD request against the upstream
// registry to learn the current digest a mutable tag resolves to, without
// downloading the manifest body. Used to revalidate a cached tag on every
// pull, since a tag like "latest" can be repointed upstream at any time.
func (p *DockerProxy) fetchUpstreamDigest(reg *database.RegistryConfig, repository, reference string) (string, error) {
	base, err := upstreamBase(reg)
	if err != nil {
		return "", err
	}
	reqURL := fmt.Sprintf("%s/v2/%s/manifests/%s", base, normalizeRepository(repository), reference)
	resp, err := p.upstreamRequest(reg, http.MethodHead, reqURL, map[string]string{
		"Accept": manifestAcceptHeader,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("upstream HEAD response had no Docker-Content-Digest header")
	}
	return digest, nil
}

// streamBlobFromUpstream pulls a blob through from the upstream registry and
// writes it straight into storage as it downloads, so a large layer (a
// multi-hundred-MB Docker image blob is common) never sits fully buffered in
// process memory. Callers read the cached copy back via
// storage.GetArtifactStream once this returns.
func (p *DockerProxy) streamBlobFromUpstream(reg *database.RegistryConfig, repository, digest string) error {
	base, err := upstreamBase(reg)
	if err != nil {
		return err
	}
	reqURL := fmt.Sprintf("%s/v2/%s/blobs/%s", base, normalizeRepository(repository), digest)
	resp, err := p.upstreamRequest(reg, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	_, err = p.storage.SaveArtifactStream("docker", repository, "blob", digest, resp.Body)
	return err
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
