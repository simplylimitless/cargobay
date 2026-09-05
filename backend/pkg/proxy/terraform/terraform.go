// Package terraform implements the Terraform provider and module registry
// protocols.
//
// This package provides a caching proxy that:
//   - Serves the .well-known/terraform.json service-discovery document
//   - Serves provider version listings and download metadata, caching
//     provider distribution .zip files from upstream
//   - Serves module version listings and download redirects, caching
//     module .tar.gz archives from upstream
//
// Terraform registry protocol: https://developer.hashicorp.com/terraform/internals/provider-registry-protocol
// and https://developer.hashicorp.com/terraform/internals/module-registry-protocol
package terraform

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

// TerraformProxy implements the Terraform provider and module registry
// protocols. Providers are namespaced by "{namespace}"; modules by
// "{namespace}/{system}".
type TerraformProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// NewTerraformProxy creates a new Terraform registry proxy instance.
func NewTerraformProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &TerraformProxy{db: db, storage: storage, cache: cache}

	// Service discovery is unauthenticated per the Terraform spec — clients
	// probe it before knowing whether the registry requires auth.
	r.Get("/.well-known/terraform.json", proxy.handleServiceDiscovery)

	r.Group(func(r chi.Router) {
		r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "terraform"))

		r.Get("/providers/v1/{namespace}/{ptype}/versions", proxy.handleProviderVersions)
		r.Get("/providers/v1/{namespace}/{ptype}/{version}/download/{os}/{arch}", proxy.handleProviderDownload)
		r.Get("/providers/v1/{namespace}/{ptype}/{version}/files/{fileName}", proxy.handleProviderFile)

		r.Get("/modules/v1/{namespace}/{name}/{system}/versions", proxy.handleModuleVersions)
		r.Get("/modules/v1/{namespace}/{name}/{system}/{version}/download", proxy.handleModuleDownloadRedirect)
		r.Get("/modules/v1/{namespace}/{name}/{system}/{version}/archive.tar.gz", proxy.handleModuleArchive)
	})

	return r
}

// handleServiceDiscovery advertises the provider and module protocol paths.
func (p *TerraformProxy) handleServiceDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"providers.v1": "/terraform/providers/v1/",
		"modules.v1":   "/terraform/modules/v1/",
	})
}

// handleProviderVersions serves the known versions for a provider.
func (p *TerraformProxy) handleProviderVersions(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	ptype := chi.URLParam(r, "ptype")
	t := proxypkg.TargetFromContext(r, "terraform")

	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{
		Namespace:    namespace,
		ArtifactType: "terraform",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list versions: %v", err), http.StatusInternalServerError)
		return
	}

	var versions []map[string]interface{}
	for _, a := range artifacts {
		if a.ArtifactName == ptype {
			versions = append(versions, map[string]interface{}{"version": a.Version, "protocols": []string{"5.0"}})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"versions": versions})
}

// handleProviderDownload serves download metadata for one provider
// version/os/arch, pointing download_url at our own cached file endpoint.
func (p *TerraformProxy) handleProviderDownload(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	ptype := chi.URLParam(r, "ptype")
	version := chi.URLParam(r, "version")
	osName := chi.URLParam(r, "os")
	arch := chi.URLParam(r, "arch")

	fileName := fmt.Sprintf("terraform-provider-%s_%s_%s_%s.zip", ptype, version, osName, arch)
	downloadURL := fmt.Sprintf("%s/providers/v1/%s/%s/%s/files/%s", baseURL(r), namespace, ptype, version, fileName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"protocols":    []string{"5.0"},
		"os":           osName,
		"arch":         arch,
		"filename":     fileName,
		"download_url": downloadURL,
		"shasums_url":  "",
	})
}

// handleProviderFile handles the actual provider .zip download.
func (p *TerraformProxy) handleProviderFile(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	ptype := chi.URLParam(r, "ptype")
	version := chi.URLParam(r, "version")
	fileName := chi.URLParam(r, "fileName")
	t := proxypkg.TargetFromContext(r, "terraform")

	if rc, err := p.storage.GetArtifactStream(t.Label, namespace, ptype, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/zip")
		if err := p.db.IncrementArtifactDownloads(t.Label, namespace, ptype, version); err != nil {
			fmt.Printf("failed to record download for %s/%s@%s: %v\n", namespace, ptype, version, err)
		}
		io.Copy(w, rc)
		return
	}

	upstreamPath := fmt.Sprintf("providers/v1/%s/%s/%s/files/%s", namespace, ptype, version, fileName)
	resp, err := p.fetchStreamFromUpstream(t.Reg, upstreamPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download provider: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, namespace, ptype, version, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save provider: %v", err), http.StatusInternalServerError)
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("terraform:%s:%s:%s:%s", t.Label, namespace, ptype, version),
		RegistryID:   t.Label,
		ArtifactType: "terraform",
		Namespace:    namespace,
		ArtifactName: ptype,
		Version:      version,
		Size:         counter.N,
		Metadata:     map[string]interface{}{},
		Tags:         []string{version},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save provider metadata: %v\n", err)
	}

	rc, err := p.storage.GetArtifactStream(t.Label, namespace, ptype, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve provider", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/zip")
	if err := p.db.IncrementArtifactDownloads(t.Label, namespace, ptype, version); err != nil {
		fmt.Printf("failed to record download for %s/%s@%s: %v\n", namespace, ptype, version, err)
	}
	io.Copy(w, rc)
}

// handleModuleVersions serves the known versions for a module.
func (p *TerraformProxy) handleModuleVersions(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	system := chi.URLParam(r, "system")
	moduleNamespace := fmt.Sprintf("%s/%s", namespace, system)
	t := proxypkg.TargetFromContext(r, "terraform")

	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{
		Namespace:    moduleNamespace,
		ArtifactType: "terraform-module",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list versions: %v", err), http.StatusInternalServerError)
		return
	}

	var versions []map[string]string
	for _, a := range artifacts {
		if a.ArtifactName == name {
			versions = append(versions, map[string]string{"version": a.Version})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"modules": []map[string]interface{}{{"versions": versions}},
	})
}

// handleModuleDownloadRedirect responds per the module protocol: a 204
// with an X-Terraform-Get header naming the archive location, here
// pointing at our own cached-archive endpoint.
func (p *TerraformProxy) handleModuleDownloadRedirect(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	system := chi.URLParam(r, "system")
	version := chi.URLParam(r, "version")

	archiveURL := fmt.Sprintf("%s/modules/v1/%s/%s/%s/%s/archive.tar.gz", baseURL(r), namespace, name, system, version)
	w.Header().Set("X-Terraform-Get", archiveURL)
	w.WriteHeader(http.StatusNoContent)
}

// handleModuleArchive handles the actual module .tar.gz download.
func (p *TerraformProxy) handleModuleArchive(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	system := chi.URLParam(r, "system")
	version := chi.URLParam(r, "version")
	moduleNamespace := fmt.Sprintf("%s/%s", namespace, system)
	t := proxypkg.TargetFromContext(r, "terraform")

	if rc, err := p.storage.GetArtifactStream(t.Label, moduleNamespace, name, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/gzip")
		if err := p.db.IncrementArtifactDownloads(t.Label, moduleNamespace, name, version); err != nil {
			fmt.Printf("failed to record download for %s/%s@%s: %v\n", moduleNamespace, name, version, err)
		}
		io.Copy(w, rc)
		return
	}

	upstreamPath := fmt.Sprintf("modules/v1/%s/%s/%s/%s/archive.tar.gz", namespace, name, system, version)
	resp, err := p.fetchStreamFromUpstream(t.Reg, upstreamPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download module: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, moduleNamespace, name, version, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save module: %v", err), http.StatusInternalServerError)
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("terraform-module:%s:%s:%s:%s", t.Label, moduleNamespace, name, version),
		RegistryID:   t.Label,
		ArtifactType: "terraform-module",
		Namespace:    moduleNamespace,
		ArtifactName: name,
		Version:      version,
		Size:         counter.N,
		Metadata:     map[string]interface{}{},
		Tags:         []string{version},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save module metadata: %v\n", err)
	}

	rc, err := p.storage.GetArtifactStream(t.Label, moduleNamespace, name, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve module", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/gzip")
	if err := p.db.IncrementArtifactDownloads(t.Label, moduleNamespace, name, version); err != nil {
		fmt.Printf("failed to record download for %s/%s@%s: %v\n", moduleNamespace, name, version, err)
	}
	io.Copy(w, rc)
}

func (p *TerraformProxy) fetchFromUpstream(reg *database.RegistryConfig, path string) ([]byte, error) {
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

// fetchStreamFromUpstream is like fetchFromUpstream but returns the live
// response so large artifacts (provider binaries, module archives) can be
// streamed straight into storage instead of buffered in memory. Callers
// must close the returned body.
func (p *TerraformProxy) fetchStreamFromUpstream(reg *database.RegistryConfig, path string) (*http.Response, error) {
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

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}
