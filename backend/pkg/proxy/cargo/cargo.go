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
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	proxypkg "github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
)

// CargoProxy implements the Cargo sparse registry protocol proxy.
type CargoProxy struct {
	db         *database.Database
	storage    storage.StorageAdapter
	cache      *cache.Cache
	rbacMgr    *rbac.RBAC
	registries []database.RegistryConfig
}

// NewCargoProxy creates a new Cargo proxy instance.
func NewCargoProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &CargoProxy{db: db, storage: storage, cache: cache, rbacMgr: rbacMgr, registries: registries}

	r.Group(func(r chi.Router) {
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
	})

	// Publish ("cargo publish"): a PUT of the length-prefixed JSON+crate
	// binary body described at
	// https://doc.rust-lang.org/cargo/reference/registry-web-api.html#publish.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)
		r.Put("/api/v1/crates/new", proxy.handlePublish)
	})

	return r
}

// checkPublishAccess resolves the registry addressed by r's Host header and
// enforces publish access on it, writing a 401/403 and returning ok=false if
// denied.
func (p *CargoProxy) checkPublishAccess(w http.ResponseWriter, r *http.Request) (proxypkg.Target, bool) {
	reg := proxypkg.ResolveRegistry(p.db, p.registries, r.Host, "cargo")
	label := "cargo"
	if reg != nil {
		label = reg.ID
	}
	t := proxypkg.Target{Reg: reg, Label: label}
	return t, proxypkg.CheckAccess(w, p.rbacMgr, middleware.GetUser(r), reg, true)
}

// cargoPublishMetadata is the subset of the publish JSON body needed to
// store and index the crate. See
// https://doc.rust-lang.org/cargo/reference/registry-web-api.html#publish
// for the full schema.
type cargoPublishMetadata struct {
	Name string        `json:"name"`
	Vers string        `json:"vers"`
	Deps []interface{} `json:"deps"`
}

// readU32LE reads a 32-bit little-endian length prefix, as used throughout
// the publish wire format.
func readU32LE(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

// handlePublish handles "cargo publish": a PUT body consisting of a
// length-prefixed JSON metadata object followed by a length-prefixed .crate
// tarball.
func (p *CargoProxy) handlePublish(w http.ResponseWriter, r *http.Request) {
	t, ok := p.checkPublishAccess(w, r)
	if !ok {
		return
	}

	jsonLen, err := readU32LE(r.Body)
	if err != nil {
		http.Error(w, "Malformed publish request: missing metadata length", http.StatusBadRequest)
		return
	}
	jsonBytes := make([]byte, jsonLen)
	if _, err := io.ReadFull(r.Body, jsonBytes); err != nil {
		http.Error(w, "Malformed publish request: truncated metadata", http.StatusBadRequest)
		return
	}
	var meta cargoPublishMetadata
	if err := json.Unmarshal(jsonBytes, &meta); err != nil {
		http.Error(w, fmt.Sprintf("Malformed publish metadata: %v", err), http.StatusBadRequest)
		return
	}
	if meta.Name == "" || meta.Vers == "" {
		http.Error(w, "Missing required field: name and vers are required", http.StatusBadRequest)
		return
	}

	crateLen, err := readU32LE(r.Body)
	if err != nil {
		http.Error(w, "Malformed publish request: missing crate length", http.StatusBadRequest)
		return
	}

	hasher := sha256.New()
	limited := io.LimitReader(r.Body, int64(crateLen))
	if _, err := p.storage.SaveArtifactStream(t.Label, "", meta.Name, meta.Vers, io.TeeReader(limited, hasher)); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save crate: %v", err), http.StatusInternalServerError)
		return
	}
	digest := hex.EncodeToString(hasher.Sum(nil))

	if meta.Deps == nil {
		meta.Deps = []interface{}{}
	}
	indexEntry := map[string]interface{}{
		"name":     meta.Name,
		"vers":     meta.Vers,
		"deps":     meta.Deps,
		"cksum":    digest,
		"features": map[string]interface{}{},
		"yanked":   false,
	}
	indexLine, _ := json.Marshal(indexEntry)

	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("cargo:%s:%s:%s", t.Label, meta.Name, meta.Vers),
		RegistryID:      t.Label,
		ArtifactType:    "cargo",
		ArtifactName:    meta.Name,
		Version:         meta.Vers,
		Digest:          "sha256:" + digest,
		DigestAlgorithm: "sha256",
		Size:            int64(crateLen),
		Metadata:        map[string]interface{}{"indexLine": string(indexLine)},
		Tags:            []string{meta.Vers},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save crate metadata: %v", err), http.StatusInternalServerError)
		return
	}

	p.cache.Delete(fmt.Sprintf("cargo:index:%s", meta.Name))

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{}`))
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
		data, err := p.fetchIndexFromUpstream(t.Reg, t.Label, name)
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

	if rc, err := p.storage.GetArtifactStream(t.Label, "", name, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.crate", name, version))
		if err := p.db.IncrementArtifactDownloads(t.Label, "", name, version); err != nil {
			fmt.Printf("failed to record download for %s@%s: %v\n", name, version, err)
		}
		io.Copy(w, rc)
		return
	}

	resp, err := p.fetchCrateStreamFromUpstream(t.Reg, name, version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download crate: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, "", name, version, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save crate: %v", err), http.StatusInternalServerError)
		return
	}
	rc, err := p.storage.GetArtifactStream(t.Label, "", name, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve crate", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.crate", name, version))
	if err := p.db.IncrementArtifactDownloads(t.Label, "", name, version); err != nil {
		fmt.Printf("failed to record download for %s@%s: %v\n", name, version, err)
	}
	io.Copy(w, rc)
}

// fetchIndexFromUpstream fetches a crate's sparse index file from upstream
// and saves one artifact record per version line so subsequent index and
// download requests are served from the database/storage cache.
func (p *CargoProxy) fetchIndexFromUpstream(reg *database.RegistryConfig, registryLabel, name string) ([]byte, error) {
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
			RegistryID:      registryLabel,
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

// fetchCrateStreamFromUpstream downloads a .crate tarball from upstream,
// returning the live response so it can be streamed straight into storage
// instead of buffered in memory. Callers must close the returned body.
func (p *CargoProxy) fetchCrateStreamFromUpstream(reg *database.RegistryConfig, name, version string) (*http.Response, error) {
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
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	return resp, nil
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
