// Package nuget implements the NuGet V2/V3 API proxy
//
// This package provides a caching proxy for NuGet packages that:
//   - Implements NuGet V2 API (OData-based)
//   - Implements NuGet V3 API (JSON-based)
//   - Caches packages from api.nuget.org
//   - Handles .nupkg files
//
// NuGet API: https://learn.microsoft.com/en-us/nuget/api/overview
package nuget

import (
	"encoding/json"
	"fmt"
	"io"
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

// NuGetProxy implements the NuGet API proxy
type NuGetProxy struct {
	db         *database.Database
	storage    storage.StorageAdapter
	cache      *cache.Cache
	registries []database.RegistryConfig
	registry   string
}

// NewNuGetProxy creates a new NuGet proxy instance
func NewNuGetProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &NuGetProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "nuget",
	}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "nuget"))

	// NuGet V3 API routes (recommended)
	// Search: /query?q={query}&skip={skip}&take={take}
	r.Get("/query", proxy.handleQuery)

	// Package metadata: /package/{id}/{version}
	r.Get("/package/{packageId}/{version}", proxy.handlePackageMetadata)

	// Download: /package/{id}/{version}.nupkg
	r.Get("/package/{packageId}/{version}.nupkg", proxy.handleDownload)

	// V2 API routes (for backwards compatibility)
	// Search: /Search()?$filter=IsLatestVersion&searchTerm={query}
	r.Get("/Search()", proxy.handleSearchV2)

	// Package: /Package/{id}/{version}
	r.Get("/Package/{packageId}/{version}", proxy.handlePackageV2)

	// ListPackages: /Package()?$filter=IsLatestVersion
	r.Get("/Package()", proxy.handleListPackages)

	// V3 Registration (JSON-based)
	r.Get("/registration/{packageId}/index.json", proxy.handleRegistrationIndex)
	r.Get("/registration/{packageId}/{version}.json", proxy.handleRegistrationVersion)

	return r
}

// handleQuery handles the V3 search query endpoint
func (n *NuGetProxy) handleQuery(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	skip := 0
	take := 21 // Default NuGet client page size

	if s := r.URL.Query().Get("skip"); s != "" {
		skip = 0 // default
	}
	if t := r.URL.Query().Get("take"); t != "" {
		take = 21 // default
	}

	// Try cache first
	cacheKey := fmt.Sprintf("nuget:query:%s:%d:%d", query, skip, take)
	if data, err := n.cacheGet(cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	// Search in database
	artifacts, err := n.db.SearchArtifacts(query, database.SearchOptions{
		ArtifactType: "nuget",
		Limit:        take,
		Offset:       skip,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Search failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Build response
	results := make([]map[string]interface{}, 0, len(artifacts))
	for _, a := range artifacts {
		meta, ok := a.Metadata["metadata"].(map[string]interface{})
		if !ok {
			meta = a.Metadata
		}

		result := map[string]interface{}{
			"id":            a.ArtifactName,
			"version":       a.Version,
			"description":   meta["description"],
			"authors":       meta["authors"],
			"iconUrl":       meta["iconUrl"],
			"licenseUrl":    meta["licenseUrl"],
			"projectUrl":    meta["projectUrl"],
			"summary":       meta["summary"],
			"tags":          meta["tags"],
			"title":         meta["title"],
			"totalDownloads": meta["totalDownloads"],
			"verified":      false,
		}

		// Get latest version for this package
		latest, _ := n.getLatestVersion(a.ArtifactName)
		if latest != nil && latest.Version == a.Version {
			result["isLatestVersion"] = true
		} else {
			result["isLatestVersion"] = false
		}

		results = append(results, result)
	}

	response := map[string]interface{}{
		"data":        results,
		"skip":        skip,
		"take":        take,
		"totalHits":   len(results),
	}

	data, _ := json.Marshal(response)
	n.cacheSet(cacheKey, data)

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handlePackageMetadata handles package metadata lookup
func (n *NuGetProxy) handlePackageMetadata(w http.ResponseWriter, r *http.Request) {
	packageId := chi.URLParam(r, "packageId")
	version := chi.URLParam(r, "version")

	// Try to get from database
	artifact, err := n.db.GetArtifactByParams("nuget", "", packageId, version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get package: %v", err), http.StatusInternalServerError)
		return
	}

	// If not found, fetch from upstream
	if artifact == nil {
		t := proxypkg.TargetFromContext(r, "nuget")
		artifact, err = n.fetchPackageFromUpstream(t.Reg, t.Label, packageId, version)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch package: %v", err), http.StatusBadGateway)
			return
		}
	}

	meta, ok := artifact.Metadata["metadata"].(map[string]interface{})
	if !ok {
		meta = artifact.Metadata
	}

	response := map[string]interface{}{
		"packageContent": fmt.Sprintf("/package/%s/%s.nupkg", packageId, version),
		"versions":       []string{version},
		"data": map[string]interface{}{
			"id":            artifact.ArtifactName,
			"version":       artifact.Version,
			"description":   meta["description"],
			"authors":       meta["authors"],
			"iconUrl":       meta["iconUrl"],
			"licenseUrl":    meta["licenseUrl"],
			"projectUrl":    meta["projectUrl"],
			"summary":       meta["summary"],
			"tags":          meta["tags"],
			"title":         meta["title"],
			"totalDownloads": meta["totalDownloads"],
			"dependencies":  meta["dependencies"],
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDownload handles package download
func (n *NuGetProxy) handleDownload(w http.ResponseWriter, r *http.Request) {
	packageId := chi.URLParam(r, "packageId")
	version := chi.URLParam(r, "version")
	fileName := fmt.Sprintf("%s.%s.nupkg", packageId, version)

	// Try to get from storage first
	data, err := n.storage.GetArtifact("nuget", "", packageId, version)
	if err == nil && data != nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
		w.Header().Set("X-NuGet-Protocol-Version", "3.0.0")
		w.Write(data)
		return
	}

	// Fetch from upstream
	data, err = n.fetchPackageFileFromUpstream(proxypkg.TargetFromContext(r, "nuget").Reg, packageId, version, fileName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download package: %v", err), http.StatusBadGateway)
		return
	}

	// Save to storage
	if _, err = n.storage.SaveArtifact("nuget", "", packageId, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	// Save to cache
	n.cacheSet(fmt.Sprintf("nuget:file:%s:%s", packageId, version), data)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	w.Header().Set("X-NuGet-Protocol-Version", "3.0.0")
	w.Write(data)
}

// handleSearchV2 handles the V2 search endpoint
func (n *NuGetProxy) handleSearchV2(w http.ResponseWriter, r *http.Request) {
	searchTerm := r.URL.Query().Get("searchTerm")
	take := 20

	if t := r.URL.Query().Get("take"); t != "" {
		take = 20 // default
	}

	// Try cache
	cacheKey := fmt.Sprintf("nuget:search:v2:%s:%d", searchTerm, take)
	if data, err := n.cacheGet(cacheKey); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	// Search
	artifacts, err := n.db.SearchArtifacts(searchTerm, database.SearchOptions{
		ArtifactType: "nuget",
		Limit:        take,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Search failed: %v", err), http.StatusInternalServerError)
		return
	}

	results := make([]map[string]interface{}, 0, len(artifacts))
	for _, a := range artifacts {
		meta, ok := a.Metadata["metadata"].(map[string]interface{})
		if !ok {
			meta = a.Metadata
		}

		results = append(results, map[string]interface{}{
			"id":              a.ArtifactName,
			"version":         a.Version,
			"versionDownloads": 0,
			"versionTimestamp": a.Created,
			"summary":         meta["summary"],
			"title":           meta["title"],
			"iconUrl":         meta["iconUrl"],
			"licenseUrl":      meta["licenseUrl"],
			"projectUrl":      meta["projectUrl"],
			"description":     meta["description"],
			"tags":            meta["tags"],
			"authors":         meta["authors"],
			"totalDownloads":  meta["totalDownloads"],
			"publishers":      []string{},
		})
	}

	// Check if latest for each package
	seen := make(map[string]bool)
	latestVersions := make(map[string]bool)
	for _, a := range artifacts {
		if !seen[a.ArtifactName] {
			latestVersions[a.ArtifactName] = true
			seen[a.ArtifactName] = true
		}
	}

	for i, result := range results {
		result["isLatestVersion"] = latestVersions[result["id"].(string)]
		results[i] = result
	}

	response := map[string]interface{}{
		"data":        results,
		"skip":        0,
		"take":        take,
		"totalHits":   len(results),
	}

	data, _ := json.Marshal(response)
	n.cacheSet(cacheKey, data)

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handlePackageV2 handles V2 package endpoint
func (n *NuGetProxy) handlePackageV2(w http.ResponseWriter, r *http.Request) {
	packageId := chi.URLParam(r, "packageId")
	version := chi.URLParam(r, "version")

	artifact, err := n.db.GetArtifactByParams("nuget", "", packageId, version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get package: %v", err), http.StatusInternalServerError)
		return
	}

	if artifact == nil {
		t := proxypkg.TargetFromContext(r, "nuget")
		artifact, err = n.fetchPackageFromUpstream(t.Reg, t.Label, packageId, version)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch package: %v", err), http.StatusBadGateway)
			return
		}
	}

	meta, ok := artifact.Metadata["metadata"].(map[string]interface{})
	if !ok {
		meta = artifact.Metadata
	}

	response := map[string]interface{}{
		"d": map[string]interface{}{
			"Id":              fmt.Sprintf("%s|%s", packageId, version),
			"Version":         version,
			"Title":           meta["title"],
			"IconUrl":         meta["iconUrl"],
			"LicenseUrl":      meta["licenseUrl"],
			"ProjectUrl":      meta["projectUrl"],
			"Description":     meta["description"],
			"Summary":         meta["summary"],
			"Tags":            meta["tags"],
			"Authors":         meta["authors"],
			"TotalDownloads":  meta["totalDownloads"],
			"Versions":        []interface{}{version},
			"VersionDownloads": 0,
			"LastUpdated":     artifact.Updated,
			"Published":       artifact.Created,
			"Flags":           0,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleListPackages handles V2 list packages endpoint
func (n *NuGetProxy) handleListPackages(w http.ResponseWriter, r *http.Request) {
	limit := 20

	artifacts, err := n.db.ListArtifacts(proxypkg.TargetFromContext(r, "nuget").Label, database.ListOptions{
		ArtifactType: "nuget",
		Limit:        limit,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list packages: %v", err), http.StatusInternalServerError)
		return
	}

	results := make([]map[string]interface{}, 0, len(artifacts))
	for _, a := range artifacts {
		meta, ok := a.Metadata["metadata"].(map[string]interface{})
		if !ok {
			meta = a.Metadata
		}

		results = append(results, map[string]interface{}{
			"Id":              fmt.Sprintf("%s|%s", a.ArtifactName, a.Version),
			"Version":         a.Version,
			"Title":           meta["title"],
			"IconUrl":         meta["iconUrl"],
			"LicenseUrl":      meta["licenseUrl"],
			"ProjectUrl":      meta["projectUrl"],
			"Description":     meta["description"],
			"Summary":         meta["summary"],
			"Tags":            meta["tags"],
			"Authors":         meta["authors"],
			"TotalDownloads":  meta["totalDownloads"],
			"Versions":        []interface{}{a.Version},
			"VersionDownloads": 0,
			"LastUpdated":     a.Updated,
			"Published":       a.Created,
			"Flags":           0,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"d": results})
}

// handleRegistrationIndex handles V3 registration index
func (n *NuGetProxy) handleRegistrationIndex(w http.ResponseWriter, r *http.Request) {
	packageId := chi.URLParam(r, "packageId")

	// Get all versions from database
	artifacts, err := n.db.ListArtifacts(proxypkg.TargetFromContext(r, "nuget").Label, database.ListOptions{
		ArtifactType: "nuget",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get package versions: %v", err), http.StatusInternalServerError)
		return
	}

	versions := make([]map[string]interface{}, 0, len(artifacts))
	for _, a := range artifacts {
		if a.ArtifactName != packageId {
			continue
		}
		meta, ok := a.Metadata["metadata"].(map[string]interface{})
		if !ok {
			meta = a.Metadata
		}

		versions = append(versions, map[string]interface{}{
			"catalogEntry": map[string]interface{}{
				"id":            a.ArtifactName,
				"version":       a.Version,
				"description":   meta["description"],
				"authors":       []string{},
				"iconUrl":       "",
				"licenseUrl":    "",
				"projectUrl":    "",
				"tags":          []string{},
				"summary":       "",
				"title":         "",
			},
			"packageContent": fmt.Sprintf("/package/%s/%s.nupkg", a.ArtifactName, a.Version),
		})
	}

	response := map[string]interface{}{
		"count":   len(versions),
		"items":   versions,
		"totalHits": len(versions),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleRegistrationVersion handles V3 registration version
func (n *NuGetProxy) handleRegistrationVersion(w http.ResponseWriter, r *http.Request) {
	packageId := chi.URLParam(r, "packageId")
	version := chi.URLParam(r, "version")

	artifact, err := n.db.GetArtifactByParams("nuget", "", packageId, version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get package: %v", err), http.StatusInternalServerError)
		return
	}

	if artifact == nil {
		t := proxypkg.TargetFromContext(r, "nuget")
		artifact, err = n.fetchPackageFromUpstream(t.Reg, t.Label, packageId, version)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch package: %v", err), http.StatusBadGateway)
			return
		}
	}

	meta, ok := artifact.Metadata["metadata"].(map[string]interface{})
	if !ok {
		meta = artifact.Metadata
	}

	response := map[string]interface{}{
		"packageContent": fmt.Sprintf("/package/%s/%s.nupkg", packageId, version),
		"catalogEntry": map[string]interface{}{
			"id":            artifact.ArtifactName,
			"version":       artifact.Version,
			"description":   meta["description"],
			"authors":       []string{},
			"iconUrl":       meta["iconUrl"],
			"licenseUrl":    meta["licenseUrl"],
			"projectUrl":    meta["projectUrl"],
			"tags":          []string{},
			"summary":       meta["summary"],
			"title":         meta["title"],
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// fetchPackageFromUpstream fetches package metadata from NuGet
func (n *NuGetProxy) fetchPackageFromUpstream(reg *database.RegistryConfig, registryLabel, packageId, version string) (*database.ArtifactMetadata, error) {
	upstream := "https://api.nuget.org/v3/index.json"
	if reg != nil && reg.Proxy && reg.URL != "" {
		upstream = reg.URL
	}

	// Get catalog entry
	catalogURL := fmt.Sprintf("%s/%s/%s.json", strings.TrimSuffix(upstream, "/v3/index.json"), packageId, version)
	catalogReq, err := http.NewRequest(http.MethodGet, catalogURL, nil)
	if err != nil {
		return nil, err
	}
	proxypkg.ApplyUpstreamAuth(catalogReq, reg)
	resp, err := http.DefaultClient.Do(catalogReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Extract metadata
	packageContent, _ := result["packageContent"].(string)
	catalogEntry := result["catalogEntry"].(map[string]interface{})

	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("nuget:%s:%s:%s", registryLabel, packageId, version),
		RegistryID:      registryLabel,
		ArtifactType:    "nuget",
		Namespace:       "",
		ArtifactName:    packageId,
		Version:         version,
		DigestAlgorithm: "sha256",
		Size:            0,
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata:        catalogEntry,
		Tags:            []string{version},
	}

	// Get package content size
	if contentReq, err := http.NewRequest(http.MethodGet, packageContent, nil); err == nil {
		proxypkg.ApplyUpstreamAuth(contentReq, reg)
		if contentResp, err := http.DefaultClient.Do(contentReq); err == nil {
			contentResp.Body.Close()
			artifact.Size = contentResp.ContentLength
		}
	}

	n.db.SaveArtifact(artifact)
	return artifact, nil
}

// fetchPackageFileFromUpstream fetches a .nupkg file from NuGet
func (n *NuGetProxy) fetchPackageFileFromUpstream(reg *database.RegistryConfig, packageId, version, fileName string) ([]byte, error) {
	upstream := "https://www.nuget.org"
	if reg != nil && reg.Proxy && reg.URL != "" {
		upstream = reg.URL
	}

	// NuGet package URL format
	fileURL := fmt.Sprintf("%s/api/v2/package/%s/%s", upstream, packageId, version)
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
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}

	return data, nil
}

// getLatestVersion returns the latest version of a package
func (n *NuGetProxy) getLatestVersion(packageId string) (*database.ArtifactMetadata, error) {
	artifacts, err := n.db.ListArtifacts("nuget", database.ListOptions{
		ArtifactType: "nuget",
		OrderBy:      "created",
		Order:        "desc",
		Limit:        1,
	})
	if err != nil || len(artifacts) == 0 {
		return nil, err
	}
	return &artifacts[0], nil
}

// cacheGet retrieves data from cache
func (n *NuGetProxy) cacheGet(key string) ([]byte, error) {
	var data []byte
	err := n.cache.Get(key, &data)
	return data, err
}

// cacheSet stores data in cache
func (n *NuGetProxy) cacheSet(key string, data []byte) {
	n.cache.Set(key, data)
}
