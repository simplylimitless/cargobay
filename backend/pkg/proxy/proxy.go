// Package proxy provides the main proxy manager
//
// The proxy package orchestrates all proxy handlers (npm, Maven, Docker)
// and provides a unified API for artifact operations.
package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/go-chi/chi/v5"
)

// proxyStore is the subset of *database.Database's methods ProxyManager
// depends on. Narrowed to an interface so tests can substitute a fake.
type proxyStore interface {
	SearchArtifacts(query string, opts database.SearchOptions) ([]database.ArtifactMetadata, error)
	GetArtifactByParams(registryID, namespace, artifactName, version string) (*database.ArtifactMetadata, error)
}

// proxyCache is the subset of *cache.Cache's methods ProxyManager depends
// on. Narrowed to an interface so tests can substitute a fake.
type proxyCache interface {
	Get(key string, value interface{}) error
	SetWithTTL(key string, value interface{}, ttl time.Duration) error
}

// ProxyManager manages all proxy handlers
type ProxyManager struct {
	db         proxyStore
	storage    storage.StorageAdapter
	cache      proxyCache
	registries []database.RegistryConfig
}

// New creates a new ProxyManager
func New(db proxyStore, storage storage.StorageAdapter, cache proxyCache, registries []database.RegistryConfig) *ProxyManager {
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

// SearchArtifacts searches across all artifacts with caching
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

	// Generate cache key for search results
	cacheKey := fmt.Sprintf("search:v2:%s:%s:%s:%d:%d", query, registryID, artifactType, limit, offset)

	// Try to get cached results
	var cachedResults map[string]interface{}
	if err := pm.cache.Get(cacheKey, &cachedResults); err == nil && cachedResults != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		json.NewEncoder(w).Encode(cachedResults)
		return
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

	// Calculate total (for pagination metadata)
	total := len(artifacts)
	if total >= limit {
		total = limit // For now, actual total would require a separate COUNT query
	}

	results := map[string]interface{}{
		"query":    query,
		"results":  artifacts,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")

	// Store in cache with 5-minute TTL
	if err := pm.cache.SetWithTTL(cacheKey, results, 5*time.Minute); err != nil {
		// Log but don't fail the request if cache fails
		fmt.Printf("Failed to cache search results: %v\n", err)
	}

	json.NewEncoder(w).Encode(results)
}

// GetArtifactInfo returns artifact information
func (pm *ProxyManager) GetArtifactInfo(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	artifactName := chi.URLParam(r, "artifactName")
	version := chi.URLParam(r, "version")
	registryID := r.URL.Query().Get("registry")

	artifact, err := pm.db.GetArtifactByParams(registryID, namespace, artifactName, version)
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

	artifact, err := pm.db.GetArtifactByParams(registryID, namespace, artifactName, version)
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
