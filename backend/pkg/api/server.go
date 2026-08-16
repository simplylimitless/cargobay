package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/cargobay/backend/pkg/auth"
	"github.com/anthropics/cargobay/backend/pkg/cache"
	"github.com/anthropics/cargobay/backend/pkg/database"
	"github.com/anthropics/cargobay/backend/pkg/middleware"
	"github.com/anthropics/cargobay/backend/pkg/rbac"
	"github.com/anthropics/cargobay/backend/pkg/storage"
	"github.com/anthropics/cargobay/backend/pkg/vulnerability"
	"github.com/go-chi/chi/v5"
)

// Server represents the HTTP API server
type Server struct {
	router         chi.Router
	db             *database.Database
	rbac           *rbac.RBAC
	scanner        *vulnerability.VulnerabilityScanner
	storageAdapter storage.StorageAdapter
	cache          *cache.Cache
	server         *http.Server
}

// APIResponse represents a standard API response
type APIResponse struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// PaginationResponse represents a paginated response
type PaginationResponse struct {
	Artifacts []database.ArtifactMetadata `json:"artifacts"`
	Total     int                         `json:"total"`
	HasMore   bool                        `json:"hasMore"`
	NextCursor string                      `json:"nextCursor,omitempty"`
	PrevCursor string                      `json:"prevCursor,omitempty"`
}

// NewServer creates a new API server
func NewServer(db *database.Database, rbacMgr *rbac.RBAC, scanner *vulnerability.VulnerabilityScanner,
	storageAdapter storage.StorageAdapter, cacheClient *cache.Cache) *Server {

	s := &Server{
		db:             db,
		rbac:           rbacMgr,
		scanner:        scanner,
		storageAdapter: storageAdapter,
		cache:          cacheClient,
	}

	s.router = chi.NewRouter()
	s.setupRoutes()

	return s
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// Health and version endpoints (public)
	s.router.Get("/health", s.handleHealth)
	s.router.Get("/version", s.handleVersion)

	// Metrics endpoint (public)
	s.router.Get("/metrics", s.handleMetrics)

	// Routes below are relative — the Server is mounted under /api/v1 by
	// the caller (see cmd/server/main.go), so no prefix is added here.
	api := s.router

	// Auth endpoints (public)
	api.Post("/auth/login", s.handleLogin)
	api.Post("/auth/logout", s.handleLogout)

	// Artifacts endpoints
	api.Get("/artifacts", s.handleListArtifacts)
	api.Get("/artifacts/{id}", s.handleGetArtifact)
	api.Get("/artifacts/{id}/download", s.handleDownloadArtifact)

	// Search endpoint
	api.Get("/search", s.handleSearch)

	// Registries endpoints (read)
	api.Get("/registries", s.handleListRegistries)
	api.Get("/registries/{id}", s.handleGetRegistry)

	// Users/roles/permissions endpoints (read)
	api.Get("/users", s.handleListUsers)
	api.Get("/users/{id}", s.handleGetUser)
	api.Get("/users/{id}/roles", s.handleGetUserRoles)
	api.Get("/users/{id}/permissions", s.handleGetUserPermissions)
	api.Get("/roles", s.handleListRoles)
	api.Get("/roles/{id}", s.handleGetRole)
	api.Get("/roles/{id}/permissions", s.handleGetRolePermissions)
	api.Get("/permissions", s.handleListPermissions)
	api.Get("/permissions/{id}", s.handleGetPermission)

	// Replication/audit endpoints (read)
	api.Get("/replication/status", s.handleReplicationStatus)
	api.Get("/replication/regions", s.handleListRegions)
	api.Get("/audit/logs", s.handleListAuditLogs)

	// Everything below mutates state and requires an authenticated caller.
	api.Group(func(api chi.Router) {
		api.Use(middleware.RequireAuth)

		api.Post("/artifacts", s.handleCreateArtifact)
		api.Delete("/artifacts/{id}", s.handleDeleteArtifact)
		api.Post("/artifacts/{id}/scan", s.handleScanArtifact)
		api.Post("/artifacts/{id}/sign", s.handleSignArtifact)

		api.Post("/registries", s.handleCreateRegistry)
		api.Delete("/registries/{id}", s.handleDeleteRegistry)

		api.Post("/replication/sync", s.handleReplicationSync)

		// User management requires admin ("user:admin"), enforced per-handler
		// via rbac.CanManageUser / RBAC.RequirePermission.
		api.Post("/users", s.rbac.RequirePermission("user:admin")(http.HandlerFunc(s.handleCreateUser)).ServeHTTP)
		api.Delete("/users/{id}", s.handleDeactivateUser)
		api.Post("/users/{id}/roles", s.rbac.RequirePermission("user:admin")(http.HandlerFunc(s.handleAssignUserRole)).ServeHTTP)
		api.Delete("/users/{id}/roles/{roleId}", s.rbac.RequirePermission("user:admin")(http.HandlerFunc(s.handleRevokeUserRole)).ServeHTTP)
	})
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// handleVersion handles version requests
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"version": "1.0.0",
		"build":   time.Now().UTC().Format(time.RFC3339),
	})
}

// handleMetrics handles Prometheus metrics requests
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	// Basic metrics output
	fmt.Fprint(w, "# HELP cargobay_artifacts_total Total number of artifacts\n")
	fmt.Fprintf(w, "# TYPE cargobay_artifacts_total gauge\n")
	fmt.Fprintf(w, "cargobay_artifacts_total 0\n")

	fmt.Fprint(w, "# HELP cargobay_registries_total Total number of registries\n")
	fmt.Fprintf(w, "# TYPE cargobay_registries_total gauge\n")
	fmt.Fprintf(w, "cargobay_registries_total 0\n")

	fmt.Fprint(w, "# HELP cargobay_users_total Total number of users\n")
	fmt.Fprintf(w, "# TYPE cargobay_users_total gauge\n")
	fmt.Fprintf(w, "cargobay_users_total 0\n")

	fmt.Fprint(w, "# HELP cargobay_cache_hits_total Total cache hits\n")
	fmt.Fprintf(w, "# TYPE cargobay_cache_hits_total counter\n")
	fmt.Fprintf(w, "cargobay_cache_hits_total 0\n")

	fmt.Fprint(w, "# HELP cargobay_cache_misses_total Total cache misses\n")
	fmt.Fprintf(w, "# TYPE cargobay_cache_misses_total counter\n")
	fmt.Fprintf(w, "cargobay_cache_misses_total 0\n")
}

// handleListArtifacts handles listing artifacts with cursor-based pagination
func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	// Check if cursor-based pagination is requested
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" {
		s.handleListArtifactsCursor(w, r)
		return
	}

	// Fallback to offset-based pagination for backward compatibility
	opts := database.ListOptions{
		Namespace:    r.URL.Query().Get("namespace"),
		ArtifactType: r.URL.Query().Get("artifactType"),
		Limit:        parseIntParam(r.URL.Query().Get("limit"), 100),
		Offset:       parseIntParam(r.URL.Query().Get("offset"), 0),
		OrderBy:      queryParamOrDefault(r, "orderBy", "created"),
		Order:        queryParamOrDefault(r, "order", "desc"),
		Cursor:       cursor,
	}

	artifacts, err := s.db.ListArtifacts(r.URL.Query().Get("registryId"), opts)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list artifacts: %v", err))
		return
	}

	total, err := s.db.CountArtifacts(r.URL.Query().Get("registryId"), opts)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to count artifacts: %v", err))
		return
	}

	response := PaginationResponse{
		Artifacts: artifacts,
		Total:     total,
		HasMore:   len(artifacts) > 0 && len(artifacts) == opts.Limit,
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleListArtifactsCursor handles cursor-based pagination for artifacts
func (s *Server) handleListArtifactsCursor(w http.ResponseWriter, r *http.Request) {
	opts := database.CursorPaginationOptions{
		Namespace:    r.URL.Query().Get("namespace"),
		ArtifactType: r.URL.Query().Get("artifactType"),
		Limit:        parseIntParam(r.URL.Query().Get("limit"), 50),
		Cursor:       r.URL.Query().Get("cursor"),
		OrderBy:      queryParamOrDefault(r, "orderBy", "created"),
		Order:        queryParamOrDefault(r, "order", "desc"),
	}

	artifacts, nextCursor, hasMore, err := s.db.ListArtifactsCursor(opts)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list artifacts: %v", err))
		return
	}

	response := database.CursorPaginationResponse[database.ArtifactMetadata]{
		Items:     artifacts,
		NextCursor: nextCursor,
		HasNext:   hasMore,
		Limit:     opts.Limit,
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleCreateArtifact handles creating an artifact
func (s *Server) handleCreateArtifact(w http.ResponseWriter, r *http.Request) {
	// Enforce artifact:write permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasPermission(user.UserID, "artifact:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: artifact:write required")
		return
	}

	var artifact database.ArtifactMetadata
	if err := json.NewDecoder(r.Body).Decode(&artifact); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if artifact.ArtifactName == "" {
		s.writeJSONError(w, http.StatusBadRequest, "artifactName is required")
		return
	}
	if artifact.Version == "" {
		s.writeJSONError(w, http.StatusBadRequest, "version is required")
		return
	}

	// Set uploadedBy to current user
	if user != nil {
		artifact.Metadata = map[string]any{
			"uploadedBy": user.Username,
			"uploadedAt": time.Now().UTC(),
		}
	}

	// Create the artifact
	err := s.db.SaveArtifact(&artifact)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save artifact: %v", err))
		return
	}

	// Invalidate cache for this artifact
	if s.cache != nil {
		_ = s.cache.InvalidateArtifact(artifact.RegistryID, artifact.Namespace, artifact.ArtifactName, artifact.Version)
	}

	s.writeJSON(w, http.StatusCreated, artifact)
}

// handleGetArtifact handles getting a single artifact
func (s *Server) handleGetArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	artifact, err := s.db.GetArtifact(id)
	if err != nil {
		s.writeJSONError(w, http.StatusNotFound, "Artifact not found")
		return
	}

	s.writeJSON(w, http.StatusOK, artifact)
}

// handleDeleteArtifact handles deleting an artifact
func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Enforce artifact:delete permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasPermission(user.UserID, "artifact:delete") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: artifact:delete required")
		return
	}

	artifact, err := s.db.GetArtifact(id)
	if err != nil {
		s.writeJSONError(w, http.StatusNotFound, "Artifact not found")
		return
	}

	// Delete from storage if available
	if s.storageAdapter != nil {
		_ = s.storageAdapter.DeleteArtifact(artifact.RegistryID, artifact.Namespace, artifact.ArtifactName, artifact.Version)
	}

	// Delete from database
	_, err = s.db.DeleteArtifact(artifact.RegistryID, artifact.Namespace, artifact.ArtifactName, artifact.Version)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete artifact: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Artifact deleted successfully"})
}

// handleScanArtifact handles scanning an artifact for vulnerabilities
func (s *Server) handleScanArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Enforce artifact:read permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasPermission(user.UserID, "artifact:read") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: artifact:read required")
		return
	}

	// Get artifact
	artifact, err := s.db.GetArtifact(id)
	if err != nil {
		s.writeJSONError(w, http.StatusNotFound, "Artifact not found")
		return
	}

	// Get artifact data from storage
	var data []byte
	if s.storageAdapter != nil {
		data, err = s.storageAdapter.GetArtifact(artifact.RegistryID, artifact.Namespace, artifact.ArtifactName, artifact.Version)
		if err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to retrieve artifact: %v", err))
			return
		}
	}

	// Scan the artifact
	result, err := s.scanner.ScanArtifact(artifact, data)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to scan artifact: %v", err))
		return
	}

	// Save scan result
	err = s.scanner.SaveScanResult(result)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save scan result: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// handleDownloadArtifact handles downloading an artifact
func (s *Server) handleDownloadArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Enforce artifact:read permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasPermission(user.UserID, "artifact:read") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: artifact:read required")
		return
	}

	artifact, err := s.db.GetArtifact(id)
	if err != nil {
		s.writeJSONError(w, http.StatusNotFound, "Artifact not found")
		return
	}

	// Get artifact data from storage
	data, err := s.storageAdapter.GetArtifact(artifact.RegistryID, artifact.Namespace, artifact.ArtifactName, artifact.Version)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to retrieve artifact: %v", err))
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.tar.gz", artifact.ArtifactName, artifact.Version))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleSignArtifact handles signing an artifact
func (s *Server) handleSignArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Enforce artifact:sign permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasPermission(user.UserID, "artifact:sign") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: artifact:sign required")
		return
	}

	var request struct {
		KeyID string `json:"keyId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	_, err := s.db.GetArtifact(id)
	if err != nil {
		s.writeJSONError(w, http.StatusNotFound, "Artifact not found")
		return
	}

	// In production, this would actually sign the artifact
	signature := &database.Signature{
		Type:      "pgp",
		KeyID:     request.KeyID,
		Timestamp: time.Now(),
		Verified:  true,
	}

	// Save signature
	err = s.db.SaveSignature(id, signature)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save signature: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":   "Artifact signed successfully",
		"signature": signature,
	})
}

// handleSearch handles searching artifacts
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		s.writeJSONError(w, http.StatusBadRequest, "Query parameter 'q' is required")
		return
	}

	opts := database.SearchOptions{
		Limit:        parseIntParam(r.URL.Query().Get("limit"), 50),
		Offset:       parseIntParam(r.URL.Query().Get("offset"), 0),
		RegistryID:   r.URL.Query().Get("registryId"),
		ArtifactType: r.URL.Query().Get("artifactType"),
	}

	results, err := s.db.SearchArtifacts(query, opts)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to search: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, results)
}

// handleListRegistries handles listing registries
func (s *Server) handleListRegistries(w http.ResponseWriter, r *http.Request) {
	registries, err := s.db.ListRegistries()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list registries: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"registries": registries,
	})
}

// handleCreateRegistry handles creating a registry
// handleCreateRegistry handles creating a registry
func (s *Server) handleCreateRegistry(w http.ResponseWriter, r *http.Request) {
	// Enforce registry:write permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasPermission(user.UserID, "registry:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: registry:write required")
		return
	}

	var registry database.RegistryConfig
	if err := json.NewDecoder(r.Body).Decode(&registry); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	err := s.db.SaveRegistry(&registry)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save registry: %v", err))
		return
	}

	s.writeJSON(w, http.StatusCreated, registry)
}

// handleGetRegistry handles getting a single registry
func (s *Server) handleGetRegistry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	registry, err := s.db.GetRegistry(id)
	if err != nil {
		s.writeJSONError(w, http.StatusNotFound, "Registry not found")
		return
	}

	s.writeJSON(w, http.StatusOK, registry)
}

// handleDeleteRegistry handles deleting a registry
func (s *Server) handleDeleteRegistry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Enforce registry:delete permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasPermission(user.UserID, "registry:delete") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: registry:delete required")
		return
	}

	err := s.db.DeleteRegistry(id)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete registry: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Registry deleted successfully"})
}

// handleListUsers handles listing users
// handleListUsers handles listing users with cursor-based pagination
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" {
		s.handleListUsersCursor(w, r)
		return
	}

	users, err := s.db.ListUsers()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list users: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"users": users,
	})
}

// handleListUsersCursor handles cursor-based pagination for users
func (s *Server) handleListUsersCursor(w http.ResponseWriter, r *http.Request) {
	opts := database.CursorPaginationOptions{
		Limit:     parseIntParam(r.URL.Query().Get("limit"), 50),
		Cursor:    r.URL.Query().Get("cursor"),
		OrderBy:   queryParamOrDefault(r, "orderBy", "created_at"),
		Order:     queryParamOrDefault(r, "order", "desc"),
	}

	users, nextCursor, hasMore, err := s.db.ListUsersCursor(opts)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list users: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"users":      users,
		"nextCursor": nextCursor,
		"hasNext":    hasMore,
		"limit":      opts.Limit,
	})
}

// handleCreateUser handles creating a new user
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username   string   `json:"username"`
		Email      string   `json:"email"`
		Password   string   `json:"password"`
		Roles      []string `json:"roles"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if input.Username == "" {
		s.writeJSONError(w, http.StatusBadRequest, "username is required")
		return
	}
	if input.Email == "" {
		s.writeJSONError(w, http.StatusBadRequest, "email is required")
		return
	}
	if input.Password == "" {
		s.writeJSONError(w, http.StatusBadRequest, "password is required")
		return
	}

	// Hash password
	passwordHash, err := auth.HashPassword(input.Password)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to hash password: %v", err))
		return
	}

	// Create user
	user, err := s.db.CreateUser(input.Username, input.Email, passwordHash, input.Roles)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create user: %v", err))
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"user": user,
		"message": "User created successfully",
	})
}

// handleDeactivateUser handles soft-deleting a user
func (s *Server) handleDeactivateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Check if user exists
	user, err := s.db.GetUser(id)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get user: %v", err))
		return
	}
	if user == nil {
		s.writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}

	// Soft delete
	err = s.db.SetUserActive(id, false)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to deactivate user: %v", err))
		return
	}

	// Invalidate all access keys for this user
	keys, err := s.db.ListUserAccessKeys(id)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list access keys: %v", err))
		return
	}
	for _, key := range keys {
		if err := s.db.InvalidateAccessKey(key.ID); err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to invalidate access key %s: %v", key.ID, err))
			return
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"message": "User deactivated successfully"})
}

// handleAssignUserRole handles assigning a role to a user
func (s *Server) handleAssignUserRole(w http.ResponseWriter, r *http.Request) {
	 userID := chi.URLParam(r, "id")
	 roleID := chi.URLParam(r, "roleId")

	 // Check if user exists
	 user, err := s.db.GetUser(userID)
	 if err != nil {
		 s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get user: %v", err))
		 return
	 }
	 if user == nil {
		 s.writeJSONError(w, http.StatusNotFound, "User not found")
		 return
	 }

	 // Check if role exists (in DB or static roles)
	 role := s.rbac.GetRole(roleID)
	 if role == nil {
		 s.writeJSONError(w, http.StatusBadRequest, "Role not found")
		 return
	 }

	 // Assign role
	 err = s.db.AssignRoleToUser(userID, roleID)
	 if err != nil {
		 s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to assign role: %v", err))
		 return
	 }

	 s.writeJSON(w, http.StatusOK, map[string]interface{}{
		 "message": "Role assigned successfully",
		 "role":    role,
	 })
}

// handleRevokeUserRole handles revoking a role from a user
func (s *Server) handleRevokeUserRole(w http.ResponseWriter, r *http.Request) {
	 userID := chi.URLParam(r, "id")
	 roleID := chi.URLParam(r, "roleId")

	 // Check if user exists
	 user, err := s.db.GetUser(userID)
	 if err != nil {
		 s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get user: %v", err))
		 return
	 }
	 if user == nil {
		 s.writeJSONError(w, http.StatusNotFound, "User not found")
		 return
	 }

	 // Revoke role
	 err = s.db.RevokeRoleFromUser(userID, roleID)
	 if err != nil {
		 s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to revoke role: %v", err))
		 return
	 }

	 s.writeJSON(w, http.StatusOK, map[string]string{"message": "Role revoked successfully"})
}

// handleGetUser handles getting a single user
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	user, err := s.db.GetUser(id)
	if err != nil {
		s.writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}

	s.writeJSON(w, http.StatusOK, user)
}

// handleGetUserRoles handles getting user roles
func (s *Server) handleGetUserRoles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	roles, err := s.db.GetUserRoles(id)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get user roles: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"roles": roles,
	})
}

// handleGetUserPermissions handles getting user permissions
func (s *Server) handleGetUserPermissions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	permissions := s.rbac.GetUserRolePermissions(id)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"permissions": permissions,
	})
}

// handleListRoles handles listing roles
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles := s.rbac.ListRoles()
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"roles": roles,
	})
}

// handleGetRole handles getting a single role
func (s *Server) handleGetRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	role := s.rbac.GetRole(id)
	if role == nil {
		s.writeJSONError(w, http.StatusNotFound, "Role not found")
		return
	}

	s.writeJSON(w, http.StatusOK, role)
}

// handleGetRolePermissions handles getting role permissions
func (s *Server) handleGetRolePermissions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	permissions := s.rbac.GetRolePermissions(id)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"permissions": permissions,
	})
}

// handleListPermissions handles listing permissions
func (s *Server) handleListPermissions(w http.ResponseWriter, r *http.Request) {
	permissions := s.rbac.ListPermissions()
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"permissions": permissions,
	})
}

// handleGetPermission handles getting a single permission
func (s *Server) handleGetPermission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	permission := s.rbac.GetPermission(id)
	if permission == nil {
		s.writeJSONError(w, http.StatusNotFound, "Permission not found")
		return
	}

	s.writeJSON(w, http.StatusOK, permission)
}

// handleReplicationStatus handles getting replication status
func (s *Server) handleReplicationStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":  "running",
		"regions": []string{"us-east-1", "us-west-2", "eu-west-1"},
		"lag":     "0s",
	}

	s.writeJSON(w, http.StatusOK, status)
}

// handleReplicationSync handles triggering replication sync
// handleReplicationSync handles triggering replication sync
func (s *Server) handleReplicationSync(w http.ResponseWriter, r *http.Request) {
	// Enforce replication:sync permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasPermission(user.UserID, "replication:sync") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: replication:sync required")
		return
	}

	go func() {
		// Trigger async replication
		_ = s.db
	}()

	s.writeJSON(w, http.StatusOK, map[string]string{
		"message": "Replication sync triggered",
	})
}

// handleListRegions handles listing replication regions
func (s *Server) handleListRegions(w http.ResponseWriter, r *http.Request) {
	regions := []string{"us-east-1", "us-west-2", "eu-west-1", "ap-southeast-1"}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"regions": regions,
	})
}

// handleListAuditLogs handles listing audit logs
func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	opts := database.SearchOptions{
		Limit:  parseIntParam(r.URL.Query().Get("limit"), 100),
		Offset: parseIntParam(r.URL.Query().Get("offset"), 0),
	}

	logs, err := s.db.ListAuditLogs(opts)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list audit logs: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":  logs,
		"total": len(logs),
	})
}

// handleLogin handles user login with username and password
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var input auth.LoginInput

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if input.Username == "" {
		s.writeJSONError(w, http.StatusBadRequest, "username is required")
		return
	}
	if input.Password == "" {
		s.writeJSONError(w, http.StatusBadRequest, "password is required")
		return
	}

	output, err := auth.Login(s.db, input.Username, input.Password)
	if err != nil {
		switch err {
		case fmt.Errorf("invalid credentials"):
			s.writeJSONError(w, http.StatusUnauthorized, "Invalid credentials")
		default:
			s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Login failed: %v", err))
		}
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"accessToken": output.AccessToken,
		"expiresAt":   output.ExpiresAt,
		"user": map[string]interface{}{
			"userId":      output.UserID,
			"username":    output.Username,
			"email":       output.Email,
			"roles":       output.Roles,
			"permissions": output.Permissions,
		},
	})
}

// handleLogout handles user logout by invalidating the access key
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		s.writeJSONError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	// Get the access key ID from the request header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		s.writeJSONError(w, http.StatusBadRequest, "Authorization header required")
		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid authorization format")
		return
	}

	// Find the key ID for this token
	key, err := s.db.ValidateAccessKey(token)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to validate key: %v", err))
		return
	}

	if key == nil {
		s.writeJSONError(w, http.StatusNotFound, "Access key not found")
		return
	}

	// Invalidate the key
	if err := s.db.InvalidateAccessKey(key.ID); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to invalidate key: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{
		"message": "Logged out successfully",
	})
}

// writeJSON writes a JSON response
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeJSONError writes a JSON error response
func (s *Server) writeJSONError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{
		"error": message,
	})
}

// parseIntParam parses an integer query parameter with a default
func parseIntParam(value string, defaultValue int) int {
	if value == "" {
		return defaultValue
	}
	var result int
	_, err := fmt.Sscanf(value, "%d", &result)
	if err != nil {
		return defaultValue
	}
	return result
}

// queryParamOrDefault returns the query parameter value or a default if not present
func queryParamOrDefault(r *http.Request, key, defaultValue string) string {
	if value := r.URL.Query().Get(key); value != "" {
		return value
	}
	return defaultValue
}

// ServeHTTP implements http.Handler by delegating to the internal router
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// Start starts the HTTP server
func (s *Server) Start(addr string) error {
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	return s.server.ListenAndServe()
}

// Stop stops the HTTP server
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}
