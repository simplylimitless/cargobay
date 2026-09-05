// Package conan implements a caching Conan (C/C++) package registry proxy.
//
// This package implements the simpler Conan v1 REST API (recipe + binary
// package files addressed by download_urls, rather than the v2
// revisions-based API) — download_urls responses point back at our own
// /files/ endpoints, and each individual recipe/package file is cached and
// served independently.
//
// Conan v1 API: https://docs.conan.io/1/devtools/server/http.html
package conan

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

// recipeFiles are the standard files making up a Conan recipe.
var recipeFiles = []string{"conanfile.py", "conanmanifest.txt", "conan_export.tgz", "conan_sources.tgz"}

// packageFiles are the standard files making up a built Conan binary package.
var packageFiles = []string{"conaninfo.txt", "conanmanifest.txt", "conan_package.tgz"}

// largeBlobs are the files substantial enough to count as an actual
// download (matching how other proxies only increment on the real
// package blob, not small manifest/info files).
var largeBlobs = map[string]bool{
	"conan_export.tgz":  true,
	"conan_sources.tgz": true,
	"conan_package.tgz": true,
}

// ConanProxy implements the Conan v1 REST API. Recipes are namespaced by
// "{user}/{channel}".
type ConanProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// NewConanProxy creates a new Conan proxy instance.
func NewConanProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &ConanProxy{db: db, storage: storage, cache: cache}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "conan"))

	r.Get("/v1/conans/{name}/{version}/{user}/{channel}/download_urls", proxy.handleRecipeDownloadURLs)
	r.Get("/v1/conans/{name}/{version}/{user}/{channel}/files/{fileName}", proxy.handleRecipeFile)
	r.Get("/v1/conans/{name}/{version}/{user}/{channel}/packages/{packageID}/download_urls", proxy.handlePackageDownloadURLs)
	r.Get("/v1/conans/{name}/{version}/{user}/{channel}/packages/{packageID}/files/{fileName}", proxy.handlePackageFile)

	return r
}

func recipeNamespace(user, channel string) string {
	return fmt.Sprintf("%s/%s", user, channel)
}

// handleRecipeDownloadURLs returns a filename->URL map for the recipe, in
// the shape Conan v1 clients expect, pointing back at our own /files/
// endpoint for each file we know about.
func (p *ConanProxy) handleRecipeDownloadURLs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")
	user := chi.URLParam(r, "user")
	channel := chi.URLParam(r, "channel")
	namespace := recipeNamespace(user, channel)

	base := fmt.Sprintf("%s/v1/conans/%s/%s/%s/%s/files", baseURL(r), name, version, user, channel)

	urls := map[string]string{}
	for _, f := range recipeFiles {
		if rc, err := p.storage.GetArtifactStream("conan", namespace, name, fileVersionKey(version, f)); err == nil && rc != nil {
			rc.Close()
			urls[f] = fmt.Sprintf("%s/%s", base, f)
		}
	}
	if len(urls) == 0 {
		// Nothing cached locally yet — advertise the standard file set so
		// the client requests them, triggering upstream fetch-and-cache.
		for _, f := range recipeFiles {
			urls[f] = fmt.Sprintf("%s/%s", base, f)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(urls)
}

// handlePackageDownloadURLs is the binary-package equivalent of
// handleRecipeDownloadURLs.
func (p *ConanProxy) handlePackageDownloadURLs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")
	user := chi.URLParam(r, "user")
	channel := chi.URLParam(r, "channel")
	packageID := chi.URLParam(r, "packageID")
	namespace := recipeNamespace(user, channel)

	base := fmt.Sprintf("%s/v1/conans/%s/%s/%s/%s/packages/%s/files", baseURL(r), name, version, user, channel, packageID)

	urls := map[string]string{}
	for _, f := range packageFiles {
		if rc, err := p.storage.GetArtifactStream("conan-pkg", namespace, name, packageVersionKey(version, packageID, f)); err == nil && rc != nil {
			rc.Close()
			urls[f] = fmt.Sprintf("%s/%s", base, f)
		}
	}
	if len(urls) == 0 {
		for _, f := range packageFiles {
			urls[f] = fmt.Sprintf("%s/%s", base, f)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(urls)
}

// handleRecipeFile serves (or fetches-and-caches) a single recipe file.
func (p *ConanProxy) handleRecipeFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")
	user := chi.URLParam(r, "user")
	channel := chi.URLParam(r, "channel")
	fileName := chi.URLParam(r, "fileName")
	namespace := recipeNamespace(user, channel)
	key := fileVersionKey(version, fileName)

	t := proxypkg.TargetFromContext(r, "conan")

	if rc, err := p.storage.GetArtifactStream(t.Label, namespace, name, key); err == nil && rc != nil {
		defer rc.Close()
		p.serveBlobStream(w, name, version, fileName, rc, func() error {
			return p.db.IncrementArtifactDownloads(t.Label, namespace, name, version)
		})
		return
	}

	upstreamPath := fmt.Sprintf("v1/conans/%s/%s/%s/%s/files/%s", name, version, user, channel, fileName)
	resp, err := p.fetchStreamFromUpstream(t.Reg, upstreamPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download file: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, namespace, name, key, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
		return
	}
	if fileName == "conan_export.tgz" {
		artifact := &database.ArtifactMetadata{
			ID:           fmt.Sprintf("conan:%s:%s:%s:%s", t.Label, namespace, name, version),
			RegistryID:   t.Label,
			ArtifactType: "conan",
			Namespace:    namespace,
			ArtifactName: name,
			Version:      version,
			Size:         counter.N,
			Metadata:     map[string]interface{}{},
			Tags:         []string{version},
		}
		if err := p.db.SaveArtifact(artifact); err != nil {
			fmt.Printf("Warning: failed to save recipe metadata: %v\n", err)
		}
	}

	rc, err := p.storage.GetArtifactStream(t.Label, namespace, name, key)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve file", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	p.serveBlobStream(w, name, version, fileName, rc, func() error {
		return p.db.IncrementArtifactDownloads(t.Label, namespace, name, version)
	})
}

// handlePackageFile serves (or fetches-and-caches) a single binary-package
// file.
func (p *ConanProxy) handlePackageFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")
	user := chi.URLParam(r, "user")
	channel := chi.URLParam(r, "channel")
	packageID := chi.URLParam(r, "packageID")
	fileName := chi.URLParam(r, "fileName")
	namespace := recipeNamespace(user, channel)
	key := packageVersionKey(version, packageID, fileName)

	t := proxypkg.TargetFromContext(r, "conan")

	if rc, err := p.storage.GetArtifactStream(t.Label, namespace, name, key); err == nil && rc != nil {
		defer rc.Close()
		p.serveBlobStream(w, name, version, fileName, rc, func() error {
			return p.db.IncrementArtifactDownloads(t.Label, namespace, name, version)
		})
		return
	}

	upstreamPath := fmt.Sprintf("v1/conans/%s/%s/%s/%s/packages/%s/files/%s", name, version, user, channel, packageID, fileName)
	resp, err := p.fetchStreamFromUpstream(t.Reg, upstreamPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download file: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, namespace, name, key, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
		return
	}

	rc, err := p.storage.GetArtifactStream(t.Label, namespace, name, key)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve file", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	p.serveBlobStream(w, name, version, fileName, rc, func() error {
		return p.db.IncrementArtifactDownloads(t.Label, namespace, name, version)
	})
}

// serveBlobStream writes headers and streams a cached file's contents to
// the client, incrementing the download counter only for the large blob
// files (matching how other proxies only count the real package blob, not
// small manifest/info files).
func (p *ConanProxy) serveBlobStream(w http.ResponseWriter, name, version, fileName string, r io.Reader, count func() error) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	if largeBlobs[fileName] {
		if err := count(); err != nil {
			fmt.Printf("failed to record download for %s/%s (%s): %v\n", name, version, fileName, err)
		}
	}
	io.Copy(w, r)
}

// fetchStreamFromUpstream is like a []byte-returning fetch but returns the
// live response so large recipe/package files can be streamed straight into
// storage instead of buffered in memory. Callers must close the returned
// body.
func (p *ConanProxy) fetchStreamFromUpstream(reg *database.RegistryConfig, path string) (*http.Response, error) {
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

func fileVersionKey(version, fileName string) string {
	return version + "::" + fileName
}

func packageVersionKey(version, packageID, fileName string) string {
	return version + "::" + packageID + "::" + fileName
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}
