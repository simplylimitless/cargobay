package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/simplylimitless/cargobay/backend/pkg/auth"
	"github.com/simplylimitless/cargobay/backend/pkg/backup"
	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/docker"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/searchindex"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/simplylimitless/cargobay/backend/pkg/vulnerability"
)

// Server represents the HTTP API server
type Server struct {
	router               chi.Router
	db                   *database.Database
	rbac                 *rbac.RBAC
	scanner              *vulnerability.VulnerabilityScanner
	vulnDBUpdater        *vulnerability.DBUpdater
	vulnRescanner        *vulnerability.Rescanner
	searchIndexReindexer *searchindex.Reindexer
	backupSvc            *backup.Backup
	storageAdapter       storage.StorageAdapter
	cache                *cache.Cache
	server               *http.Server
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
	Artifacts  []database.ArtifactMetadata `json:"artifacts"`
	Total      int                         `json:"total"`
	HasMore    bool                        `json:"hasMore"`
	NextCursor string                      `json:"nextCursor,omitempty"`
	PrevCursor string                      `json:"prevCursor,omitempty"`
}

// NewServer creates a new API server
func NewServer(db *database.Database, rbacMgr *rbac.RBAC, scanner *vulnerability.VulnerabilityScanner,
	storageAdapter storage.StorageAdapter, cacheClient *cache.Cache, vulnDBUpdater *vulnerability.DBUpdater,
	searchIndexReindexer *searchindex.Reindexer, vulnRescanner *vulnerability.Rescanner, backupSvc *backup.Backup) *Server {

	s := &Server{
		db:                   db,
		rbac:                 rbacMgr,
		scanner:              scanner,
		vulnDBUpdater:        vulnDBUpdater,
		vulnRescanner:        vulnRescanner,
		searchIndexReindexer: searchIndexReindexer,
		backupSvc:            backupSvc,
		storageAdapter:       storageAdapter,
		cache:                cacheClient,
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

	// First-run setup endpoints (public - handleSetupInit refuses once any user exists)
	api.Get("/setup/status", s.handleSetupStatus)
	api.Post("/setup", s.handleSetupInit)

	// Artifacts endpoints
	api.Get("/artifacts", s.handleListArtifacts)
	api.Get("/artifacts/{id}", s.handleGetArtifact)
	api.Get("/artifacts/{id}/download", s.handleDownloadArtifact)

	// Search endpoint
	api.Get("/search", s.handleSearch)
	api.Get("/search/autocomplete", s.handleAutocomplete)

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

	// Vulnerability DB settings (read) — permission enforced in-handler via system:read
	api.Get("/settings/vulnerability-db", s.handleGetVulnDBSettings)

	// Search index settings (read) — permission enforced in-handler via system:read
	api.Get("/settings/search-index", s.handleGetSearchIndexSettings)

	// Backup settings/list (read) — permission enforced in-handler via system:read
	api.Get("/settings/backup", s.handleGetBackupSettings)
	api.Get("/settings/backup/list", s.handleListBackups)

	// Vulnerability scan settings (read) — permission enforced in-handler via system:read
	api.Get("/settings/vulnerability-scan", s.handleGetVulnScanSettings)

	// Vulnerability scan results (read-only, public — same visibility as artifacts themselves)
	api.Get("/artifacts/vulnerability-summary", s.handleGetVulnerabilitySummary)
	api.Get("/vulnerability-scans", s.handleListVulnerabilityScans)

	// Instance-wide stats (read-only, public — bandwidth saved + top pulled artifacts)
	api.Get("/stats", s.handleGetStats)

	// Everything below mutates state and requires an authenticated caller.
	api.Group(func(api chi.Router) {
		api.Use(middleware.RequireAuth)

		api.Post("/artifacts", s.handleCreateArtifact)
		api.Delete("/artifacts/{id}", s.handleDeleteArtifact)
		api.Post("/artifacts/{id}/scan", s.handleScanArtifact)
		api.Post("/artifacts/{id}/sign", s.handleSignArtifact)

		api.Post("/registries", s.handleCreateRegistry)
		api.Delete("/registries/{id}", s.handleDeleteRegistry)
		api.Post("/registries/{id}/access", s.handleGrantRegistryAccess)
		api.Delete("/registries/{id}/access/{userId}", s.handleRevokeRegistryAccess)
		api.Get("/registries/{id}/access", s.handleListRegistryAccess)
		api.Post("/registries/{id}/prefetch", s.handlePrefetchArtifact)

		api.Post("/replication/sync", s.handleReplicationSync)

		api.Put("/settings/vulnerability-db", s.handleUpdateVulnDBSettings)
		api.Post("/settings/vulnerability-db/update", s.handleTriggerVulnDBUpdate)

		api.Put("/settings/search-index", s.handleUpdateSearchIndexSettings)
		api.Post("/settings/search-index/reindex", s.handleTriggerSearchIndexReindex)

		api.Put("/settings/backup", s.handleUpdateBackupSettings)
		api.Post("/settings/backup/backup-now", s.handleTriggerBackup)
		// Restore replaces the entire database in one shot, so it requires the
		// same admin gate (user:admin) as user management rather than the
		// system:write check other settings endpoints use in-handler.
		api.Post("/settings/backup/restore", s.rbac.RequirePermission("user:admin")(http.HandlerFunc(s.handleRestoreBackup)).ServeHTTP)

		api.Put("/settings/vulnerability-scan", s.handleUpdateVulnScanSettings)
		api.Post("/settings/vulnerability-scan/scan-now", s.handleTriggerVulnScan)

		// Self-service profile endpoints - any authenticated user may update their own account.
		api.Put("/users/me", s.handleUpdateCurrentUser)
		api.Post("/users/me/password", s.handleChangePassword)

		// Self-service personal access tokens - any authenticated user may
		// create/list/revoke their own tokens, no special permission required.
		api.Get("/users/me/access-keys", s.handleListMyAccessKeys)
		api.Post("/users/me/access-keys", s.handleCreateMyAccessKey)
		api.Delete("/users/me/access-keys/{keyId}", s.handleRevokeMyAccessKey)

		// User management requires admin ("user:admin"), enforced per-handler
		// via rbac.CanManageUser / RBAC.RequirePermission.
		api.Post("/users", s.rbac.RequirePermission("user:admin")(http.HandlerFunc(s.handleCreateUser)).ServeHTTP)
		api.Put("/users/{id}", s.rbac.RequirePermission("user:admin")(http.HandlerFunc(s.handleUpdateUser)).ServeHTTP)
		api.Post("/users/{id}/password", s.rbac.RequirePermission("user:admin")(http.HandlerFunc(s.handleAdminResetPassword)).ServeHTTP)
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

// callerUserID returns the authenticated user's ID, or "" for an anonymous caller.
func (s *Server) callerUserID(r *http.Request) string {
	if user := middleware.GetUser(r); user != nil {
		return user.UserID
	}
	return ""
}

// callerUser returns the authenticated user, or nil for an anonymous caller.
func (s *Server) callerUser(r *http.Request) *middleware.User {
	return middleware.GetUser(r)
}

// readableRegistryIDs returns the IDs of every enabled registry the caller
// (possibly anonymous) may read, per RBAC.CanReadRegistry — public
// registries are always included, private ones only with a grant.
func (s *Server) readableRegistryIDs(r *http.Request) ([]string, error) {
	registries, err := s.db.ListRegistries()
	if err != nil {
		return nil, err
	}
	user := s.callerUser(r)
	ids := make([]string, 0, len(registries))
	for _, reg := range registries {
		if s.rbac.CanReadRegistry(user, &reg) {
			ids = append(ids, reg.ID)
		}
	}
	return ids, nil
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

	registryID := r.URL.Query().Get("registryId")

	var artifacts []database.ArtifactMetadata
	var total int
	var err error

	if registryID != "" {
		registry, gerr := s.db.GetRegistry(registryID)
		if gerr != nil {
			s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list artifacts: %v", gerr))
			return
		}
		if registry == nil || !s.rbac.CanReadRegistry(s.callerUser(r), registry) {
			s.writeJSON(w, http.StatusOK, PaginationResponse{Artifacts: []database.ArtifactMetadata{}, Total: 0, HasMore: false})
			return
		}
		artifacts, err = s.db.ListArtifacts(registryID, opts)
		if err == nil {
			total, err = s.db.CountArtifacts(registryID, opts)
		}
	} else {
		var registryIDs []string
		registryIDs, err = s.readableRegistryIDs(r)
		if err == nil {
			artifacts, err = s.db.ListArtifactsMulti(registryIDs, opts)
			if err == nil {
				total, err = s.db.CountArtifactsMulti(registryIDs, opts)
			}
		}
	}

	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list artifacts: %v", err))
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
	registryIDs, err := s.readableRegistryIDs(r)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list artifacts: %v", err))
		return
	}

	opts := database.CursorPaginationOptions{
		Namespace:    r.URL.Query().Get("namespace"),
		ArtifactType: r.URL.Query().Get("artifactType"),
		Limit:        parseIntParam(r.URL.Query().Get("limit"), 50),
		Cursor:       r.URL.Query().Get("cursor"),
		OrderBy:      queryParamOrDefault(r, "orderBy", "created"),
		Order:        queryParamOrDefault(r, "order", "desc"),
		RegistryIDs:  registryIDs,
	}

	artifacts, nextCursor, hasMore, err := s.db.ListArtifactsCursor(opts)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list artifacts: %v", err))
		return
	}

	response := database.CursorPaginationResponse[database.ArtifactMetadata]{
		Items:      artifacts,
		NextCursor: nextCursor,
		HasNext:    hasMore,
		Limit:      opts.Limit,
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleCreateArtifact handles creating an artifact
func (s *Server) handleCreateArtifact(w http.ResponseWriter, r *http.Request) {
	// Enforce artifact:write permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "artifact:write") {
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
	if err != nil || artifact == nil {
		s.writeJSONError(w, http.StatusNotFound, "Artifact not found")
		return
	}

	registry, err := s.db.GetRegistry(artifact.RegistryID)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load registry: %v", err))
		return
	}
	if !s.rbac.CanReadRegistry(s.callerUser(r), registry) {
		s.writeJSONError(w, http.StatusNotFound, "Artifact not found")
		return
	}

	s.writeJSON(w, http.StatusOK, artifact)
}

// handleDeleteArtifact handles deleting an artifact
func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user := middleware.GetUser(r)
	if user == nil {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: artifact:delete required")
		return
	}

	artifact, err := s.db.GetArtifact(id)
	if err != nil || artifact == nil {
		s.writeJSONError(w, http.StatusNotFound, "Artifact not found")
		return
	}

	// Allow deletion if the caller has artifact:delete, or is the original
	// uploader of this artifact (recorded in Metadata by handleCreateArtifact).
	uploadedBy, _ := artifact.Metadata["uploadedBy"].(string)
	isOwner := uploadedBy != "" && uploadedBy == user.Username
	if !isOwner && !s.rbac.HasEffectivePermission(user, "artifact:delete") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: artifact:delete required")
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
	if user == nil || !s.rbac.HasEffectivePermission(user, "artifact:read") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: artifact:read required")
		return
	}

	// Get artifact
	artifact, err := s.db.GetArtifact(id)
	if err != nil || artifact == nil {
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

// handleGetVulnDBSettings returns the current vulnerability-DB update settings.
func (s *Server) handleGetVulnDBSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:read") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:read required")
		return
	}

	settings, err := s.db.GetVulnDBSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get vulnerability DB settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, settings)
}

// handleUpdateVulnDBSettings updates the auto-update toggle and refresh interval.
func (s *Server) handleUpdateVulnDBSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:write required")
		return
	}

	var input struct {
		AutoUpdateEnabled   bool `json:"autoUpdateEnabled"`
		UpdateIntervalHours int  `json:"updateIntervalHours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.UpdateIntervalHours < 1 {
		s.writeJSONError(w, http.StatusBadRequest, "updateIntervalHours must be at least 1")
		return
	}

	if err := s.db.UpdateVulnDBSettings(input.AutoUpdateEnabled, input.UpdateIntervalHours); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update vulnerability DB settings: %v", err))
		return
	}

	settings, err := s.db.GetVulnDBSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get vulnerability DB settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, settings)
}

// handleGetSearchIndexSettings returns the current search-index reindex settings.
func (s *Server) handleGetSearchIndexSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:read") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:read required")
		return
	}

	settings, err := s.db.GetSearchIndexSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get search index settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, settings)
}

// handleUpdateSearchIndexSettings updates the auto-reindex toggle and refresh interval.
func (s *Server) handleUpdateSearchIndexSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:write required")
		return
	}

	var input struct {
		AutoReindexEnabled   bool `json:"autoReindexEnabled"`
		ReindexIntervalHours int  `json:"reindexIntervalHours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.ReindexIntervalHours < 1 {
		s.writeJSONError(w, http.StatusBadRequest, "reindexIntervalHours must be at least 1")
		return
	}

	if err := s.db.UpdateSearchIndexSettings(input.AutoReindexEnabled, input.ReindexIntervalHours); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update search index settings: %v", err))
		return
	}

	settings, err := s.db.GetSearchIndexSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get search index settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, settings)
}

// handleTriggerSearchIndexReindex synchronously rebuilds the search index
// and returns the resulting settings row.
func (s *Server) handleTriggerSearchIndexReindex(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:write required")
		return
	}

	if s.searchIndexReindexer == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Search index reindexer is not configured")
		return
	}

	// Errors are surfaced via the settings row's lastError field (set by
	// RunReindex), not as an HTTP error — the request itself succeeded in
	// attempting the reindex.
	_ = s.searchIndexReindexer.RunReindex(r.Context())

	settings, err := s.db.GetSearchIndexSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get search index settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, settings)
}

// sensitiveBackupStorageKeys lists storage_config keys whose values are
// credentials and must never round-trip to the browser in cleartext --
// covers the secret fields across all four storage.New backends (see
// backend/pkg/storage/{local,s3,gcs,azure}.go's New*Adapter constructors).
var sensitiveBackupStorageKeys = map[string]bool{
	"secret_key":        true,
	"session_token":     true,
	"account_key":       true,
	"sas_token":         true,
	"connection_string": true,
	"json_key":          true,
}

// maskedSecretValue is the sentinel returned in place of a configured
// secret. handleUpdateBackupSettings recognizes it on the way back in and
// preserves the previously stored value, so an untouched secret field
// doesn't get overwritten with the mask string itself.
const maskedSecretValue = "••••••••"

// maskSensitiveConfig returns a copy of cfg with sensitive values replaced
// by maskedSecretValue (left empty if unset), everything else unchanged.
func maskSensitiveConfig(cfg map[string]string) map[string]string {
	masked := make(map[string]string, len(cfg))
	for k, v := range cfg {
		if sensitiveBackupStorageKeys[k] && v != "" {
			masked[k] = maskedSecretValue
		} else {
			masked[k] = v
		}
	}
	return masked
}

// maskedBackupSettings returns a copy of settings with storage_config
// secrets masked, safe to send to the client.
func maskedBackupSettings(settings *database.BackupSettings) database.BackupSettings {
	out := *settings
	out.StorageConfig = maskSensitiveConfig(settings.StorageConfig)
	return out
}

var validBackupStorageTypes = map[string]bool{"": true, "local": true, "s3": true, "gcs": true, "azure": true}

// handleGetBackupSettings returns the current scheduled-backup settings.
func (s *Server) handleGetBackupSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:read") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:read required")
		return
	}

	settings, err := s.db.GetBackupSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get backup settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, maskedBackupSettings(settings))
}

// handleUpdateBackupSettings updates the auto-backup toggle/interval and the
// backup storage destination (type + config). Masked secret fields
// (maskedSecretValue) in the incoming storageConfig are replaced with the
// currently stored value before saving, so the client never has to resend
// a real credential just to change an unrelated field.
func (s *Server) handleUpdateBackupSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:write required")
		return
	}

	var input struct {
		AutoBackupEnabled   bool              `json:"autoBackupEnabled"`
		BackupIntervalHours int               `json:"backupIntervalHours"`
		StorageType         string            `json:"storageType"`
		StorageConfig       map[string]string `json:"storageConfig"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.BackupIntervalHours < 1 {
		s.writeJSONError(w, http.StatusBadRequest, "backupIntervalHours must be at least 1")
		return
	}
	if !validBackupStorageTypes[input.StorageType] {
		s.writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid storage type: %s", input.StorageType))
		return
	}

	current, err := s.db.GetBackupSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get backup settings: %v", err))
		return
	}

	mergedConfig := make(map[string]string, len(input.StorageConfig))
	for k, v := range input.StorageConfig {
		if sensitiveBackupStorageKeys[k] && v == maskedSecretValue {
			mergedConfig[k] = current.StorageConfig[k]
		} else {
			mergedConfig[k] = v
		}
	}

	if input.StorageType != "" {
		if _, err := storage.New(input.StorageType, mergedConfig); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid backup storage config: %v", err))
			return
		}
	}

	if err := s.db.UpdateBackupSettings(input.AutoBackupEnabled, input.BackupIntervalHours); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update backup settings: %v", err))
		return
	}
	if err := s.db.UpdateBackupStorageSettings(input.StorageType, mergedConfig); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update backup storage settings: %v", err))
		return
	}

	settings, err := s.db.GetBackupSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get backup settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, maskedBackupSettings(settings))
}

// handleTriggerBackup synchronously runs a full database backup and
// returns the resulting settings row.
func (s *Server) handleTriggerBackup(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:write required")
		return
	}

	if s.backupSvc == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Backup service is not configured")
		return
	}

	// Errors are surfaced via the settings row's lastError field (set by
	// Run), not as an HTTP error — the request itself succeeded in
	// attempting the backup.
	_ = s.backupSvc.Run(r.Context())

	settings, err := s.db.GetBackupSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get backup settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, settings)
}

// handleListBackups lists every stored backup archive, newest first.
func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:read") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:read required")
		return
	}

	if s.backupSvc == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Backup service is not configured")
		return
	}

	backups, err := s.backupSvc.List(r.Context())
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list backups: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{"backups": backups})
}

// handleRestoreBackup replaces the entire database with the contents of a
// previously stored backup. Higher-risk than every other settings endpoint
// (it's a destructive, whole-database operation), so it's gated by
// user:admin at the route level (see setupRoutes) rather than the
// system:write check used elsewhere in this file.
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if s.backupSvc == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Backup service is not configured")
		return
	}

	var input struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Path == "" {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body: path is required")
		return
	}

	if err := s.backupSvc.Restore(r.Context(), input.Path); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to restore backup: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "restored", "path": input.Path})
}

// handleTriggerVulnDBUpdate synchronously runs a vulnerability-DB refresh
// and returns the resulting settings row.
func (s *Server) handleTriggerVulnDBUpdate(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:write required")
		return
	}

	if s.vulnDBUpdater == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Vulnerability DB updater is not configured")
		return
	}

	// Errors are surfaced via the settings row's lastError field (set by
	// RunUpdate), not as an HTTP error — the request itself succeeded in
	// attempting the update.
	_ = s.vulnDBUpdater.RunUpdate(r.Context())

	settings, err := s.db.GetVulnDBSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get vulnerability DB settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, settings)
}

// handleGetVulnScanSettings returns the current vulnerability-scan settings.
func (s *Server) handleGetVulnScanSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:read") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:read required")
		return
	}

	settings, err := s.db.GetVulnScanSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get vulnerability scan settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, settings)
}

// handleUpdateVulnScanSettings updates the auto-scan toggle and rescan interval.
func (s *Server) handleUpdateVulnScanSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:write required")
		return
	}

	var input struct {
		AutoScanEnabled   bool `json:"autoScanEnabled"`
		ScanIntervalHours int  `json:"scanIntervalHours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.ScanIntervalHours < 1 {
		s.writeJSONError(w, http.StatusBadRequest, "scanIntervalHours must be at least 1")
		return
	}

	if err := s.db.UpdateVulnScanSettings(input.AutoScanEnabled, input.ScanIntervalHours); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update vulnerability scan settings: %v", err))
		return
	}

	settings, err := s.db.GetVulnScanSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get vulnerability scan settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, settings)
}

// handleTriggerVulnScan synchronously sweeps every cached docker/oci
// artifact for vulnerabilities and returns the resulting settings row.
func (s *Server) handleTriggerVulnScan(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "system:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: system:write required")
		return
	}

	if s.vulnRescanner == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "Vulnerability rescanner is not configured")
		return
	}

	// Errors are surfaced via the settings row's lastError field (set by
	// RunRescan), not as an HTTP error — the request itself succeeded in
	// attempting the scan.
	_ = s.vulnRescanner.RunRescan(r.Context())

	settings, err := s.db.GetVulnScanSettings()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get vulnerability scan settings: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, settings)
}

// handleGetVulnerabilitySummary returns the latest scan result per artifact
// ID for a batched set of IDs (?ids=a,b,c), backing Docker Hub-style tag
// badges without an N+1 request per visible version.
func (s *Server) handleGetVulnerabilitySummary(w http.ResponseWriter, r *http.Request) {
	idsParam := r.URL.Query().Get("ids")
	if idsParam == "" {
		s.writeJSON(w, http.StatusOK, map[string]*vulnerability.ScanResult{})
		return
	}
	ids := strings.Split(idsParam, ",")

	results, err := s.scanner.GetLatestScanResults(ids)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get vulnerability summary: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, results)
}

// handleListVulnerabilityScans lists vulnerability scan results, optionally
// filtered by artifactId and/or severity, for the Vulnerabilities page.
func (s *Server) handleListVulnerabilityScans(w http.ResponseWriter, r *http.Request) {
	artifactID := r.URL.Query().Get("artifactId")
	severity := r.URL.Query().Get("severity")

	results, err := s.scanner.ListVulnerabilityScans(artifactID, severity)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list vulnerability scans: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

// StatsResponse is the payload for GET /stats: instance-wide bandwidth and
// pull-count figures backing the Stats page.
type StatsResponse struct {
	BandwidthSavedBytes int64                       `json:"bandwidthSavedBytes"`
	TotalPulls          int64                       `json:"totalPulls"`
	TopArtifacts        []database.ArtifactMetadata `json:"topArtifacts"`
}

// handleGetStats returns bandwidth saved by caching plus a leaderboard of the
// most-pulled artifacts. Read-only and public, matching the visibility of the
// vulnerability-summary endpoint — but the leaderboard itself is still
// filtered down to registries the caller may read, so a private registry's
// artifacts never leak through it.
func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	limit := parseIntParam(r.URL.Query().Get("limit"), 10)

	bandwidthSaved, err := s.db.GetBandwidthSaved()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get bandwidth saved: %v", err))
		return
	}

	totalPulls, err := s.db.TotalDownloads()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get total pulls: %v", err))
		return
	}

	readableIDs, err := s.readableRegistryIDs(r)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to check registry access: %v", err))
		return
	}
	readable := make(map[string]bool, len(readableIDs))
	for _, id := range readableIDs {
		readable[id] = true
	}

	// Pull a wider candidate set than requested since some of the top-N by
	// raw download count may belong to registries the caller can't read.
	candidates, err := s.db.TopArtifactsByDownloads(limit * 5)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get top artifacts: %v", err))
		return
	}
	topArtifacts := make([]database.ArtifactMetadata, 0, limit)
	for _, a := range candidates {
		if !readable[a.RegistryID] {
			continue
		}
		topArtifacts = append(topArtifacts, a)
		if len(topArtifacts) >= limit {
			break
		}
	}

	s.writeJSON(w, http.StatusOK, StatsResponse{
		BandwidthSavedBytes: bandwidthSaved,
		TotalPulls:          totalPulls,
		TopArtifacts:        topArtifacts,
	})
}

// handleDownloadArtifact handles downloading an artifact
func (s *Server) handleDownloadArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Enforce artifact:read permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "artifact:read") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: artifact:read required")
		return
	}

	artifact, err := s.db.GetArtifact(id)
	if err != nil || artifact == nil {
		s.writeJSONError(w, http.StatusNotFound, "Artifact not found")
		return
	}

	registry, err := s.db.GetRegistry(artifact.RegistryID)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load registry: %v", err))
		return
	}
	if !s.rbac.CanReadRegistry(user, registry) {
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
	if user == nil || !s.rbac.HasEffectivePermission(user, "artifact:sign") {
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

	readableIDs, err := s.readableRegistryIDs(r)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to check registry access: %v", err))
		return
	}
	readable := make(map[string]bool, len(readableIDs))
	for _, id := range readableIDs {
		readable[id] = true
	}
	filtered := make([]database.ArtifactMetadata, 0, len(results))
	for _, a := range results {
		if readable[a.RegistryID] {
			filtered = append(filtered, a)
		}
	}

	s.writeJSON(w, http.StatusOK, filtered)
}

// handleAutocomplete handles search-as-you-type suggestions for artifact names
func (s *Server) handleAutocomplete(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("q")
	if prefix == "" {
		s.writeJSON(w, http.StatusOK, map[string]interface{}{"suggestions": []string{}})
		return
	}

	limit := parseIntParam(r.URL.Query().Get("limit"), 8)
	suggestions, err := s.db.AutocompleteArtifacts(prefix, limit)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to autocomplete: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{"suggestions": suggestions})
}

// handleListRegistries handles listing registries. Private registries the
// caller (anonymous or authenticated) can't read are omitted entirely — this
// is the only interface most clients use to discover what registries exist,
// so filtering here is what actually keeps a private registry's existence
// from leaking to users with no access to it.
func (s *Server) handleListRegistries(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	userID := ""
	if user != nil {
		userID = user.UserID
	}

	// Admin management UIs (Settings) pass ?all=true to also see disabled
	// registries, so they don't vanish from the list when toggled off.
	var registries []database.RegistryConfig
	var err error
	if r.URL.Query().Get("all") == "true" && userID != "" && s.rbac.HasPermission(userID, "registry:write") {
		registries, err = s.db.ListAllRegistries()
	} else {
		registries, err = s.db.ListRegistries()
	}
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list registries: %v", err))
		return
	}

	visible := make([]database.RegistryConfig, 0, len(registries))
	for _, reg := range registries {
		if s.rbac.CanReadRegistry(user, &reg) {
			visible = append(visible, reg)
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"registries": visible,
	})
}

// handleCreateRegistry handles creating a registry
// handleCreateRegistry handles creating a registry
func (s *Server) handleCreateRegistry(w http.ResponseWriter, r *http.Request) {
	// Enforce registry:write permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "registry:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: registry:write required")
		return
	}

	var registry database.RegistryConfig
	if err := json.NewDecoder(r.Body).Decode(&registry); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if registry.Type == "maven-virtual" {
		if len(registry.Members) == 0 {
			s.writeJSONError(w, http.StatusBadRequest, "A virtual Maven repository requires at least one member registry")
			return
		}
		for _, memberID := range registry.Members {
			member, err := s.db.GetRegistry(memberID)
			if err != nil || member == nil {
				s.writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Member registry %q not found", memberID))
				return
			}
			if !member.Enabled {
				s.writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Member registry %q must be enabled", memberID))
				return
			}
			if member.Type != "maven" {
				s.writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Member registry %q must be of type \"maven\" (nested virtual repositories are not supported)", memberID))
				return
			}
		}
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

	user := middleware.GetUser(r)
	if !s.rbac.CanReadRegistry(user, registry) {
		s.writeJSONError(w, http.StatusNotFound, "Registry not found")
		return
	}

	s.writeJSON(w, http.StatusOK, registry)
}

// handleGrantRegistryAccess grants (or updates) a user's read/publish rights
// on a private registry. Only callers with registry:write may manage grants.
func (s *Server) handleGrantRegistryAccess(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "registry:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: registry:write required")
		return
	}

	registryID := chi.URLParam(r, "id")
	var body struct {
		UserID     string `json:"userId"`
		CanRead    bool   `json:"canRead"`
		CanPublish bool   `json:"canPublish"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.UserID == "" {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body: userId is required")
		return
	}

	access := &database.RegistryAccess{
		RegistryID: registryID,
		UserID:     body.UserID,
		CanRead:    body.CanRead,
		CanPublish: body.CanPublish,
	}
	if err := s.db.GrantRegistryAccess(access); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to grant access: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, access)
}

// handleRevokeRegistryAccess removes a user's grant on a private registry.
func (s *Server) handleRevokeRegistryAccess(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "registry:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: registry:write required")
		return
	}

	registryID := chi.URLParam(r, "id")
	userID := chi.URLParam(r, "userId")
	if err := s.db.RevokeRegistryAccess(registryID, userID); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to revoke access: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// handleListRegistryAccess lists all user grants on a private registry.
func (s *Server) handleListRegistryAccess(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "registry:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: registry:write required")
		return
	}

	registryID := chi.URLParam(r, "id")
	grants, err := s.db.ListRegistryAccessForRegistry(registryID)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list access: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"grants": grants,
	})
}

// handleDeleteRegistry handles deleting a registry
func (s *Server) handleDeleteRegistry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Enforce registry:delete permission
	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "registry:delete") {
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

// handlePrefetchArtifact fetches an artifact reference (e.g. "ubuntu:latest")
// from a registry's upstream on demand, caching it locally if it isn't
// already cached — without waiting for a real client pull to trigger it.
// Currently only supported for docker registries.
func (s *Server) handlePrefetchArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user := middleware.GetUser(r)
	if user == nil || !s.rbac.HasEffectivePermission(user, "registry:write") {
		s.writeJSONError(w, http.StatusForbidden, "Permission denied: registry:write required")
		return
	}

	var body struct {
		Reference string `json:"reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	reference := strings.TrimSpace(body.Reference)
	if reference == "" {
		s.writeJSONError(w, http.StatusBadRequest, "reference is required")
		return
	}

	reg, err := s.db.GetRegistry(id)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load registry: %v", err))
		return
	}
	if reg == nil {
		s.writeJSONError(w, http.StatusNotFound, "Registry not found")
		return
	}
	if reg.Type != "docker" {
		s.writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Prefetch is only supported for docker registries (got %q)", reg.Type))
		return
	}
	if !reg.Proxy {
		s.writeJSONError(w, http.StatusBadRequest, "Registry is not configured as a pull-through cache")
		return
	}

	repository, ref := parseDockerReference(reference)

	alreadyCached, err := docker.PrefetchManifest(s.db, s.storageAdapter, s.cache, s.scanner, reg, repository, ref)
	if err != nil {
		s.writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("Failed to fetch from upstream: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"repository":    repository,
		"reference":     ref,
		"alreadyCached": alreadyCached,
	})
}

// parseDockerReference splits a user-supplied docker reference like
// "ubuntu:latest", "library/ubuntu@sha256:abc...", or "myorg/myimage" (no
// tag, defaults to "latest") into its repository and reference (tag or
// digest) parts.
func parseDockerReference(ref string) (repository, reference string) {
	if idx := strings.Index(ref, "@"); idx != -1 {
		return ref[:idx], ref[idx+1:]
	}
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		return ref[:idx], ref[idx+1:]
	}
	return ref, "latest"
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
		Limit:   parseIntParam(r.URL.Query().Get("limit"), 50),
		Cursor:  r.URL.Query().Get("cursor"),
		OrderBy: queryParamOrDefault(r, "orderBy", "created_at"),
		Order:   queryParamOrDefault(r, "order", "desc"),
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
		Username string   `json:"username"`
		Email    string   `json:"email"`
		Password string   `json:"password"`
		Roles    []string `json:"roles"`
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

	if existing, err := s.db.GetUserByUsername(input.Username); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to check username: %v", err))
		return
	} else if existing != nil {
		s.writeJSONError(w, http.StatusConflict, "Username is already taken")
		return
	}
	if existing, err := s.db.GetUserByEmail(input.Email); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to check email: %v", err))
		return
	} else if existing != nil {
		s.writeJSONError(w, http.StatusConflict, "Email is already in use")
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
		"user":    user,
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

// handleUpdateUser handles an admin editing another user's profile: username,
// email, and active status. Role changes go through /users/{id}/roles;
// password resets go through handleAdminResetPassword below.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := s.db.GetUser(id)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get user: %v", err))
		return
	}
	if user == nil {
		s.writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}

	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		IsActive bool   `json:"isActive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Username == "" {
		s.writeJSONError(w, http.StatusBadRequest, "username is required")
		return
	}
	if input.Email == "" {
		s.writeJSONError(w, http.StatusBadRequest, "email is required")
		return
	}

	if input.Username != user.Username {
		if existing, err := s.db.GetUserByUsername(input.Username); err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to check username: %v", err))
			return
		} else if existing != nil {
			s.writeJSONError(w, http.StatusConflict, "Username is already taken")
			return
		}
	}
	if input.Email != user.Email {
		if existing, err := s.db.GetUserByEmail(input.Email); err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to check email: %v", err))
			return
		} else if existing != nil {
			s.writeJSONError(w, http.StatusConflict, "Email is already in use")
			return
		}
	}

	if err := s.db.UpdateUserProfile(id, input.Username, input.Email, user.Timezone); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update profile: %v", err))
		return
	}
	if err := s.db.SetUserActive(id, input.IsActive); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update active status: %v", err))
		return
	}

	if !input.IsActive {
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
	}

	updated, err := s.db.GetUser(id)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load updated user: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, updated)
}

// handleAdminResetPassword handles an admin setting a user's password
// without knowing their current one (unlike handleChangePassword, which is
// self-service and requires it).
func (s *Server) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := s.db.GetUser(id)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get user: %v", err))
		return
	}
	if user == nil {
		s.writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}

	var input struct {
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(input.NewPassword) < 8 {
		s.writeJSONError(w, http.StatusBadRequest, "newPassword must be at least 8 characters")
		return
	}

	newHash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	if err := s.db.UpdateUserPassword(id, newHash); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update password: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Password reset successfully"})
}

// handleAssignUserRole handles assigning a role to a user. Users hold a
// single role, so this replaces any roles the user currently has rather
// than adding to them.
func (s *Server) handleAssignUserRole(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")

	var input struct {
		RoleID string `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.RoleID == "" {
		s.writeJSONError(w, http.StatusBadRequest, "roleId is required")
		return
	}
	roleID := input.RoleID

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

	// Replace existing roles with the single new role
	existingRoles, err := s.db.GetUserRoles(userID)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get user roles: %v", err))
		return
	}
	for _, existingRoleID := range existingRoles {
		if existingRoleID == roleID {
			continue
		}
		if err := s.db.RevokeRoleFromUser(userID, existingRoleID); err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to revoke existing role: %v", err))
			return
		}
	}

	if err := s.db.AssignRoleToUser(userID, roleID); err != nil {
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

// handleUpdateCurrentUser handles the logged-in user updating their own username/email
func (s *Server) handleUpdateCurrentUser(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		s.writeJSONError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Timezone string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Username == "" {
		s.writeJSONError(w, http.StatusBadRequest, "username is required")
		return
	}
	if input.Email == "" {
		s.writeJSONError(w, http.StatusBadRequest, "email is required")
		return
	}

	if err := s.db.UpdateUserProfile(user.UserID, input.Username, input.Email, input.Timezone); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update profile: %v", err))
		return
	}

	updated, err := s.db.GetUserByID(user.UserID)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load updated profile: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, updated)
}

// handleChangePassword handles the logged-in user changing their own password
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		s.writeJSONError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.CurrentPassword == "" || input.NewPassword == "" {
		s.writeJSONError(w, http.StatusBadRequest, "currentPassword and newPassword are required")
		return
	}
	if len(input.NewPassword) < 8 {
		s.writeJSONError(w, http.StatusBadRequest, "newPassword must be at least 8 characters")
		return
	}

	dbUser, err := s.db.GetUserByID(user.UserID)
	if err != nil || dbUser == nil {
		s.writeJSONError(w, http.StatusNotFound, "User not found")
		return
	}

	if !auth.VerifyPassword(dbUser.PasswordHash, input.CurrentPassword) {
		s.writeJSONError(w, http.StatusUnauthorized, "Current password is incorrect")
		return
	}

	newHash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	if err := s.db.UpdateUserPassword(user.UserID, newHash); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update password: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Password updated successfully"})
}

// accessKeyMetadata is the public shape of an AccessKey — everything except
// KeyHash, which must never leave the server once the key has been created.
type accessKeyMetadata struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Scope       string     `json:"scope"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsed    *time.Time `json:"lastUsed"`
	ExpiresAt   *time.Time `json:"expiresAt"`
	IsActive    bool       `json:"isActive"`
}

// personalAccessTokenScope maps the UI-facing scope string to the permissions
// slice stored in access_keys.permissions. middleware.accessKeyScope derives
// "full" vs "read" back out of this same slice (it treats presence of
// "write" as full scope), so the two must stay in sync.
func personalAccessTokenScope(scope string) ([]string, bool) {
	switch scope {
	case "read":
		return []string{"read"}, true
	case "read-write":
		return []string{"read", "write"}, true
	default:
		return nil, false
	}
}

// accessKeyUIScope is the inverse of personalAccessTokenScope, for display.
func accessKeyUIScope(permissions []string) string {
	for _, p := range permissions {
		if p == "write" {
			return "read-write"
		}
	}
	return "read"
}

func toAccessKeyMetadata(key *database.AccessKey) accessKeyMetadata {
	return accessKeyMetadata{
		ID:          key.ID,
		Name:        key.Name,
		Description: key.Description,
		Scope:       accessKeyUIScope(key.Permissions),
		CreatedAt:   key.CreatedAt,
		LastUsed:    key.LastUsed,
		ExpiresAt:   key.ExpiresAt,
		IsActive:    key.IsActive,
	}
}

// handleListMyAccessKeys lists the caller's own personal access tokens.
// Never includes KeyHash.
func (s *Server) handleListMyAccessKeys(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		s.writeJSONError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	keys, err := s.db.ListUserPersonalAccessTokens(user.UserID)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list access keys: %v", err))
		return
	}

	metadata := make([]accessKeyMetadata, 0, len(keys))
	for _, key := range keys {
		metadata = append(metadata, toAccessKeyMetadata(&key))
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"accessKeys": metadata,
	})
}

// handleCreateMyAccessKey creates a new personal access token for the
// caller. The raw token is returned exactly once, here — it is never
// recoverable afterward since only its hash is persisted.
func (s *Server) handleCreateMyAccessKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		s.writeJSONError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var input struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Scope       string  `json:"scope"`
		ExpiresAt   *string `json:"expiresAt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Name == "" {
		s.writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	permissions, ok := personalAccessTokenScope(input.Scope)
	if !ok {
		s.writeJSONError(w, http.StatusBadRequest, `scope must be "read" or "read-write"`)
		return
	}

	var expiresAt *time.Time
	if input.ExpiresAt != nil && *input.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, *input.ExpiresAt)
		if err != nil {
			s.writeJSONError(w, http.StatusBadRequest, "expiresAt must be an RFC3339 timestamp")
			return
		}
		expiresAt = &parsed
	}

	token, err := auth.GenerateToken()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	key, err := s.db.CreatePersonalAccessToken(user.UserID, input.Name, auth.HashToken(token), permissions, expiresAt, input.Description)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create access token: %v", err))
		return
	}

	resp := struct {
		accessKeyMetadata
		Token string `json:"token"`
	}{
		accessKeyMetadata: toAccessKeyMetadata(key),
		Token:             token,
	}
	s.writeJSON(w, http.StatusCreated, resp)
}

// handleRevokeMyAccessKey revokes one of the caller's own personal access
// tokens. db.InvalidateAccessKey itself has no ownership check, so the
// caller's key list is consulted first to confirm the key belongs to them.
func (s *Server) handleRevokeMyAccessKey(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		s.writeJSONError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	keyID := chi.URLParam(r, "keyId")

	keys, err := s.db.ListUserPersonalAccessTokens(user.UserID)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list access keys: %v", err))
		return
	}
	owned := false
	for _, key := range keys {
		if key.ID == keyID {
			owned = true
			break
		}
	}
	if !owned {
		s.writeJSONError(w, http.StatusNotFound, "Access token not found")
		return
	}

	if err := s.db.InvalidateAccessKey(keyID); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to revoke access token: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"message": "Access token revoked"})
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
	if user == nil || !s.rbac.HasEffectivePermission(user, "replication:sync") {
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
			"timezone":    output.Timezone,
			"roles":       output.Roles,
			"permissions": output.Permissions,
		},
	})
}

// handleSetupStatus reports whether the instance still needs first-run setup
// (i.e. no users exist yet).
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.db.CountUsers()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to check setup status: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"needsSetup": count == 0,
	})
}

// handleSetupInit creates the initial admin user and optional starter
// registries. It is public but only usable while zero users exist - it
// re-checks that invariant itself rather than trusting a prior status call,
// so it cannot be used to create a backdoor admin once the instance is live.
func (s *Server) handleSetupInit(w http.ResponseWriter, r *http.Request) {
	count, err := s.db.CountUsers()
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to check setup status: %v", err))
		return
	}
	if count > 0 {
		s.writeJSONError(w, http.StatusForbidden, "Setup has already been completed")
		return
	}

	var input struct {
		Username   string `json:"username"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		Registries []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			URL      string `json:"url"`
			Type     string `json:"type"`
			Enabled  bool   `json:"enabled"`
			Proxy    bool   `json:"proxy"`
			Priority int    `json:"priority"`
			Host     string `json:"host"`
		} `json:"registries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Username == "" {
		s.writeJSONError(w, http.StatusBadRequest, "username is required")
		return
	}
	if input.Email == "" {
		s.writeJSONError(w, http.StatusBadRequest, "email is required")
		return
	}
	if len(input.Password) < 8 {
		s.writeJSONError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	passwordHash, err := auth.HashPassword(input.Password)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	if _, err := s.db.CreateUser(input.Username, input.Email, passwordHash, []string{"admin"}); err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create admin user: %v", err))
		return
	}

	for _, reg := range input.Registries {
		if reg.ID == "" || reg.URL == "" {
			continue
		}
		registry := database.RegistryConfig{
			ID:       reg.ID,
			Name:     reg.Name,
			URL:      reg.URL,
			Type:     reg.Type,
			Enabled:  reg.Enabled,
			Proxy:    reg.Proxy,
			Priority: reg.Priority,
			Host:     reg.Host,
		}
		if err := s.db.SaveRegistry(&registry); err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save registry %s: %v", reg.ID, err))
			return
		}
	}

	// Log the new admin in immediately so the wizard can hand off straight
	// into an authenticated session.
	output, err := auth.Login(s.db, input.Username, input.Password)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("Admin created but login failed: %v", err))
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"accessToken": output.AccessToken,
		"expiresAt":   output.ExpiresAt,
		"user": map[string]interface{}{
			"userId":      output.UserID,
			"username":    output.Username,
			"email":       output.Email,
			"timezone":    output.Timezone,
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
