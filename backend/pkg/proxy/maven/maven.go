// Package maven implements the Maven Repository API proxy
//
// This package provides a caching proxy for Maven/Gradle artifacts that:
//   - Implements Maven Repository Layout
//   - Caches JARs, POMs, and other Maven artifacts
//   - Generates maven-metadata.xml dynamically
//   - Handles snapshot and release versions
//
// Maven Repository Layout: https://maven.apache.org/repositories/layout.html
package maven

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

// MavenProxy implements the Maven Repository API proxy
type MavenProxy struct {
	db       *database.Database
	storage  storage.StorageAdapter
	cache    *cache.Cache
	registries []database.RegistryConfig
	registry string
}

// NewMavenProxy creates a new Maven proxy instance
func NewMavenProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	return newMavenProxy(db, storage, cache, rbacMgr, registries, "maven")
}

// NewMavenAliasProxy mounts the same Maven-layout proxy under a different
// registry type — Gradle and SBT both consume standard Maven-layout
// repositories, so they need no protocol differences, only their own
// registry `Type` label for resolution/RBAC. Cached artifacts are still
// stored and listed as plain "maven" artifacts, since the on-disk layout
// and metadata are identical regardless of which client fetched them.
func NewMavenAliasProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig, artifactType string) chi.Router {
	return newMavenProxy(db, storage, cache, rbacMgr, registries, artifactType)
}

func newMavenProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig, artifactType string) chi.Router {
	r := chi.NewRouter()

	proxy := &MavenProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   artifactType,
	}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, artifactType))

	// Maven Repository Layout paths
	// /group/artifact/version/artifact-version.ext

	r.Get("/{group}/{artifact}/{version}/{fileName}-{fileVersion}.jar", proxy.handleJAR)
	r.Get("/{group}/{artifact}/{version}/{fileName}-{fileVersion}.pom", proxy.handlePOM)
	r.Get("/{group}/{artifact}/{version}/{fileName}-{fileVersion}.war", proxy.handleWAR)
	r.Get("/{group}/{artifact}/{version}/{fileName}-{fileVersion}.zip", proxy.handleZIP)
	r.Get("/{group}/{artifact}/{version}/{fileName}-{fileVersion}.tgz", proxy.handleTGZ)

	// Metadata files
	r.Get("/{group}/{artifact}/{version}/maven-metadata.xml", proxy.handleMetadata)

	// Directory listing
	r.Get("/{group}/{artifact}/{version}/", proxy.handleVersionDir)
	r.Get("/{group}/{artifact}/", proxy.handleArtifactDir)
	r.Get("/{group}/", proxy.handleGroupDir)

	// Root
	r.Get("/", proxy.handleRoot)

	return r
}

// handleRoot returns registry info
func (p *MavenProxy) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`
<!DOCTYPE html>
<html>
<head><title>Cargobay Maven Proxy</title></head>
<body>
<h1>Cargobay Maven Proxy</h1>
<p>Caching proxy for Maven artifacts</p>
</body>
</html>
`))
}

// handleJAR handles JAR file downloads
func (p *MavenProxy) handleJAR(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	artifact := chi.URLParam(r, "artifact")
	version := chi.URLParam(r, "version")

	t := proxypkg.TargetFromContext(r, "maven")

	// Try cache first
	if data, err := p.storage.GetArtifact("maven", group, artifact, version); err == nil && data != nil {
		w.Header().Set("Content-Type", "application/java-archive")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.jar", artifact, version))
		if err := p.db.IncrementArtifactDownloads(t.Label, group, artifact, version); err != nil {
			fmt.Printf("failed to record download for %s:%s:%s: %v\n", group, artifact, version, err)
		}
		w.Write(data)
		return
	}

	// Fetch from upstream
	tarballURL, err := p.getUpstreamURL(t.Reg, group, artifact, version, "jar")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get JAR upstream URL: %v", err), http.StatusBadGateway)
		return
	}
	req, err := http.NewRequest(http.MethodGet, tarballURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build JAR request: %v", err), http.StatusInternalServerError)
		return
	}
	proxypkg.ApplyUpstreamAuth(req, t.Reg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download JAR: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	data := make([]byte, resp.ContentLength)
	resp.Body.Read(data)

	// Save to storage
	if _, err := p.storage.SaveArtifact("maven", group, artifact, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	// Save to cache
	p.cache.Set(fmt.Sprintf("jar:%s:%s:%s", group, artifact, version), data)

	w.Header().Set("Content-Type", "application/java-archive")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.jar", artifact, version))
	if err := p.db.IncrementArtifactDownloads(t.Label, group, artifact, version); err != nil {
		fmt.Printf("failed to record download for %s:%s:%s: %v\n", group, artifact, version, err)
	}
	w.Write(data)
}

// handlePOM handles POM file downloads
func (p *MavenProxy) handlePOM(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	artifact := chi.URLParam(r, "artifact")
	version := chi.URLParam(r, "version")

	// Try cache first
	if data, err := p.storage.GetArtifact("maven", group, artifact, version); err == nil && data != nil {
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.pom", artifact, version))
		w.Write(data)
		return
	}

	// Fetch from upstream
	t := proxypkg.TargetFromContext(r, "maven")
	tarballURL, err := p.getUpstreamURL(t.Reg, group, artifact, version, "pom")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get POM upstream URL: %v", err), http.StatusBadGateway)
		return
	}
	req, err := http.NewRequest(http.MethodGet, tarballURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build POM request: %v", err), http.StatusInternalServerError)
		return
	}
	proxypkg.ApplyUpstreamAuth(req, t.Reg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download POM: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	data := make([]byte, resp.ContentLength)
	resp.Body.Read(data)

	// Save to storage
	if _, err := p.storage.SaveArtifact("maven", group, artifact, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	// Save to cache
	p.cache.Set(fmt.Sprintf("pom:%s:%s:%s", group, artifact, version), data)

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.pom", artifact, version))
	w.Write(data)
}

// handleWAR handles WAR file downloads
func (p *MavenProxy) handleWAR(w http.ResponseWriter, r *http.Request) {
	p.handleArtifactFile(w, r, "war", "application/octet-stream")
}

// handleZIP handles ZIP file downloads
func (p *MavenProxy) handleZIP(w http.ResponseWriter, r *http.Request) {
	p.handleArtifactFile(w, r, "zip", "application/zip")
}

// handleTGZ handles TGZ file downloads
func (p *MavenProxy) handleTGZ(w http.ResponseWriter, r *http.Request) {
	p.handleArtifactFile(w, r, "tgz", "application/gzip")
}

// handleArtifactFile handles a generic Maven artifact file download
func (p *MavenProxy) handleArtifactFile(w http.ResponseWriter, r *http.Request, ext, contentType string) {
	group := chi.URLParam(r, "group")
	artifact := chi.URLParam(r, "artifact")
	version := chi.URLParam(r, "version")

	// Try cache first
	if data, err := p.storage.GetArtifact("maven", group, artifact, version); err == nil && data != nil {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.%s", artifact, version, ext))
		w.Write(data)
		return
	}

	// Fetch from upstream
	t := proxypkg.TargetFromContext(r, "maven")
	upstreamURL, err := p.getUpstreamURL(t.Reg, group, artifact, version, ext)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get %s upstream URL: %v", ext, err), http.StatusBadGateway)
		return
	}
	req, err := http.NewRequest(http.MethodGet, upstreamURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build %s request: %v", ext, err), http.StatusInternalServerError)
		return
	}
	proxypkg.ApplyUpstreamAuth(req, t.Reg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download %s: %v", ext, err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	data := make([]byte, resp.ContentLength)
	resp.Body.Read(data)

	// Save to storage
	if _, err := p.storage.SaveArtifact("maven", group, artifact, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	// Save to cache
	p.cache.Set(fmt.Sprintf("%s:%s:%s:%s", ext, group, artifact, version), data)

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.%s", artifact, version, ext))
	w.Write(data)
}

// handleVersionDir lists artifact files for a version
func (p *MavenProxy) handleVersionDir(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	artifact := chi.URLParam(r, "artifact")
	version := chi.URLParam(r, "version")

	artifacts, err := p.db.ListArtifacts(proxypkg.TargetFromContext(r, "maven").Label, database.ListOptions{
		Namespace:    group,
		ArtifactType: "maven",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list version directory: %v", err), http.StatusBadGateway)
		return
	}

	files := []string{}
	for _, a := range artifacts {
		if a.ArtifactName == artifact && a.Version == version {
			files = append(files, a.ArtifactName+"-"+a.Version)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"files": files})
}

// handleArtifactDir lists versions for an artifact
func (p *MavenProxy) handleArtifactDir(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	artifact := chi.URLParam(r, "artifact")

	artifacts, err := p.db.ListArtifacts(proxypkg.TargetFromContext(r, "maven").Label, database.ListOptions{
		Namespace:    group,
		ArtifactType: "maven",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list artifact directory: %v", err), http.StatusBadGateway)
		return
	}

	versions := []string{}
	for _, a := range artifacts {
		if a.ArtifactName == artifact {
			versions = append(versions, a.Version)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"versions": versions})
}

// handleGroupDir lists artifacts for a group
func (p *MavenProxy) handleGroupDir(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")

	artifacts, err := p.db.ListArtifacts(proxypkg.TargetFromContext(r, "maven").Label, database.ListOptions{
		Namespace:    group,
		ArtifactType: "maven",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list group directory: %v", err), http.StatusBadGateway)
		return
	}

	seen := make(map[string]bool)
	names := []string{}
	for _, a := range artifacts {
		if !seen[a.ArtifactName] {
			names = append(names, a.ArtifactName)
			seen[a.ArtifactName] = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"artifacts": names})
}

// handleMetadata handles maven-metadata.xml generation
func (p *MavenProxy) handleMetadata(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	artifact := chi.URLParam(r, "artifact")
	version := chi.URLParam(r, "version")

	// Get versions from database
	versions, err := p.db.ListArtifacts("maven", database.ListOptions{
		Namespace:    group,
		ArtifactType: "maven",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get versions: %v", err), http.StatusBadGateway)
		return
	}

	// Generate maven-metadata.xml
	metadata := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>%s</groupId>
  <artifactId>%s</artifactId>
  <versioning>
    <latest>%s</latest>
    <release>%s</release>
    <versions>`, group, artifact, version, version)
	for _, v := range versions {
		metadata += fmt.Sprintf("\n      <version>%s</version>", v.Version)
	}
	metadata += `
    </versions>
    <lastUpdated>` + time.Now().UTC().Format("20060102150405") + `</lastUpdated>
  </versioning>
</metadata>`

	w.Header().Set("Content-Type", "application/xml")
	w.Write([]byte(metadata))
}

// getUpstreamURL returns the upstream URL for an artifact. Returns an error
// if the resolved registry has no upstream proxy configured — it never
// silently falls back to Maven Central.
func (p *MavenProxy) getUpstreamURL(reg *database.RegistryConfig, group, artifact, version, ext string) (string, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return "", fmt.Errorf("no upstream proxy configured for this registry")
	}

	// Maven path: group/artifact/version/artifact-version.ext
	groupPath := strings.ReplaceAll(group, ".", "/")
	return fmt.Sprintf("%s/%s/%s/%s/%s-%s.%s", reg.URL, groupPath, artifact, version, artifact, version, ext), nil
}
