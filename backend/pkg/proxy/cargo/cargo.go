// Package cargo implements the Cargo (Rust) sparse HTTP registry protocol.
//
// This package provides a caching proxy for crates that:
//   - Serves the registry config.json required by every sparse registry
//   - Serves the sparse index (one JSON-lines file per crate, path sharded
//     the same way crates.io shards its own index)
//   - Caches .crate tarballs from an upstream sparse registry (crates.io by
//     default)
//
// Sparse registry protocol: https://doc.rust-lang.org/cargo/reference/registries.html#sparse-protocol
package cargo

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

// CargoProxy implements the Cargo sparse registry protocol proxy.
type CargoProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// NewCargoProxy creates a new Cargo proxy instance.
func NewCargoProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &CargoProxy{db: db, storage: storage, cache: cache}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "cargo"))

	// Registry configuration, required by every sparse registry.
	r.Get("/config.json", proxy.handleConfig)

	// Sparse index, sharded the same way crates.io shards its own index:
	//   1-char name:  /1/{name}
	//   2-char name:  /2/{name}
	//   3-char name:  /3/{name[0]}/{name}
	//   4+-char name: /{name[0:2]}/{name[2:4]}/{name}
	r.Get("/1/{name}", proxy.handleIndex)
	r.Get("/2/{name}", proxy.handleIndex)
	r.Get("/3/{prefix}/{name}", proxy.handleIndex)
	r.Get("/{p1}/{p2}/{name}", proxy.handleIndex)

	// Crate tarball download.
	r.Get("/api/v1/crates/{name}/{version}/download", proxy.handleDownload)

	return r
}

// handleConfig serves the registry's config.json.
func (p *CargoProxy) handleConfig(w http.ResponseWriter, r *http.Request) {
	origin := fmt.Sprintf("%s://%s", schemeFor(r), r.Host)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"dl":  origin + "/cargo/api/v1/crates",
		"api": origin + "/cargo",
	})
}

// handleIndex serves the sparse index file for a single crate: one JSON
// object per line, one line per published version.
func (p *CargoProxy) handleIndex(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	t := proxypkg.TargetFromContext(r, "cargo")

	cacheKey := fmt.Sprintf("cargo:index:%s", name)
	if data, err := p.cacheGet(cacheKey); err == nil {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(data)
		return
	}

	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{
		ArtifactType: "cargo",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list crate versions: %v", err), http.StatusBadGateway)
		return
	}

	var lines []string
	for _, a := range artifacts {
		if a.ArtifactName != name {
			continue
		}
		lines = append(lines, cargoIndexLine(name, a))
	}

	if len(lines) == 0 {
		data, err := p.fetchIndexFromUpstream(t.Reg, name)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch crate index: %v", err), http.StatusNotFound)
			return
		}
		p.cacheSet(cacheKey, data)
		w.Header().Set("Content-Type", "text/plain")
		w.Write(data)
		return
	}

	data := []byte(strings.Join(lines, "\n") + "\n")
	p.cacheSet(cacheKey, data)
	w.Header().Set("Content-Type", "text/plain")
	w.Write(data)
}

// cargoIndexLine builds one sparse-index JSON-lines entry from a cached
// artifact's metadata (saved verbatim from upstream on first fetch, or from
// a locally published crate's Cargo.toml-derived metadata).
func cargoIndexLine(name string, a database.ArtifactMetadata) string {
	if raw, ok := a.Metadata["indexLine"].(string); ok && raw != "" {
		return raw
	}
	entry := map[string]interface{}{
		"name":     name,
		"vers":     a.Version,
		"deps":     []interface{}{},
		"cksum":    strings.TrimPrefix(a.Digest, "sha256:"),
		"features": map[string]interface{}{},
		"yanked":   false,
	}
	line, _ := json.Marshal(entry)
	return string(line)
}

// handleDownload handles .crate tarball downloads.
func (p *CargoProxy) handleDownload(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")
	t := proxypkg.TargetFromContext(r, "cargo")

	if data, err := p.storage.GetArtifact("cargo", "", name, version); err == nil && data != nil {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.crate", name, version))
		if err := p.db.IncrementArtifactDownloads(t.Label, "", name, version); err != nil {
			fmt.Printf("failed to record download for %s@%s: %v\n", name, version, err)
		}
		w.Write(data)
		return
	}

	data, err := p.fetchCrateFromUpstream(t.Reg, t.Label, name, version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download crate: %v", err), http.StatusBadGateway)
		return
	}

	if _, err := p.storage.SaveArtifact("cargo", "", name, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save crate: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.crate", name, version))
	if err := p.db.IncrementArtifactDownloads(t.Label, "", name, version); err != nil {
		fmt.Printf("failed to record download for %s@%s: %v\n", name, version, err)
	}
	w.Write(data)
}

// fetchIndexFromUpstream fetches a crate's sparse index file from upstream
// and saves one artifact record per version line so subsequent index and
// download requests are served from the database/storage cache.
func (p *CargoProxy) fetchIndexFromUpstream(reg *database.RegistryConfig, name string) ([]byte, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}

	indexURL := fmt.Sprintf("%s/%s", strings.TrimSuffix(reg.URL, "/"), sparseIndexPath(name))
	req, err := http.NewRequest(http.MethodGet, indexURL, nil)
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

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry struct {
			Vers  string `json:"vers"`
			Cksum string `json:"cksum"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Vers == "" {
			continue
		}
		artifact := &database.ArtifactMetadata{
			ID:              fmt.Sprintf("cargo:%s:%s", name, entry.Vers),
			ArtifactType:    "cargo",
			ArtifactName:    name,
			Version:         entry.Vers,
			Digest:          "sha256:" + entry.Cksum,
			DigestAlgorithm: "sha256",
			Metadata:        map[string]interface{}{"indexLine": line},
			Tags:            []string{entry.Vers},
		}
		if err := p.db.SaveArtifact(artifact); err != nil {
			fmt.Printf("Warning: failed to save crate index entry for %s@%s: %v\n", name, entry.Vers, err)
		}
	}

	return data, nil
}

// fetchCrateFromUpstream downloads a .crate tarball from upstream.
func (p *CargoProxy) fetchCrateFromUpstream(reg *database.RegistryConfig, registryLabel, name, version string) ([]byte, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}

	dlURL := fmt.Sprintf("%s/api/v1/crates/%s/%s/download", strings.TrimSuffix(reg.URL, "/"), name, version)
	req, err := http.NewRequest(http.MethodGet, dlURL, nil)
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

// sparseIndexPath computes the sharded index path for a crate name,
// matching crates.io's own sparse-index layout.
func sparseIndexPath(name string) string {
	lower := strings.ToLower(name)
	switch {
	case len(lower) == 1:
		return "1/" + name
	case len(lower) == 2:
		return "2/" + name
	case len(lower) == 3:
		return "3/" + lower[:1] + "/" + name
	default:
		return lower[:2] + "/" + lower[2:4] + "/" + name
	}
}

// schemeFor returns "https" if the request arrived over TLS or via a
// terminating proxy that says so, else "http".
func schemeFor(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}

// cacheGet retrieves data from cache.
func (p *CargoProxy) cacheGet(key string) ([]byte, error) {
	var data []byte
	err := p.cache.Get(key, &data)
	return data, err
}

// cacheSet stores data in cache.
func (p *CargoProxy) cacheSet(key string, data []byte) {
	p.cache.Set(key, data)
}
