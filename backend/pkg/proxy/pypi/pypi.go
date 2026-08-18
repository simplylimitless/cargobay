// Package pypi implements the PyPI Simple API proxy
//
// This package provides a caching proxy for Python packages that:
//   - Implements PyPI Simple API (PEP 503)
//   - Caches packages from pypi.org
//   - Handles wheel (.whl) and source distributions (.tar.gz, .zip)
//   - Supports package search and listing
//
// PyPI Simple API: https://peps.python.org/pep-0503/
package pypi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	proxypkg "github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/go-chi/chi/v5"
)

// PyPIProxy implements the PyPI Simple API proxy
type PyPIProxy struct {
	db         *database.Database
	storage    storage.StorageAdapter
	cache      *cache.Cache
	registries []database.RegistryConfig
	registry   string
}

// NewPyPIProxy creates a new PyPI proxy instance
func NewPyPIProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &PyPIProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "pypi",
	}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "pypi"))

	// PyPI Simple API routes
	// Root: /simple/ - lists all available packages
	r.Get("/simple/", proxy.handleSimpleRoot)
	r.Get("/simple", proxy.handleSimpleRoot)

	// Package index: /simple/{package}/ - lists versions of a package
	r.Get("/simple/{packageName}/", proxy.handlePackageIndex)
	r.Get("/simple/{packageName}", proxy.handlePackageIndex)

	// Package detail: /simple/{package}/{version}/ - shows package info
	r.Get("/simple/{packageName}/{version}/", proxy.handlePackageVersion)

	// Download routes
	// Wheel: /packages/{package}/{version}/{package}-{version}-{python}-{abi}-{platform}.whl
	r.Get("/packages/{packageName}/{version}/{fileName}", proxy.handlePackageFile)

	// Legacy redirect support
	r.Get("/{packageName}/", proxy.handleLegacyPackage)
	r.Get("/{packageName}/{version}/", proxy.handleLegacyPackageVersion)

	return r
}

// handleSimpleRoot returns the root of the simple API (list of packages)
func (p *PyPIProxy) handleSimpleRoot(w http.ResponseWriter, r *http.Request) {
	// Try cache first
	if data, err := p.cacheGet("-simple-root-"); err == nil {
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
		return
	}

	// Get packages from database
	artifacts, err := p.db.ListArtifacts(proxypkg.TargetFromContext(r, "pypi").Label, database.ListOptions{
		ArtifactType: "pypi",
		Limit:        1000,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list packages: %v", err), http.StatusBadGateway)
		return
	}

	// Build package list with deduplication
	packages := make(map[string]bool)
	for _, a := range artifacts {
		packages[a.ArtifactName] = true
	}

	// Generate HTML index (PyPI Simple API format)
	html := p.generateSimpleIndex(packages)
	p.cacheSet("-simple-root-", []byte(html))

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// handlePackageIndex returns the index for a specific package
func (p *PyPIProxy) handlePackageIndex(w http.ResponseWriter, r *http.Request) {
	packageName := chi.URLParam(r, "packageName")

	// Try cache first
	cacheKey := fmt.Sprintf("-simple-%s-", packageName)
	if data, err := p.cacheGet(cacheKey); err == nil {
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
		return
	}

	// Get versions from database
	artifacts, err := p.db.ListArtifacts(proxypkg.TargetFromContext(r, "pypi").Label, database.ListOptions{
		ArtifactType: "pypi",
		Limit:        1000,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list package versions: %v", err), http.StatusBadGateway)
		return
	}

	// Filter by package name (database filtering is not available in ListArtifacts)
	versions := make(map[string]bool)
	for _, a := range artifacts {
		if a.ArtifactName == packageName {
			versions[a.Version] = true
		}
	}

	// Generate HTML index
	html := p.generatePackageIndex(packageName, versions)
	p.cacheSet(cacheKey, []byte(html))

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// handlePackageVersion handles package version detail
func (p *PyPIProxy) handlePackageVersion(w http.ResponseWriter, r *http.Request) {
	packageName := chi.URLParam(r, "packageName")
	version := chi.URLParam(r, "version")

	// Try to get artifact metadata
	artifact, err := p.db.GetArtifactByParams("pypi", "", packageName, version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get package: %v", err), http.StatusBadGateway)
		return
	}

	// If not found in database, fetch from upstream
	if artifact == nil {
		t := proxypkg.TargetFromContext(r, "pypi")
		artifact, err = p.fetchPackageFromUpstream(t.Reg, t.Label, packageName, version)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch package: %v", err), http.StatusBadGateway)
			return
		}
	}

	// Generate HTML page
	html := p.generatePackageVersionHTML(packageName, version, artifact)
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// handlePackageFile handles package file downloads
func (p *PyPIProxy) handlePackageFile(w http.ResponseWriter, r *http.Request) {
	packageName := chi.URLParam(r, "packageName")
	version := chi.URLParam(r, "version")
	fileName := chi.URLParam(r, "fileName")

	// Try to get from storage first
	data, err := p.storage.GetArtifact("pypi", "", packageName, version)
	if err == nil && data != nil {
		// Determine content type based on file extension
		contentType := "application/octet-stream"
		if strings.HasSuffix(fileName, ".whl") {
			contentType = "application/zip"
		} else if strings.HasSuffix(fileName, ".tar.gz") {
			contentType = "application/gzip"
		} else if strings.HasSuffix(fileName, ".zip") {
			contentType = "application/zip"
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
		w.Write(data)
		return
	}

	// Fetch from upstream
	data, err = p.fetchPackageFileFromUpstream(proxypkg.TargetFromContext(r, "pypi").Reg, packageName, version, fileName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download package: %v", err), http.StatusBadGateway)
		return
	}

	// Save to storage
	if _, err = p.storage.SaveArtifact("pypi", "", packageName, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	// Save to cache
	p.cacheSet(fmt.Sprintf("file:%s:%s:%s", packageName, version, fileName), data)

	// Determine content type
	contentType := "application/octet-stream"
	if strings.HasSuffix(fileName, ".whl") {
		contentType = "application/zip"
	} else if strings.HasSuffix(fileName, ".tar.gz") {
		contentType = "application/gzip"
	} else if strings.HasSuffix(fileName, ".zip") {
		contentType = "application/zip"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	w.Write(data)
}

// handleLegacyPackage handles legacy package routes
func (p *PyPIProxy) handleLegacyPackage(w http.ResponseWriter, r *http.Request) {
	packageName := chi.URLParam(r, "packageName")
	http.Redirect(w, r, fmt.Sprintf("/simple/%s/", packageName), http.StatusMovedPermanently)
}

// handleLegacyPackageVersion handles legacy package version routes
func (p *PyPIProxy) handleLegacyPackageVersion(w http.ResponseWriter, r *http.Request) {
	packageName := chi.URLParam(r, "packageName")
	version := chi.URLParam(r, "version")
	http.Redirect(w, r, fmt.Sprintf("/simple/%s/%s/", packageName, version), http.StatusMovedPermanently)
}

// fetchPackageFromUpstream fetches package metadata from PyPI
func (p *PyPIProxy) fetchPackageFromUpstream(reg *database.RegistryConfig, registryLabel, packageName, version string) (*database.ArtifactMetadata, error) {
	upstream := "https://pypi.org"
	if reg != nil && reg.Proxy && reg.URL != "" {
		upstream = reg.URL
	}

	// Get package info from PyPI API
	infoURL := fmt.Sprintf("%s/pypi/%s/json", upstream, packageName)
	infoReq, err := http.NewRequest(http.MethodGet, infoURL, nil)
	if err != nil {
		return nil, err
	}
	proxypkg.ApplyUpstreamAuth(infoReq, reg)
	resp, err := http.DefaultClient.Do(infoReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Get release files
	releases, ok := result["releases"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no releases in response")
	}

	versionFiles, hasVersion := releases[version]
	if !hasVersion {
		return nil, fmt.Errorf("version %s not found", version)
	}

	files, ok := versionFiles.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid files format")
	}

	// Create artifact metadata
	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("pypi:%s:%s:%s", registryLabel, packageName, version),
		RegistryID:      registryLabel,
		ArtifactType:    "pypi",
		Namespace:       "",
		ArtifactName:    packageName,
		Version:         version,
		DigestAlgorithm: "sha256",
		Size:            0,
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata:        result,
		Tags:            []string{version},
	}

	// Collect digests from files
	digests := []string{}
	for _, f := range files {
		if fileMap, ok := f.(map[string]interface{}); ok {
			if digest, ok := fileMap["digests"].(map[string]interface{})["sha256"].(string); ok {
				digests = append(digests, digest)
			}
			if size, ok := fileMap["size"].(float64); ok {
				artifact.Size = int64(size)
			}
		}
	}

	if len(digests) > 0 {
		artifact.Digest = digests[0]
	}

	// Save to database
	p.db.SaveArtifact(artifact)

	return artifact, nil
}

// fetchPackageFileFromUpstream fetches a package file from PyPI
func (p *PyPIProxy) fetchPackageFileFromUpstream(reg *database.RegistryConfig, packageName, version, fileName string) ([]byte, error) {
	upstream := "https://files.pythonhosted.org"
	if reg != nil && reg.Proxy && strings.HasPrefix(reg.URL, "http") {
		upstream = reg.URL
	}

	// PyPI file URL format
	fileURL := fmt.Sprintf("%s/packages/%s/%s/%s", upstream, packageName[0:1], packageName, fileName)
	fileReq, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	proxypkg.ApplyUpstreamAuth(fileReq, reg)
	resp, err := http.DefaultClient.Do(fileReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			break
		}
	}

	return data, nil
}

// generateSimpleIndex generates the HTML index for the simple root
func (p *PyPIProxy) generateSimpleIndex(packages map[string]bool) string {
	html := `<!DOCTYPE html>
<html>
<head><title>Simple Index</title></head>
<body>
<h1>Simple Index</h1>
<ul>`
	for pkg := range packages {
		html += fmt.Sprintf(`<li><a href="/simple/%s/">%s</a></li>`, pkg, pkg)
	}
	html += `
</ul>
</body>
</html>`
	return html
}

// generatePackageIndex generates the HTML index for a package
func (p *PyPIProxy) generatePackageIndex(packageName string, versions map[string]bool) string {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Links for %s</title></head>
<body>
<h1>Links for %s</h1>
<ul>`, packageName, packageName)

	for ver := range versions {
		html += fmt.Sprintf(`<li><a href="/simple/%s/%s/">%s-%s</a></li>`, packageName, ver, packageName, ver)
	}
	html += `
</ul>
</body>
</html>`
	return html
}

// generatePackageVersionHTML generates the HTML for a package version
func (p *PyPIProxy) generatePackageVersionHTML(packageName, version string, artifact *database.ArtifactMetadata) string {
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Links for %s %s</title></head>
<body>
<h1>Links for %s %s</h1>
<ul>`, packageName, version, packageName, version)

	// Generate download links for different file types
	files := []string{
		fmt.Sprintf("%s-%s-py3-none-any.whl", packageName, version),
		fmt.Sprintf("%s-%s.tar.gz", packageName, version),
	}

	for _, f := range files {
		html += fmt.Sprintf(`<li><a href="/packages/%s/%s/%s">%s</a></li>`, packageName, version, f, f)
	}

	html += `
</ul>
</body>
</html>`
	return html
}

// cacheGet retrieves data from cache
func (p *PyPIProxy) cacheGet(key string) ([]byte, error) {
	var data []byte
	err := p.cache.Get(key, &data)
	return data, err
}

// cacheSet stores data in cache
func (p *PyPIProxy) cacheSet(key string, data []byte) {
	p.cache.Set(key, data)
}
