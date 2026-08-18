// Package npm implements the npm Registry API proxy
//
// This pkgName provides a caching proxy for npm packages that:
//   - Caches packages from registry.npmjs.org
//   - Implements the npm Registry API
//   - Handles scoped packages (@scope/pkgName)
//   - Supports pkgName search and listing
//
// npm Registry API: https://github.com/npm/registry/blob/master/docs/README.md
package npm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	proxypkg "github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/go-chi/chi/v5"
)

// NPMProxy implements the npm Registry API proxy
type NPMProxy struct {
	db       *database.Database
	storage  storage.StorageAdapter
	cache    *cache.Cache
	registries []database.RegistryConfig
	registry string
}

// NewNPMProxy creates a new npm proxy instance
func NewNPMProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &NPMProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "npm",
	}

	// Resolve which registry a request addresses purely from the Host
	// header the client connected on (falling back to the default public
	// npm proxy) and enforce read access — no change to how packages are
	// named or referenced.
	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "npm"))

	r.Get("/", proxy.handleRoot)
	r.Get("/-/ping", proxy.handlePing)
	r.Get("/-/user", proxy.handleUser)
	r.Get("/-/user/sync", proxy.handleUserSync)

	// Scoped packages: /@scope/pkgName
	r.Get("/@{scope}/{pkgName}", proxy.handleScopedPackage)
	r.Get("/@{scope}/{pkgName}/{version}", proxy.handleScopedPackageVersion)

	// Regular packages: /pkgName
	r.Get("/{pkgName}", proxy.handlePackage)
	r.Get("/{pkgName}/{version}", proxy.handlePackageVersion)

	// Download tarballs
	r.Get("/{pkgName}/-/{tarballName}-{version}.tgz", proxy.handleTarball)
	r.Get("/@{scope}/{pkgName}/-/{tarballName}-{version}.tgz", proxy.handleScopedTarball)

	return r
}

// handleRoot returns registry info
func (p *NPMProxy) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name": "cargobay-npm",
		"version": "0.1.0",
		"description": "Caching proxy for npm registry",
	})
}

// handlePing handles npm ping endpoint
func (p *NPMProxy) handlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("OK"))
}

// handleUser returns the authenticated user's profile
func (p *NPMProxy) handleUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{})
}

// handleUserSync is a no-op sync endpoint for npm clients
func (p *NPMProxy) handleUserSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// handleScopedPackage handles scoped pkgName metadata
func (p *NPMProxy) handleScopedPackage(w http.ResponseWriter, r *http.Request) {
	t := proxypkg.TargetFromContext(r, "npm")
	scope := chi.URLParam(r, "scope")
	pkgName := chi.URLParam(r, "pkgName")
	packageName := fmt.Sprintf("@%s/%s", scope, pkgName)

	// Try cache first
	if data, err := p.cacheGet(packageName); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	// Fetch from upstream
	packageData, err := p.fetchFromUpstream(t.Reg, fmt.Sprintf("/%s", packageName))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch pkgName: %v", err), http.StatusBadGateway)
		return
	}

	// Save to cache
	p.cacheSet(packageName, packageData)

	// Save to database
	p.savePackageMetadata(t.Label, packageName, packageData)

	w.Header().Set("Content-Type", "application/json")
	w.Write(packageData)
}

// handleScopedPackageVersion handles scoped pkgName version metadata
func (p *NPMProxy) handleScopedPackageVersion(w http.ResponseWriter, r *http.Request) {
	t := proxypkg.TargetFromContext(r, "npm")
	scope := chi.URLParam(r, "scope")
	pkgName := chi.URLParam(r, "pkgName")
	version := chi.URLParam(r, "version")
	packageName := fmt.Sprintf("@%s/%s", scope, pkgName)

	// Try cache first
	cacheKey := fmt.Sprintf("%s:%s", packageName, version)
	if data, err := p.cacheGet(cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	// Fetch from upstream
	urlPath := fmt.Sprintf("/%s/%s", packageName, version)
	packageData, err := p.fetchFromUpstream(t.Reg, urlPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch pkgName version: %v", err), http.StatusBadGateway)
		return
	}

	// Save to cache
	p.cacheSet(cacheKey, packageData)

	// Save to database
	p.savePackageVersionMetadata(t.Label, packageName, version, packageData)

	w.Header().Set("Content-Type", "application/json")
	w.Write(packageData)
}

// handlePackage handles regular pkgName metadata
func (p *NPMProxy) handlePackage(w http.ResponseWriter, r *http.Request) {
	t := proxypkg.TargetFromContext(r, "npm")
	pkgName := chi.URLParam(r, "pkgName")

	// Try cache first
	if data, err := p.cacheGet(pkgName); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	// Fetch from upstream
	packageData, err := p.fetchFromUpstream(t.Reg, fmt.Sprintf("/%s", pkgName))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch pkgName: %v", err), http.StatusBadGateway)
		return
	}

	// Save to cache
	p.cacheSet(pkgName, packageData)

	// Save to database
	p.savePackageMetadata(t.Label, pkgName, packageData)

	w.Header().Set("Content-Type", "application/json")
	w.Write(packageData)
}

// handlePackageVersion handles regular pkgName version metadata
func (p *NPMProxy) handlePackageVersion(w http.ResponseWriter, r *http.Request) {
	t := proxypkg.TargetFromContext(r, "npm")
	pkgName := chi.URLParam(r, "pkgName")
	version := chi.URLParam(r, "version")

	// Try cache first
	cacheKey := fmt.Sprintf("%s:%s", pkgName, version)
	if data, err := p.cacheGet(cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	// Fetch from upstream
	urlPath := fmt.Sprintf("/%s/%s", pkgName, version)
	packageData, err := p.fetchFromUpstream(t.Reg, urlPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch pkgName version: %v", err), http.StatusBadGateway)
		return
	}

	// Save to cache
	p.cacheSet(cacheKey, packageData)

	// Save to database
	p.savePackageVersionMetadata(t.Label, pkgName, version, packageData)

	w.Header().Set("Content-Type", "application/json")
	w.Write(packageData)
}

// handleTarball handles pkgName tarball downloads
func (p *NPMProxy) handleTarball(w http.ResponseWriter, r *http.Request) {
	t := proxypkg.TargetFromContext(r, "npm")
	pkgName := chi.URLParam(r, "pkgName")
	version := chi.URLParam(r, "version")

	// Try to get from local storage first
	data, err := p.storage.GetArtifact("npm", "", pkgName, version)
	if err == nil && data != nil {
		// Check if upstream has a newer version by comparing digest
		if updated, err := p.fetchIfUpdated(t.Reg, "npm", "", pkgName, version); err == nil && updated != nil {
			// Upstream has a newer version, use the updated data
			data = updated
			// Save updated data to storage
			if _, err := p.storage.SaveArtifact("npm", "", pkgName, version, data); err != nil {
				// Log but don't fail - we still have the local data
				fmt.Printf("Failed to save updated artifact: %v\n", err)
			}
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.tgz", pkgName, version))
		w.Write(data)
		return
	}

	// Local artifact not found or failed to check for updates
	// Fetch from upstream
	tarballURL, err := p.getTarballURL(t.Reg, pkgName, version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get tarball URL: %v", err), http.StatusBadGateway)
		return
	}

	tarballReq, err := http.NewRequest(http.MethodGet, tarballURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build tarball request: %v", err), http.StatusInternalServerError)
		return
	}
	proxypkg.ApplyUpstreamAuth(tarballReq, t.Reg)
	resp, err := http.DefaultClient.Do(tarballReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download tarball: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	data = make([]byte, resp.ContentLength)
	resp.Body.Read(data)

	// Save to storage
	if _, err := p.storage.SaveArtifact("npm", "", pkgName, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	// Save to cache
	p.cacheSet(fmt.Sprintf("tarball:%s:%s", pkgName, version), data)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.tgz", pkgName, version))
	w.Write(data)
}

// handleScopedTarball handles scoped pkgName tarball downloads
func (p *NPMProxy) handleScopedTarball(w http.ResponseWriter, r *http.Request) {
	scope := chi.URLParam(r, "scope")
	pkgName := chi.URLParam(r, "pkgName")
	version := chi.URLParam(r, "version")
	packageName := fmt.Sprintf("@%s/%s", scope, pkgName)

	// Try cache first
	if data, err := p.storage.GetArtifact("npm", scope, pkgName, version); err == nil && data != nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.tgz", packageName, version))
		w.Write(data)
		return
	}

	// Fetch from upstream
	tarballURL := fmt.Sprintf("https://registry.npmjs.org/%s/-/%s-%s.tgz", packageName, packageName, version)

	resp, err := http.Get(tarballURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download tarball: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	data := make([]byte, resp.ContentLength)
	resp.Body.Read(data)

	// Save to storage
	if _, err := p.storage.SaveArtifact("npm", scope, pkgName, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.tgz", packageName, version))
	w.Write(data)
}

// fetchFromUpstream fetches data from the upstream npm registry for the
// resolved registry (or the default public npm registry if reg is nil or
// doesn't proxy an upstream).
func (p *NPMProxy) fetchFromUpstream(reg *database.RegistryConfig, path string) ([]byte, error) {
	upstream := "https://registry.npmjs.org"
	if reg != nil && reg.Proxy && reg.URL != "" {
		upstream = reg.URL
	}

	req, err := http.NewRequest(http.MethodGet, upstream+path, nil)
	if err != nil {
		return nil, err
	}
	proxypkg.ApplyUpstreamAuth(req, reg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data := make([]byte, resp.ContentLength)
	resp.Body.Read(data)
	return data, nil
}

// fetchIfUpdated re-downloads the tarball from upstream and returns its data
// only if it differs from the locally stored artifact.
func (p *NPMProxy) fetchIfUpdated(reg *database.RegistryConfig, artifactType, namespace, pkgName, version string) ([]byte, error) {
	tarballURL, err := p.getTarballURL(reg, pkgName, version)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, tarballURL, nil)
	if err != nil {
		return nil, err
	}
	proxypkg.ApplyUpstreamAuth(req, reg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if existing, err := p.storage.GetArtifact(artifactType, namespace, pkgName, version); err == nil && existing != nil && bytes.Equal(existing, data) {
		return nil, nil
	}

	return data, nil
}

// getTarballURL returns the tarball URL for a pkgName version
func (p *NPMProxy) getTarballURL(reg *database.RegistryConfig, pkgName, version string) (string, error) {
	registryData, err := p.fetchFromUpstream(reg, fmt.Sprintf("/%s/%s", pkgName, version))
	if err != nil {
		return "", err
	}

	var pkgData map[string]interface{}
	if err := json.Unmarshal(registryData, &pkgData); err != nil {
		return "", err
	}

	dist, ok := pkgData["dist"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("no dist field in pkgName data")
	}

	tarball, ok := dist["tarball"].(string)
	if !ok {
		return "", fmt.Errorf("no tarball field in dist")
	}

	return tarball, nil
}

// cacheGet retrieves data from cache
func (p *NPMProxy) cacheGet(key string) ([]byte, error) {
	var data []byte
	err := p.cache.Get(key, &data)
	return data, err
}

// cacheSet stores data in cache
func (p *NPMProxy) cacheSet(key string, data []byte) {
	p.cache.Set(key, data)
}

// savePackageMetadata saves pkgName metadata to database
func (p *NPMProxy) savePackageMetadata(registryLabel, packageName string, data []byte) {
	// Parse and save to database
	var pkgData map[string]interface{}
	if err := json.Unmarshal(data, &pkgData); err != nil {
		return
	}

	// Extract latest version
	latest, ok := pkgData["dist-tags"].(map[string]interface{})["latest"].(string)
	if !ok {
		return
	}

	// Create artifact metadata
	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("npm:%s:%s:latest", registryLabel, packageName),
		RegistryID:      registryLabel,
		ArtifactType:    "npm",
		Namespace:       "",
		ArtifactName:    packageName,
		Version:         latest,
		Digest:          fmt.Sprintf("sha256:%x", data), // Simplified
		DigestAlgorithm: "sha256",
		Size:            int64(len(data)),
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata:        pkgData,
		Tags:            []string{"latest"},
	}

	p.db.SaveArtifact(artifact)
}

// savePackageVersionMetadata saves pkgName version metadata to database
func (p *NPMProxy) savePackageVersionMetadata(registryLabel, packageName, version string, data []byte) {
	var pkgData map[string]interface{}
	if err := json.Unmarshal(data, &pkgData); err != nil {
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("npm:%s:%s:%s", registryLabel, packageName, version),
		RegistryID:      registryLabel,
		ArtifactType:    "npm",
		Namespace:       "",
		ArtifactName:    packageName,
		Version:         version,
		Digest:          fmt.Sprintf("sha256:%x", data), // Simplified
		DigestAlgorithm: "sha256",
		Size:            int64(len(data)),
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata:        pkgData,
		Tags:            []string{version},
	}

	p.db.SaveArtifact(artifact)
}
