// Package swift implements the Swift Package Manager registry API (SE-0292).
//
// This package provides a caching proxy for Swift packages that:
//   - Lists known releases for a package
//   - Serves per-version metadata and Package.swift manifests
//   - Caches source-archive (.zip) downloads from an upstream registry
//
// Swift package registry API: https://github.com/swiftlang/swift-package-manager/blob/main/Documentation/PackageRegistry/Registry.md
package swift

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	proxypkg "github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/go-chi/chi/v5"
)

const registryContentType = "application/vnd.swift.registry.v1+json"

// SwiftProxy implements the Swift Package Manager registry API. Packages
// are namespaced by scope (e.g. "apple", "vapor").
type SwiftProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// NewSwiftProxy creates a new Swift Package Manager registry proxy
// instance.
func NewSwiftProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &SwiftProxy{db: db, storage: storage, cache: cache}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "swift"))

	r.Get("/{scope}/{name}", proxy.handleListReleases)
	// chi lets this plain-{version} route win over a sibling
	// "{version}.zip" route (both live in the same path segment), so a
	// request for the .zip source archive is dispatched here too;
	// handleReleaseMetadata detects the suffix and delegates.
	r.Get("/{scope}/{name}/{version}", proxy.handleReleaseMetadata)
	r.Get("/{scope}/{name}/{version}/Package.swift", proxy.handleManifest)

	return r
}

// handleListReleases serves the list-package-releases endpoint.
func (p *SwiftProxy) handleListReleases(w http.ResponseWriter, r *http.Request) {
	scope := chi.URLParam(r, "scope")
	name := chi.URLParam(r, "name")
	t := proxypkg.TargetFromContext(r, "swift")

	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{
		Namespace:    scope,
		ArtifactType: "swift",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list releases: %v", err), http.StatusInternalServerError)
		return
	}

	releases := map[string]interface{}{}
	for _, a := range artifacts {
		if a.ArtifactName == name {
			releases[a.Version] = map[string]string{
				"url": fmt.Sprintf("%s/%s/%s/%s", baseURL(r), scope, name, a.Version),
			}
		}
	}

	w.Header().Set("Content-Type", registryContentType)
	json.NewEncoder(w).Encode(map[string]interface{}{"releases": releases})
}

// handleReleaseMetadata serves per-version release metadata, pointing at
// our own source-archive and manifest endpoints.
func (p *SwiftProxy) handleReleaseMetadata(w http.ResponseWriter, r *http.Request) {
	scope := chi.URLParam(r, "scope")
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")

	if archiveVersion := strings.TrimSuffix(version, ".zip"); archiveVersion != version {
		p.handleSourceArchive(w, r, scope, name, archiveVersion)
		return
	}

	w.Header().Set("Content-Type", registryContentType)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      fmt.Sprintf("%s.%s", scope, name),
		"version": version,
		"resources": []map[string]interface{}{
			{
				"name": "source-archive",
				"type": "application/zip",
				"url":  fmt.Sprintf("%s/%s/%s/%s.zip", baseURL(r), scope, name, version),
			},
		},
	})
}

// handleManifest serves (or fetches-and-caches) the Package.swift manifest
// for a version.
func (p *SwiftProxy) handleManifest(w http.ResponseWriter, r *http.Request) {
	scope := chi.URLParam(r, "scope")
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")
	t := proxypkg.TargetFromContext(r, "swift")

	if data, err := p.storage.GetArtifact(t.Label, scope, name, version); err == nil && data != nil {
		w.Header().Set("Content-Type", "text/x-swift")
		w.Write(data)
		return
	}

	upstreamPath := fmt.Sprintf("%s/%s/%s/Package.swift", scope, name, version)
	data, err := p.fetchFromUpstream(t.Reg, upstreamPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch manifest: %v", err), http.StatusBadGateway)
		return
	}
	p.storage.SaveArtifact(t.Label, scope, name, version, data)

	w.Header().Set("Content-Type", "text/x-swift")
	w.Write(data)
}

// handleSourceArchive handles .zip source-archive downloads. It's reached
// via handleReleaseMetadata, which detects the ".zip" suffix that chi
// routes to the plain-{version} pattern (see NewSwiftProxy).
func (p *SwiftProxy) handleSourceArchive(w http.ResponseWriter, r *http.Request, scope, name, version string) {
	t := proxypkg.TargetFromContext(r, "swift")

	if rc, err := p.storage.GetArtifactStream(t.Label, scope, name, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/zip")
		if err := p.db.IncrementArtifactDownloads(t.Label, scope, name, version); err != nil {
			fmt.Printf("failed to record download for %s.%s@%s: %v\n", scope, name, version, err)
		}
		io.Copy(w, rc)
		return
	}

	upstreamPath := fmt.Sprintf("%s/%s/%s.zip", scope, name, version)
	resp, err := p.fetchStreamFromUpstream(t.Reg, upstreamPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch source archive: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, scope, name, version, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save source archive: %v", err), http.StatusInternalServerError)
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("swift:%s:%s:%s:%s", t.Label, scope, name, version),
		RegistryID:   t.Label,
		ArtifactType: "swift",
		Namespace:    scope,
		ArtifactName: name,
		Version:      version,
		Size:         counter.N,
		Metadata:     map[string]interface{}{},
		Tags:         []string{version},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save release metadata: %v\n", err)
	}

	rc, err := p.storage.GetArtifactStream(t.Label, scope, name, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve source archive", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/zip")
	if err := p.db.IncrementArtifactDownloads(t.Label, scope, name, version); err != nil {
		fmt.Printf("failed to record download for %s.%s@%s: %v\n", scope, name, version, err)
	}
	io.Copy(w, rc)
}

func (p *SwiftProxy) fetchStreamFromUpstream(reg *database.RegistryConfig, path string) (*http.Response, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}
	url := fmt.Sprintf("%s/%s", strings.TrimSuffix(reg.URL, "/"), path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	proxypkg.ApplyUpstreamAuth(req, reg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	return resp, nil
}

func (p *SwiftProxy) fetchFromUpstream(reg *database.RegistryConfig, path string) ([]byte, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}
	url := fmt.Sprintf("%s/%s", strings.TrimSuffix(reg.URL, "/"), path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	proxypkg.ApplyUpstreamAuth(req, reg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}
