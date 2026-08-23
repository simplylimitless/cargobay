// Package debian implements a caching APT repository proxy.
//
// This package provides a caching proxy for Debian packages that:
//   - Generates dists/{dist}/main/binary-{arch}/Packages(.gz) on demand
//     from cached artifact metadata
//   - Caches .deb packages from an upstream APT repository pool
//
// APT repository format: https://wiki.debian.org/DebianRepository/Format
package debian

import (
	"bytes"
	"compress/gzip"
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

// DebianProxy implements an APT repository proxy. Packages are namespaced
// by architecture (e.g. "amd64", "arm64"), matching APT's own
// per-architecture Packages index.
type DebianProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// NewDebianProxy creates a new Debian proxy instance.
func NewDebianProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &DebianProxy{db: db, storage: storage, cache: cache}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "debian"))

	r.Get("/dists/{dist}/Release", proxy.handleRelease)
	r.Get("/dists/{dist}/main/binary-{arch}/Packages", proxy.handlePackages)
	r.Get("/dists/{dist}/main/binary-{arch}/Packages.gz", proxy.handlePackagesGz)
	r.Get("/pool/{fileName}", proxy.handlePool)

	return r
}

// handleRelease serves a minimal Release file — enough for apt to accept
// the repository as unsigned (Cargobay does not sign indices; users must
// configure `[trusted=yes]` for this source, same as any other unsigned
// local mirror).
func (p *DebianProxy) handleRelease(w http.ResponseWriter, r *http.Request) {
	dist := chi.URLParam(r, "dist")
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "Origin: Cargobay\nLabel: Cargobay\nSuite: %s\nCodename: %s\nComponents: main\nArchitectures: amd64 arm64\n", dist, dist)
}

// handlePackages serves the plaintext Packages index for one architecture.
func (p *DebianProxy) handlePackages(w http.ResponseWriter, r *http.Request) {
	arch := chi.URLParam(r, "arch")
	data, err := p.buildPackagesIndex(r, arch)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build index: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write(data)
}

// handlePackagesGz serves the same index, gzip-compressed (the form most
// apt clients prefer for bandwidth).
func (p *DebianProxy) handlePackagesGz(w http.ResponseWriter, r *http.Request) {
	arch := chi.URLParam(r, "arch")
	data, err := p.buildPackagesIndex(r, arch)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build index: %v", err), http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write(data)
	gw.Close()

	w.Header().Set("Content-Type", "application/gzip")
	w.Write(buf.Bytes())
}

func (p *DebianProxy) buildPackagesIndex(r *http.Request, arch string) ([]byte, error) {
	t := proxypkg.TargetFromContext(r, "debian")
	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{
		Namespace:    arch,
		ArtifactType: "debian",
	})
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	for _, a := range artifacts {
		fmt.Fprintf(&buf, "Package: %s\n", a.ArtifactName)
		fmt.Fprintf(&buf, "Version: %s\n", a.Version)
		fmt.Fprintf(&buf, "Architecture: %s\n", a.Namespace)
		fmt.Fprintf(&buf, "Filename: pool/%s_%s_%s.deb\n", a.ArtifactName, a.Version, a.Namespace)
		fmt.Fprintf(&buf, "Size: %d\n", a.Size)
		if a.Digest != "" {
			fmt.Fprintf(&buf, "SHA256: %s\n", strings.TrimPrefix(a.Digest, "sha256:"))
		}
		buf.WriteString("\n")
	}
	return buf.Bytes(), nil
}

// handlePool handles .deb package downloads, addressed by their pool
// filename ("{name}_{version}_{arch}.deb").
func (p *DebianProxy) handlePool(w http.ResponseWriter, r *http.Request) {
	fileName := chi.URLParam(r, "fileName")
	name, version, arch, ok := splitDebFileName(fileName)
	if !ok {
		http.NotFound(w, r)
		return
	}

	t := proxypkg.TargetFromContext(r, "debian")

	if data, err := p.storage.GetArtifact(t.Label, arch, name, version); err == nil && data != nil {
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
		if err := p.db.IncrementArtifactDownloads(t.Label, arch, name, version); err != nil {
			fmt.Printf("failed to record download for %s_%s_%s: %v\n", name, version, arch, err)
		}
		w.Write(data)
		return
	}

	data, err := p.fetchFromUpstream(t.Reg, fileName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download package: %v", err), http.StatusBadGateway)
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("debian:%s:%s:%s:%s", t.Label, arch, name, version),
		RegistryID:      t.Label,
		ArtifactType:    "debian",
		Namespace:       arch,
		ArtifactName:    name,
		Version:         version,
		DigestAlgorithm: "sha256",
		Size:            int64(len(data)),
		Metadata:        map[string]interface{}{},
		Tags:            []string{version},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save package metadata: %v\n", err)
	}
	if _, err := p.storage.SaveArtifact(t.Label, arch, name, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save package: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	if err := p.db.IncrementArtifactDownloads(t.Label, arch, name, version); err != nil {
		fmt.Printf("failed to record download for %s_%s_%s: %v\n", name, version, arch, err)
	}
	w.Write(data)
}

func (p *DebianProxy) fetchFromUpstream(reg *database.RegistryConfig, fileName string) ([]byte, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}
	url := fmt.Sprintf("%s/pool/%s", strings.TrimSuffix(reg.URL, "/"), fileName)
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

// splitDebFileName splits "name_version_arch.deb" into its three parts.
func splitDebFileName(fileName string) (name, version, arch string, ok bool) {
	base := strings.TrimSuffix(fileName, ".deb")
	parts := strings.Split(base, "_")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}
