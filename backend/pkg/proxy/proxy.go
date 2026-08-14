// Package proxy provides the main proxy manager
//
// The proxy package orchestrates all proxy handlers (npm, Maven, Docker)
// and provides a unified API for artifact operations.
package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/cargobay/pkg/cache"
	"github.com/anthropics/cargobay/pkg/database"
	"github.com/anthropics/cargobay/pkg/search"
	"github.com/anthropics/cargobay/pkg/storage"
	"github.com/go-chi/chi/v5"
)

// ProxyManager manages all proxy handlers
type ProxyManager struct {
	db         *database.Database
	storage    storage.StorageAdapter
	cache      *cache.Cache
	registries []database.RegistryConfig
}

// New creates a new ProxyManager
func New(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, registries []database.RegistryConfig) *ProxyManager {
	return &ProxyManager{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
	}
}

// ListRegistries returns configured registries
func (pm *ProxyManager) ListRegistries(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"registries": pm.registries,
	})
}

// SearchArtifacts searches across all artifacts
func (pm *ProxyManager) SearchArtifacts(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	registryID := r.URL.Query().Get("registry")
	artifactType := r.URL.Query().Get("type")
	limit := 100
	offset := 0

	// Parse limit and offset from query
	if l := r.URL.Query().Get("limit"); l != "" {
		limit = 100 // default
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		offset = 0 // default
	}

	artifacts, err := pm.db.SearchArtifacts(query, database.SearchOptions{
		RegistryID:   registryID,
		ArtifactType: artifactType,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Search failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"query":    query,
		"results":  artifacts,
		"total":    len(artifacts),
		"limit":    limit,
		"offset":   offset,
	})
}

// upstreamSearchResult is a single row in the SearchUpstream response,
// unifying locally-cached artifacts and live upstream search hits.
type upstreamSearchResult struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"starCount"`
	Official    bool   `json:"official"`
	Source      string `json:"source"`
}

// SearchUpstream searches a single registry's locally-cached artifacts and,
// where a public anonymous search API exists (Docker Hub, Quay.io), the
// upstream registry itself, merging and de-duplicating the results. When no
// upstream search adapter exists for the registry (e.g. GHCR), or the
// upstream call fails, it degrades to local-only results rather than
// failing the request.
func (pm *ProxyManager) SearchUpstream(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	registryID := r.URL.Query().Get("registry")
	artifactType := r.URL.Query().Get("type")

	local, err := pm.db.SearchArtifacts(query, database.SearchOptions{
		RegistryID:   registryID,
		ArtifactType: artifactType,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Search failed: %v", err), http.StatusInternalServerError)
		return
	}

	seen := make(map[string]bool, len(local))
	results := make([]upstreamSearchResult, 0, len(local))
	for _, a := range local {
		seen[strings.ToLower(a.Namespace+"/"+a.ArtifactName)] = true
		results = append(results, upstreamSearchResult{
			Namespace:   a.Namespace,
			Name:        a.ArtifactName,
			Description: "",
			Source:      "local",
		})
	}

	upstreamAvailable := false
	if searcher, ok := search.Dispatch(registryID); ok && query != "" {
		upstreamAvailable = true
		upstreamResults, err := searcher.Search(r.Context(), query)
		if err != nil {
			// Upstream errors degrade to local-only results rather than
			// failing the whole request.
			upstreamAvailable = false
		} else {
			for _, res := range upstreamResults {
				key := strings.ToLower(res.Namespace + "/" + res.Name)
				if seen[key] {
					continue
				}
				seen[key] = true
				results = append(results, upstreamSearchResult{
					Namespace:   res.Namespace,
					Name:        res.Name,
					Description: res.Description,
					StarCount:   res.StarCount,
					Official:    res.Official,
					Source:      res.Source,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"query":             query,
		"results":           results,
		"upstreamAvailable": upstreamAvailable,
		"total":             len(results),
	})
}

// GetArtifactInfo returns artifact information
func (pm *ProxyManager) GetArtifactInfo(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	artifactName := chi.URLParam(r, "artifactName")
	version := chi.URLParam(r, "version")
	registryID := r.URL.Query().Get("registry")

	artifact, err := pm.db.GetArtifact(registryID, namespace, artifactName, version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get artifact: %v", err), http.StatusInternalServerError)
		return
	}

	if artifact == nil {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"registry":     registryID,
		"namespace":    namespace,
		"artifactName": artifactName,
		"version":      version,
		"artifact":     artifact,
	})
}

// DownloadArtifact downloads an artifact
func (pm *ProxyManager) DownloadArtifact(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	artifactName := chi.URLParam(r, "artifactName")
	version := chi.URLParam(r, "version")
	registryID := r.URL.Query().Get("registry")

	artifact, err := pm.db.GetArtifact(registryID, namespace, artifactName, version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get artifact: %v", err), http.StatusInternalServerError)
		return
	}

	if artifact == nil {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}

	// Get artifact data from storage
	data, err := pm.storage.GetArtifact(registryID, namespace, artifactName, version)
	if err != nil || data == nil {
		http.Error(w, "Artifact data not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s", artifactName, version))
	w.Header().Set("X-Content-Digest", artifact.Digest)
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
