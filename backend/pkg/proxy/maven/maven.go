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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	proxypkg "github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/go-chi/chi/v5"
)

// MavenProxy implements the Maven Repository API proxy
type MavenProxy struct {
	db         *database.Database
	storage    storage.StorageAdapter
	cache      *cache.Cache
	rbacMgr    *rbac.RBAC
	registries []database.RegistryConfig
	registry   string
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
		rbacMgr:    rbacMgr,
		registries: registries,
		registry:   artifactType,
	}

	r.Group(func(r chi.Router) {
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
	})

	// Deploy (mvn/gradle/sbt publish sends a PUT to the same path shape as
	// the corresponding GET download). Anonymous requests are rejected
	// outright; checkPublishAccess further enforces that the authenticated
	// user may publish to the resolved registry specifically.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)

		r.Put("/{group}/{artifact}/{version}/{fileName}-{fileVersion}.jar", proxy.handlePutArtifact("jar", "application/java-archive"))
		r.Put("/{group}/{artifact}/{version}/{fileName}-{fileVersion}.pom", proxy.handlePutArtifact("pom", "application/xml"))
		r.Put("/{group}/{artifact}/{version}/{fileName}-{fileVersion}.war", proxy.handlePutArtifact("war", "application/octet-stream"))
		r.Put("/{group}/{artifact}/{version}/{fileName}-{fileVersion}.zip", proxy.handlePutArtifact("zip", "application/zip"))
		r.Put("/{group}/{artifact}/{version}/{fileName}-{fileVersion}.tgz", proxy.handlePutArtifact("tgz", "application/gzip"))
		r.Put("/{group}/{artifact}/{version}/maven-metadata.xml", proxy.handlePutMetadataNoop)
	})

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
	if rc, err := p.storage.GetArtifactStream(t.Label, group, artifact, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/java-archive")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.jar", artifact, version))
		if err := p.db.IncrementArtifactDownloads(t.Label, group, artifact, version); err != nil {
			fmt.Printf("failed to record download for %s:%s:%s: %v\n", group, artifact, version, err)
		}
		io.Copy(w, rc)
		return
	}

	// Fetch from upstream
	candidates := p.resolveUpstreamCandidates(middleware.GetUser(r), t.Reg)
	resp, _, err := p.fetchUpstreamStream(candidates, group, artifact, version, "jar")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download JAR: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, group, artifact, version, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	rc, err := p.storage.GetArtifactStream(t.Label, group, artifact, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve artifact", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/java-archive")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.jar", artifact, version))
	if err := p.db.IncrementArtifactDownloads(t.Label, group, artifact, version); err != nil {
		fmt.Printf("failed to record download for %s:%s:%s: %v\n", group, artifact, version, err)
	}
	io.Copy(w, rc)
}

// handlePOM handles POM file downloads
func (p *MavenProxy) handlePOM(w http.ResponseWriter, r *http.Request) {
	group := chi.URLParam(r, "group")
	artifact := chi.URLParam(r, "artifact")
	version := chi.URLParam(r, "version")

	t := proxypkg.TargetFromContext(r, "maven")

	// Try cache first
	if rc, err := p.storage.GetArtifactStream(t.Label, group, artifact, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.pom", artifact, version))
		io.Copy(w, rc)
		return
	}

	// Fetch from upstream
	candidates := p.resolveUpstreamCandidates(middleware.GetUser(r), t.Reg)
	resp, _, err := p.fetchUpstreamStream(candidates, group, artifact, version, "pom")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download POM: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, group, artifact, version, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	rc, err := p.storage.GetArtifactStream(t.Label, group, artifact, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve artifact", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.pom", artifact, version))
	io.Copy(w, rc)
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

	t := proxypkg.TargetFromContext(r, "maven")

	// Try cache first
	if rc, err := p.storage.GetArtifactStream(t.Label, group, artifact, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.%s", artifact, version, ext))
		io.Copy(w, rc)
		return
	}

	// Fetch from upstream
	candidates := p.resolveUpstreamCandidates(middleware.GetUser(r), t.Reg)
	resp, _, err := p.fetchUpstreamStream(candidates, group, artifact, version, ext)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download %s: %v", ext, err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, group, artifact, version, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	rc, err := p.storage.GetArtifactStream(t.Label, group, artifact, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve artifact", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.%s", artifact, version, ext))
	io.Copy(w, rc)
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

// resolveUpstreamCandidates returns the ordered registries to try upstream
// for reg, via the shared virtual-registry resolution logic (see
// proxypkg.ResolveMemberCandidates for the "<type>-virtual" semantics).
func (p *MavenProxy) resolveUpstreamCandidates(user *middleware.User, reg *database.RegistryConfig) []*database.RegistryConfig {
	return proxypkg.ResolveMemberCandidates(p.db, p.rbacMgr, user, reg, "maven")
}

// fetchUpstreamStream tries each candidate in order, building the upstream
// URL via getUpstreamURL, via the shared FetchFirstUpstreamStream helper.
// Unlike fetchUpstream it leaves the response body open for the caller to
// stream straight into storage instead of buffering the whole artifact in
// memory; the caller must close the response body.
func (p *MavenProxy) fetchUpstreamStream(candidates []*database.RegistryConfig, group, artifact, version, ext string) (*http.Response, *database.RegistryConfig, error) {
	return proxypkg.FetchFirstUpstreamStream(candidates, func(cand *database.RegistryConfig) (string, error) {
		return p.getUpstreamURL(cand, group, artifact, version, ext)
	})
}

// checkPublishAccess resolves the target registry from the Host header
// (same resolution the GET routes use) and verifies the requester has
// publish access to it, writing an error response and returning ok=false
// if not.
func (p *MavenProxy) checkPublishAccess(w http.ResponseWriter, r *http.Request) (proxypkg.Target, bool) {
	reg := proxypkg.ResolveRegistry(p.db, p.registries, r.Host, p.registry)
	label := p.registry
	if reg != nil {
		label = reg.ID
	}
	t := proxypkg.Target{Reg: reg, Label: label}
	return t, proxypkg.CheckAccess(w, p.rbacMgr, middleware.GetUser(r), reg, true)
}

// handlePutArtifact returns a handler for deploying a Maven artifact file
// (jar/pom/war/zip/tgz) via PUT, mirroring the corresponding GET download
// path shape.
func (p *MavenProxy) handlePutArtifact(ext, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, ok := p.checkPublishAccess(w, r)
		if !ok {
			return
		}

		group := chi.URLParam(r, "group")
		artifact := chi.URLParam(r, "artifact")
		version := chi.URLParam(r, "version")

		hasher := sha256.New()
		counter := &proxypkg.CountingReader{R: io.TeeReader(r.Body, hasher)}
		_, err := p.storage.SaveArtifactStream(t.Label, group, artifact, version, counter)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
			return
		}

		am := &database.ArtifactMetadata{
			ID:              fmt.Sprintf("%s:%s:%s:%s:%s", p.registry, t.Label, group, artifact, version),
			RegistryID:      t.Label,
			ArtifactType:    p.registry,
			Namespace:       group,
			ArtifactName:    artifact,
			Version:         version,
			Digest:          "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
			DigestAlgorithm: "sha256",
			Size:            counter.N,
			Created:         time.Now(),
			Updated:         time.Now(),
			Tags:            []string{version},
		}
		if err := p.db.SaveArtifact(am); err != nil {
			http.Error(w, fmt.Sprintf("Failed to save artifact metadata: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

// handlePutMetadataNoop accepts a maven-metadata.xml deploy without
// persisting it — handleMetadata (GET) already generates this file
// dynamically from stored artifact versions, so there is nothing to store.
func (p *MavenProxy) handlePutMetadataNoop(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.checkPublishAccess(w, r); !ok {
		return
	}
	io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusCreated)
}
