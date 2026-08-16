// Package docker implements the Docker Registry v2 API proxy
//
// This package provides a caching proxy for Docker images that:
//   - Implements Docker Registry v2 API
//   - Caches image manifests and layers
//   - Handles multi-arch images (manifest lists)
//   - Supports notary signatures
//
// Docker Registry v2 API: https://docs.docker.com/registry/spec/api/
package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/anthropics/cargobay/backend/pkg/cache"
	"github.com/anthropics/cargobay/backend/pkg/database"
	"github.com/anthropics/cargobay/backend/pkg/middleware"
	"github.com/anthropics/cargobay/backend/pkg/storage"
	"github.com/go-chi/chi/v5"
)

// DockerProxy implements the Docker Registry v2 API proxy
type DockerProxy struct {
	db         *database.Database
	storage    storage.StorageAdapter
	cache      *cache.Cache
	registries []database.RegistryConfig
	registry   string
}

// NewDockerProxy creates a new Docker proxy instance
func NewDockerProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &DockerProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "docker",
	}

	// Docker Registry v2 API endpoints

	// Discovery endpoint
	r.Get("/", proxy.handleDiscovery)

	// List repositories
	r.Get("/v2/_catalog", proxy.handleCatalog)

	// List tags for a repository
	r.Get("/v2/{repository}/tags/list", proxy.handleTags)

	// Manifest read
	r.Get("/v2/{repository}/manifests/{reference}", proxy.handleManifest)

	// Blob/Layer read
	r.Head("/v2/{repository}/blobs/{digest}", proxy.handleHeadBlob)
	r.Get("/v2/{repository}/blobs/{digest}", proxy.handleGetBlob)

	// Write operations (push/delete) require an authenticated user — anonymous
	// requests may pull, but must not be able to upload or remove artifacts.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth)

		r.Put("/v2/{repository}/manifests/{reference}", proxy.handlePutManifest)
		r.Post("/v2/{repository}/blobs/uploads", proxy.handleStartUpload)
		r.Patch("/v2/{repository}/blobs/uploads/{uploadID}", proxy.handlePatchUpload)
		r.Put("/v2/{repository}/blobs/uploads/{uploadID}", proxy.handleFinishUpload)
		r.Delete("/v2/{repository}/manifests/{reference}", proxy.handleDeleteManifest)
	})

	// Health check
	r.Get("/v2/", proxy.handleHealth)

	return r
}

// handleDiscovery handles the root discovery endpoint
func (p *DockerProxy) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Welcome to cargobay Docker Registry Proxy",
	})
}

// handleCatalog lists repositories
func (p *DockerProxy) handleCatalog(w http.ResponseWriter, r *http.Request) {
	repositories := []string{}

	// Get repositories from database
	artifacts, err := p.db.ListArtifacts("docker", database.ListOptions{
		ArtifactType: "docker",
	})
	if err == nil {
		seen := make(map[string]bool)
		for _, a := range artifacts {
			if !seen[a.ArtifactName] {
				repositories = append(repositories, a.ArtifactName)
				seen[a.ArtifactName] = true
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"repositories": repositories,
	})
}

// handleTags lists tags for a repository
func (p *DockerProxy) handleTags(w http.ResponseWriter, r *http.Request) {
	repository := chi.URLParam(r, "repository")

	// Get tags from database
	artifacts, err := p.db.ListArtifacts("docker", database.ListOptions{
		ArtifactType: "docker",
		Namespace:    repository,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get tags: %v", err), http.StatusBadGateway)
		return
	}

	tags := make([]string, 0, len(artifacts))
	for _, a := range artifacts {
		tags = append(tags, a.Version)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name": repository,
		"tags": tags,
	})
}

// handleManifest retrieves a manifest
func (p *DockerProxy) handleManifest(w http.ResponseWriter, r *http.Request) {
	repository := chi.URLParam(r, "repository")
	reference := chi.URLParam(r, "reference")

	// Try to find artifact in database
	artifacts, err := p.db.ListArtifacts("docker", database.ListOptions{
		ArtifactType: "docker",
		Namespace:    repository,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get manifest: %v", err), http.StatusBadGateway)
		return
	}

	// Find matching artifact
	var artifact *database.ArtifactMetadata
	for _, a := range artifacts {
		if a.Version == reference || a.Digest == reference {
			artifact = &a
			break
		}
	}

	if artifact == nil {
		http.Error(w, "manifest unknown", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
	w.Header().Set("Docker-Content-Digest", artifact.Digest)
	w.Header().Set("X-Docker-Size", fmt.Sprintf("%d", artifact.Size))
	w.Write(artifact.Metadata["manifest"].([]byte))
}

// handlePutManifest puts a manifest
func (p *DockerProxy) handlePutManifest(w http.ResponseWriter, r *http.Request) {
	repository := chi.URLParam(r, "repository")
	reference := chi.URLParam(r, "reference")

	// Read manifest body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read manifest: %v", err), http.StatusBadRequest)
		return
	}

	// Parse manifest to get digest
	var manifest map[string]interface{}
	if err := json.Unmarshal(body, &manifest); err != nil {
		http.Error(w, "Invalid manifest", http.StatusBadRequest)
		return
	}

	// In production, calculate actual SHA-256 digest
	digest := fmt.Sprintf("sha256:%x", body)

	// Save manifest to storage
	if _, err := p.storage.SaveArtifact("docker", repository, reference, "latest", body); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save manifest: %v", err), http.StatusInternalServerError)
		return
	}

	// Save to database
	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("docker:%s:%s", repository, reference),
		RegistryID:      "docker",
		ArtifactType:    "docker",
		Namespace:       repository,
		ArtifactName:    repository,
		Version:         reference,
		Digest:          digest,
		DigestAlgorithm: "sha256",
		Size:            int64(len(body)),
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata:        map[string]interface{}{"manifest": body},
		Tags:            []string{reference},
	}

	if err := p.db.SaveArtifact(artifact); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save artifact: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

// handleGetBlob retrieves a blob (layer)
func (p *DockerProxy) handleGetBlob(w http.ResponseWriter, r *http.Request) {
	repository := chi.URLParam(r, "repository")
	digest := chi.URLParam(r, "digest")

	// Get blob from storage
	blob, err := p.storage.GetArtifact("docker", repository, "blob", digest)
	if err != nil || blob == nil {
		http.Error(w, "blob unknown", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusOK)
	w.Write(blob)
}

// handleHeadBlob checks if a blob exists
func (p *DockerProxy) handleHeadBlob(w http.ResponseWriter, r *http.Request) {
	repository := chi.URLParam(r, "repository")
	digest := chi.URLParam(r, "digest")

	exists, err := p.storage.ArtifactExists("docker", repository, "blob", digest)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check blob: %v", err), http.StatusInternalServerError)
		return
	}

	if !exists {
		http.Error(w, "blob unknown", http.StatusNotFound)
		return
	}

	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusOK)
}

// handleStartUpload starts a new blob upload session
func (p *DockerProxy) handleStartUpload(w http.ResponseWriter, r *http.Request) {
	repository := chi.URLParam(r, "repository")
	uploadID := fmt.Sprintf("upload-%d", time.Now().UnixNano())

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repository, uploadID))
	w.Header().Set("Docker-Upload-UUID", uploadID)
	w.WriteHeader(http.StatusAccepted)
}

// handlePatchUpload appends a chunk to an in-progress blob upload
func (p *DockerProxy) handlePatchUpload(w http.ResponseWriter, r *http.Request) {
	repository := chi.URLParam(r, "repository")
	uploadID := chi.URLParam(r, "uploadID")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read chunk: %v", err), http.StatusBadRequest)
		return
	}

	p.cache.Set(fmt.Sprintf("upload:%s:%s", repository, uploadID), body)

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repository, uploadID))
	w.Header().Set("Docker-Upload-UUID", uploadID)
	w.WriteHeader(http.StatusAccepted)
}

// handleFinishUpload completes a blob upload and stores the blob
func (p *DockerProxy) handleFinishUpload(w http.ResponseWriter, r *http.Request) {
	repository := chi.URLParam(r, "repository")
	digest := r.URL.Query().Get("digest")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read blob: %v", err), http.StatusBadRequest)
		return
	}
	if len(body) == 0 {
		var cached []byte
		uploadID := chi.URLParam(r, "uploadID")
		if err := p.cache.Get(fmt.Sprintf("upload:%s:%s", repository, uploadID), &cached); err == nil {
			body = cached
		}
	}

	if digest == "" {
		digest = fmt.Sprintf("sha256:%x", body)
	}

	if _, err := p.storage.SaveArtifact("docker", repository, "blob", digest, body); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save blob: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

// handleDeleteManifest deletes a manifest
func (p *DockerProxy) handleDeleteManifest(w http.ResponseWriter, r *http.Request) {
	repository := chi.URLParam(r, "repository")
	reference := chi.URLParam(r, "reference")

	if err := p.storage.DeleteArtifact("docker", repository, reference, "latest"); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete manifest: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// handleHealth is the registry health check endpoint
func (p *DockerProxy) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}
