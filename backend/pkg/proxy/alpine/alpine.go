// Package alpine implements the Alpine (apk) package repository proxy.
//
// This package provides a caching proxy for apk packages that:
//   - Generates APKINDEX.tar.gz on demand from cached artifact metadata
//   - Caches .apk packages from an upstream Alpine repository
//
// apk repository layout: https://wiki.alpinelinux.org/wiki/Apk_spec
package alpine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	proxypkg "github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/go-chi/chi/v5"
)

// AlpineProxy implements the apk repository protocol proxy. Packages are
// namespaced by architecture (e.g. "x86_64", "aarch64"), matching apk's own
// per-arch repository layout.
type AlpineProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// NewAlpineProxy creates a new Alpine proxy instance.
func NewAlpineProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &AlpineProxy{db: db, storage: storage, cache: cache}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "alpine"))

	r.Get("/{arch}/APKINDEX.tar.gz", proxy.handleIndex)
	r.Get("/{arch}/{fileName}", proxy.handlePackage)

	return r
}

// handleIndex generates APKINDEX.tar.gz from cached package metadata for
// the requested architecture.
func (p *AlpineProxy) handleIndex(w http.ResponseWriter, r *http.Request) {
	arch := chi.URLParam(r, "arch")
	t := proxypkg.TargetFromContext(r, "alpine")

	cacheKey := fmt.Sprintf("alpine:index:%s", arch)
	if data, err := p.cacheGet(cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(data)
		return
	}

	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{
		Namespace:    arch,
		ArtifactType: "alpine",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list packages: %v", err), http.StatusInternalServerError)
		return
	}

	data, err := buildAPKIndex(artifacts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build index: %v", err), http.StatusInternalServerError)
		return
	}

	p.cacheSet(cacheKey, data)
	w.Header().Set("Content-Type", "application/gzip")
	w.Write(data)
}

// buildAPKIndex builds an APKINDEX.tar.gz from artifact metadata, following
// the plain-text "K:V" record format apk itself generates.
func buildAPKIndex(artifacts []database.ArtifactMetadata) ([]byte, error) {
	var indexBuf bytes.Buffer
	for _, a := range artifacts {
		fmt.Fprintf(&indexBuf, "P:%s\n", a.ArtifactName)
		fmt.Fprintf(&indexBuf, "V:%s\n", a.Version)
		fmt.Fprintf(&indexBuf, "A:%s\n", a.Namespace)
		fmt.Fprintf(&indexBuf, "S:%d\n", a.Size)
		if a.Digest != "" {
			fmt.Fprintf(&indexBuf, "C:%s\n", strings.TrimPrefix(a.Digest, "sha256:"))
		}
		indexBuf.WriteString("\n")
	}

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	hdr := &tar.Header{
		Name: "APKINDEX",
		Mode: 0644,
		Size: int64(indexBuf.Len()),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := tw.Write(indexBuf.Bytes()); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	if _, err := gw.Write(tarBuf.Bytes()); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return gzBuf.Bytes(), nil
}

// handlePackage handles .apk package downloads.
func (p *AlpineProxy) handlePackage(w http.ResponseWriter, r *http.Request) {
	arch := chi.URLParam(r, "arch")
	fileName := chi.URLParam(r, "fileName")
	if !strings.HasSuffix(fileName, ".apk") {
		http.NotFound(w, r)
		return
	}
	pkgName, version := splitAPKFileName(fileName)
	if pkgName == "" {
		http.NotFound(w, r)
		return
	}

	t := proxypkg.TargetFromContext(r, "alpine")

	if rc, err := p.storage.GetArtifactStream(t.Label, arch, pkgName, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/vnd.alpine.apk")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
		if err := p.db.IncrementArtifactDownloads(t.Label, arch, pkgName, version); err != nil {
			fmt.Printf("failed to record download for %s-%s (%s): %v\n", pkgName, version, arch, err)
		}
		io.Copy(w, rc)
		return
	}

	resp, err := p.fetchStreamFromUpstream(t.Reg, arch, fileName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download package: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, arch, pkgName, version, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save package: %v", err), http.StatusInternalServerError)
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("alpine:%s:%s:%s:%s", t.Label, arch, pkgName, version),
		RegistryID:      t.Label,
		ArtifactType:    "alpine",
		Namespace:       arch,
		ArtifactName:    pkgName,
		Version:         version,
		DigestAlgorithm: "sha256",
		Size:            counter.N,
		Metadata:        map[string]interface{}{},
		Tags:            []string{version},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save package metadata: %v\n", err)
	}

	rc, err := p.storage.GetArtifactStream(t.Label, arch, pkgName, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve package", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/vnd.alpine.apk")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	if err := p.db.IncrementArtifactDownloads(t.Label, arch, pkgName, version); err != nil {
		fmt.Printf("failed to record download for %s-%s (%s): %v\n", pkgName, version, arch, err)
	}
	io.Copy(w, rc)
}

// fetchStreamFromUpstream downloads a .apk file from upstream, returning the
// live response so it can be streamed straight into storage. Callers must
// close the returned body.
func (p *AlpineProxy) fetchStreamFromUpstream(reg *database.RegistryConfig, arch, fileName string) (*http.Response, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}
	url := fmt.Sprintf("%s/%s/%s", strings.TrimSuffix(reg.URL, "/"), arch, fileName)
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

// splitAPKFileName splits "name-1.2.3-r0.apk" into ("name", "1.2.3-r0").
// apk package filenames are "{name}-{version}-r{release}.apk", where the
// version segment itself starts with a digit — this is the same convention
// apk's own tooling relies on.
func splitAPKFileName(fileName string) (name, version string) {
	base := strings.TrimSuffix(fileName, ".apk")
	parts := strings.Split(base, "-")
	for i := len(parts) - 1; i > 0; i-- {
		if len(parts[i]) > 0 {
			if _, err := strconv.Atoi(parts[i][:1]); err == nil {
				return strings.Join(parts[:i], "-"), strings.Join(parts[i:], "-")
			}
		}
	}
	return "", ""
}

// cacheGet retrieves data from cache.
func (p *AlpineProxy) cacheGet(key string) ([]byte, error) {
	var data []byte
	err := p.cache.Get(key, &data)
	return data, err
}

// cacheSet stores data in cache.
func (p *AlpineProxy) cacheSet(key string, data []byte) {
	p.cache.Set(key, data)
}
