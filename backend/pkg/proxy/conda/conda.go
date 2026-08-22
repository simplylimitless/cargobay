// Package conda implements a caching Conda channel repository proxy.
//
// This package provides a caching proxy for conda packages that:
//   - Generates repodata.json on demand from cached artifact metadata,
//     for a given channel/subdir (e.g. "conda-forge/linux-64")
//   - Caches .tar.bz2 / .conda package files from an upstream channel
//
// Conda repository format: https://docs.conda.io/projects/conda-build/en/latest/concepts/generating-index.html
package conda

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

// CondaProxy implements the conda channel repository protocol. Packages are
// namespaced by "{channel}/{subdir}" (e.g. "conda-forge/linux-64").
type CondaProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// NewCondaProxy creates a new Conda channel proxy instance.
func NewCondaProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &CondaProxy{db: db, storage: storage, cache: cache}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "conda"))

	r.Get("/{channel}/{subdir}/repodata.json", proxy.handleRepodata)
	r.Get("/{channel}/{subdir}/{fileName}", proxy.handlePackage)

	return r
}

type condaPackageEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Build   string `json:"build"`
	Size    int64  `json:"size"`
}

// handleRepodata generates repodata.json from cached package metadata for
// the requested channel/subdir.
func (p *CondaProxy) handleRepodata(w http.ResponseWriter, r *http.Request) {
	channel := chi.URLParam(r, "channel")
	subdir := chi.URLParam(r, "subdir")
	namespace := fmt.Sprintf("%s/%s", channel, subdir)
	t := proxypkg.TargetFromContext(r, "conda")

	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{
		Namespace:    namespace,
		ArtifactType: "conda",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list packages: %v", err), http.StatusInternalServerError)
		return
	}

	packages := map[string]condaPackageEntry{}
	packagesConda := map[string]condaPackageEntry{}
	for _, a := range artifacts {
		version, build, ext := splitCondaVersionKey(a.Version)
		fileName := fmt.Sprintf("%s-%s-%s%s", a.ArtifactName, version, build, ext)
		entry := condaPackageEntry{Name: a.ArtifactName, Version: version, Build: build, Size: a.Size}
		if ext == ".conda" {
			packagesConda[fileName] = entry
		} else {
			packages[fileName] = entry
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"info":           map[string]string{"subdir": subdir},
		"packages":       packages,
		"packages.conda": packagesConda,
	})
}

// handlePackage handles .tar.bz2 / .conda package downloads.
func (p *CondaProxy) handlePackage(w http.ResponseWriter, r *http.Request) {
	channel := chi.URLParam(r, "channel")
	subdir := chi.URLParam(r, "subdir")
	fileName := chi.URLParam(r, "fileName")
	namespace := fmt.Sprintf("%s/%s", channel, subdir)

	name, versionKey, ok := splitCondaFileName(fileName)
	if !ok {
		http.NotFound(w, r)
		return
	}

	t := proxypkg.TargetFromContext(r, "conda")

	if data, err := p.storage.GetArtifact("conda", namespace, name, versionKey); err == nil && data != nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
		if err := p.db.IncrementArtifactDownloads(t.Label, namespace, name, versionKey); err != nil {
			fmt.Printf("failed to record download for %s: %v\n", fileName, err)
		}
		w.Write(data)
		return
	}

	upstreamPath := fmt.Sprintf("%s/%s/%s", channel, subdir, fileName)
	data, err := p.fetchFromUpstream(t.Reg, upstreamPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download package: %v", err), http.StatusBadGateway)
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("conda:%s:%s:%s:%s", t.Label, namespace, name, versionKey),
		RegistryID:      t.Label,
		ArtifactType:    "conda",
		Namespace:       namespace,
		ArtifactName:    name,
		Version:         versionKey,
		DigestAlgorithm: "sha256",
		Size:            int64(len(data)),
		Metadata:        map[string]interface{}{},
		Tags:            []string{versionKey},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save package metadata: %v\n", err)
	}
	if _, err := p.storage.SaveArtifact("conda", namespace, name, versionKey, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save package: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	if err := p.db.IncrementArtifactDownloads(t.Label, namespace, name, versionKey); err != nil {
		fmt.Printf("failed to record download for %s: %v\n", fileName, err)
	}
	w.Write(data)
}

func (p *CondaProxy) fetchFromUpstream(reg *database.RegistryConfig, path string) ([]byte, error) {
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

// splitCondaFileName splits "name-version-build.tar.bz2" (or ".conda") into
// ("name", "version::build::ext"), the storage/version key used to
// disambiguate the (version, build string) pair conda packages need.
func splitCondaFileName(fileName string) (name, versionKey string, ok bool) {
	ext := ".tar.bz2"
	base := strings.TrimSuffix(fileName, ext)
	if base == fileName {
		ext = ".conda"
		base = strings.TrimSuffix(fileName, ext)
		if base == fileName {
			return "", "", false
		}
	}

	parts := strings.Split(base, "-")
	if len(parts) < 3 {
		return "", "", false
	}
	build := parts[len(parts)-1]
	version := parts[len(parts)-2]
	name = strings.Join(parts[:len(parts)-2], "-")
	return name, fmt.Sprintf("%s::%s::%s", version, build, ext), true
}

// splitCondaVersionKey reverses the encoding produced by splitCondaFileName,
// falling back to treating the whole key as a bare version with no
// build/extension if it wasn't produced by this proxy (e.g. legacy data).
func splitCondaVersionKey(versionKey string) (version, build, ext string) {
	parts := strings.SplitN(versionKey, "::", 3)
	if len(parts) != 3 {
		return versionKey, "", ".tar.bz2"
	}
	return parts[0], parts[1], parts[2]
}
