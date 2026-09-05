// Package composer implements a caching PHP Composer registry proxy.
//
// This package serves the simple (non-provider) Composer repository
// protocol: a single packages.json listing every known package version,
// with dist URLs rewritten to point back at our own caching download
// endpoint. Composer packages are namespaced by vendor ("{vendor}/{name}").
//
// Composer repository protocol: https://getcomposer.org/doc/05-repositories.md#package-repository
package composer

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

// ComposerProxy implements the Composer package-repository protocol.
// Packages are namespaced by vendor.
type ComposerProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// NewComposerProxy creates a new PHP Composer registry proxy instance.
func NewComposerProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &ComposerProxy{db: db, storage: storage, cache: cache}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "composer"))

	r.Get("/packages.json", proxy.handlePackagesJSON)
	// chi cannot match a literal suffix glued onto a {param} within the same
	// path segment (e.g. "{version}.zip"), so the version+".zip" is captured
	// as a wildcard and the suffix is stripped in handleDist instead.
	r.Get("/dist/{vendor}/{name}/*", proxy.handleDist)

	return r
}

type composerPackage struct {
	Name    string          `json:"name"`
	Version string          `json:"version"`
	Dist    composerDistRef `json:"dist"`
}

type composerDistRef struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// handlePackagesJSON serves the package listing, with every dist URL
// rewritten to our own /dist/ download endpoint.
func (p *ComposerProxy) handlePackagesJSON(w http.ResponseWriter, r *http.Request) {
	t := proxypkg.TargetFromContext(r, "composer")
	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{ArtifactType: "composer"})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list packages: %v", err), http.StatusInternalServerError)
		return
	}

	packages := map[string]map[string]composerPackage{}
	for _, a := range artifacts {
		fullName := fmt.Sprintf("%s/%s", a.Namespace, a.ArtifactName)
		if packages[fullName] == nil {
			packages[fullName] = map[string]composerPackage{}
		}
		packages[fullName][a.Version] = composerPackage{
			Name:    fullName,
			Version: a.Version,
			Dist: composerDistRef{
				Type: "zip",
				URL:  fmt.Sprintf("%s/dist/%s/%s/%s.zip", baseURL(r), a.Namespace, a.ArtifactName, a.Version),
			},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"packages": packages})
}

// handleDist handles the actual package .zip download. On a cache miss, it
// fetches the upstream packages.json to resolve the real (often
// third-party, e.g. a VCS host) dist URL for this version, then downloads
// and caches that.
func (p *ComposerProxy) handleDist(w http.ResponseWriter, r *http.Request) {
	vendor := chi.URLParam(r, "vendor")
	name := chi.URLParam(r, "name")
	rest := chi.URLParam(r, "*")
	version := strings.TrimSuffix(rest, ".zip")
	if version == rest {
		http.NotFound(w, r)
		return
	}
	t := proxypkg.TargetFromContext(r, "composer")

	if rc, err := p.storage.GetArtifactStream(t.Label, vendor, name, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/zip")
		if err := p.db.IncrementArtifactDownloads(t.Label, vendor, name, version); err != nil {
			fmt.Printf("failed to record download for %s/%s@%s: %v\n", vendor, name, version, err)
		}
		io.Copy(w, rc)
		return
	}

	distURL, err := p.resolveUpstreamDistURL(t.Reg, vendor, name, version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to resolve package: %v", err), http.StatusNotFound)
		return
	}
	resp, err := fetchStream(t.Reg, distURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download package: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, vendor, name, version, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save package: %v", err), http.StatusInternalServerError)
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("composer:%s:%s:%s:%s", t.Label, vendor, name, version),
		RegistryID:   t.Label,
		ArtifactType: "composer",
		Namespace:    vendor,
		ArtifactName: name,
		Version:      version,
		Size:         counter.N,
		Metadata:     map[string]interface{}{},
		Tags:         []string{version},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save package metadata: %v\n", err)
	}

	rc, err := p.storage.GetArtifactStream(t.Label, vendor, name, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve package", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/zip")
	if err := p.db.IncrementArtifactDownloads(t.Label, vendor, name, version); err != nil {
		fmt.Printf("failed to record download for %s/%s@%s: %v\n", vendor, name, version, err)
	}
	io.Copy(w, rc)
}

func (p *ComposerProxy) resolveUpstreamDistURL(reg *database.RegistryConfig, vendor, name, version string) (string, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return "", fmt.Errorf("no upstream proxy configured for this registry")
	}
	data, err := fetchBytes(reg, fmt.Sprintf("%s/packages.json", strings.TrimSuffix(reg.URL, "/")))
	if err != nil {
		return "", err
	}

	var doc struct {
		Packages map[string]map[string]composerPackage `json:"packages"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	fullName := fmt.Sprintf("%s/%s", vendor, name)
	versions, ok := doc.Packages[fullName]
	if !ok {
		return "", fmt.Errorf("package %s not found upstream", fullName)
	}
	pkg, ok := versions[version]
	if !ok {
		return "", fmt.Errorf("version %s of %s not found upstream", version, fullName)
	}
	if pkg.Dist.URL == "" {
		return "", fmt.Errorf("no dist URL for %s@%s", fullName, version)
	}
	return pkg.Dist.URL, nil
}

func fetchBytes(reg *database.RegistryConfig, url string) ([]byte, error) {
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

// fetchStream is like fetchBytes but returns the live response so a large
// dist archive can be streamed straight into storage instead of buffered in
// memory. Callers must close the returned body.
func fetchStream(reg *database.RegistryConfig, url string) (*http.Response, error) {
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

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}
