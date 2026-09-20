package rbac

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"golang.org/x/exp/slices"
)

// RBAC implements Role-Based Access Control for cargobay
type RBAC struct {
	db          *database.Database
	mu          sync.RWMutex
	roleCache   map[string]*RoleDefinition
	permissionCache map[string]*PermissionDefinition
}

// RoleDefinition represents a role with its permissions
type RoleDefinition struct {
	ID          string
	Name        string
	Description string
	Permissions []string
	IsSystem    bool
}

// PermissionDefinition represents a permission
type PermissionDefinition struct {
	ID          string
	Name        string
	Description string
	Resource    string
	Action      string
	IsSystem    bool
}

// New creates a new RBAC manager
func New(db *database.Database) *RBAC {
	rbac := &RBAC{
		db:              db,
		roleCache:       make(map[string]*RoleDefinition),
		permissionCache: make(map[string]*PermissionDefinition),
	}
	rbac.loadStaticPermissions()
	rbac.loadStaticRoles()
	return rbac
}

// Static permissions that cannot be modified
var staticPermissions = map[string]*PermissionDefinition{
	"artifact:read": {
		ID:          "artifact:read",
		Name:        "Read Artifacts",
		Description: "Ability to read/download artifacts",
		Resource:    "artifact",
		Action:      "read",
		IsSystem:    true,
	},
	"artifact:write": {
		ID:          "artifact:write",
		Name:        "Write Artifacts",
		Description: "Ability to upload/publish artifacts",
		Resource:    "artifact",
		Action:      "write",
		IsSystem:    true,
	},
	"artifact:delete": {
		ID:          "artifact:delete",
		Name:        "Delete Artifacts",
		Description: "Ability to delete artifacts",
		Resource:    "artifact",
		Action:      "delete",
		IsSystem:    true,
	},
	"artifact:sign": {
		ID:          "artifact:sign",
		Name:        "Sign Artifacts",
		Description: "Ability to digitally sign artifacts",
		Resource:    "artifact",
		Action:      "sign",
		IsSystem:    true,
	},
	"artifact:search": {
		ID:          "artifact:search",
		Name:        "Search Artifacts",
		Description: "Ability to search artifacts",
		Resource:    "artifact",
		Action:      "search",
		IsSystem:    true,
	},
	"artifact:replicate": {
		ID:          "artifact:replicate",
		Name:        "Replicate Artifacts",
		Description: "Ability to replicate artifacts between instances",
		Resource:    "artifact",
		Action:      "replicate",
		IsSystem:    true,
	},
	"registry:read": {
		ID:          "registry:read",
		Name:        "Read Registries",
		Description: "Ability to read registry configurations",
		Resource:    "registry",
		Action:      "read",
		IsSystem:    true,
	},
	"registry:write": {
		ID:          "registry:write",
		Name:        "Write Registries",
		Description: "Ability to configure registries",
		Resource:    "registry",
		Action:      "write",
		IsSystem:    true,
	},
	"registry:delete": {
		ID:          "registry:delete",
		Name:        "Delete Registries",
		Description: "Ability to delete registry configurations",
		Resource:    "registry",
		Action:      "delete",
		IsSystem:    true,
	},
	"user:read": {
		ID:          "user:read",
		Name:        "Read Users",
		Description: "Ability to read user information",
		Resource:    "user",
		Action:      "read",
		IsSystem:    true,
	},
	"user:write": {
		ID:          "user:write",
		Name:        "Manage Users",
		Description: "Ability to create, update, and delete users",
		Resource:    "user",
		Action:      "write",
		IsSystem:    true,
	},
	"user:admin": {
		ID:          "user:admin",
		Name:        "Admin Users",
		Description: "Ability to manage user permissions and roles",
		Resource:    "user",
		Action:      "admin",
		IsSystem:    true,
	},
	"audit:read": {
		ID:          "audit:read",
		Name:        "Read Audit Logs",
		Description: "Ability to view audit logs",
		Resource:    "audit",
		Action:      "read",
		IsSystem:    true,
	},
	"system:read": {
		ID:          "system:read",
		Name:        "Read System",
		Description: "Ability to read system configuration and stats",
		Resource:    "system",
		Action:      "read",
		IsSystem:    true,
	},
	"system:write": {
		ID:          "system:write",
		Name:        "Manage System",
		Description: "Ability to modify system configuration",
		Resource:    "system",
		Action:      "write",
		IsSystem:    true,
	},
}

// Static roles with their permissions
var staticRoles = map[string]*RoleDefinition{
	"admin": {
		ID:          "admin",
		Name:        "Administrator",
		Description: "Full administrative access to all resources",
		Permissions: []string{
			"artifact:read", "artifact:write", "artifact:delete", "artifact:sign",
			"artifact:search", "artifact:replicate",
			"registry:read", "registry:write", "registry:delete",
			"user:read", "user:write", "user:admin",
			"audit:read",
			"system:read", "system:write",
		},
		IsSystem: true,
	},
	"viewer": {
		ID:          "viewer",
		Name:        "Viewer",
		Description: "Read-only access to artifacts and registries",
		Permissions: []string{
			"artifact:read", "artifact:search",
			"registry:read",
		},
		IsSystem: true,
	},
	"publisher": {
		ID:          "publisher",
		Name:        "Publisher",
		Description: "Can upload and sign artifacts but not delete",
		Permissions: []string{
			"artifact:read", "artifact:write", "artifact:sign",
			"artifact:search",
		},
		IsSystem: true,
	},
}

// loadStaticPermissions loads the static permission definitions
func (r *RBAC) loadStaticPermissions() {
	for id, perm := range staticPermissions {
		r.permissionCache[id] = perm
	}
}

// loadStaticRoles loads the static role definitions
func (r *RBAC) loadStaticRoles() {
	for id, role := range staticRoles {
		r.roleCache[id] = role
	}
}

// GetPermission retrieves a permission by ID
func (r *RBAC) GetPermission(id string) *PermissionDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.permissionCache[id]
}

// ListPermissions lists all permissions
func (r *RBAC) ListPermissions() []*PermissionDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	perms := make([]*PermissionDefinition, 0, len(r.permissionCache))
	for _, p := range r.permissionCache {
		perms = append(perms, p)
	}
	return perms
}

// GetRole retrieves a role by ID
func (r *RBAC) GetRole(id string) *RoleDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.roleCache[id]
}

// ListRoles lists all roles
func (r *RBAC) ListRoles() []*RoleDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	roles := make([]*RoleDefinition, 0, len(r.roleCache))
	for _, role := range r.roleCache {
		roles = append(roles, role)
	}
	return roles
}

// HasPermission checks if a user has a specific permission
func (r *RBAC) HasPermission(userID, permissionID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Get user's roles from database
	roles, err := r.db.GetUserRoles(userID)
	if err != nil {
		return false
	}

	// Check if any role has the permission
	for _, roleID := range roles {
		if r.hasPermissionInRole(roleID, permissionID) {
			return true
		}
	}
	return false
}

// HasEffectivePermission is HasPermission plus the request credential's
// scope: a "read"-scoped personal access token can never satisfy a
// write/delete/sign/replicate/admin permission, regardless of what the
// underlying user's roles would otherwise allow. Use this (not
// HasPermission) for any authorization decision keyed off an
// *middleware.User taken from a request — HasPermission itself stays
// scope-unaware for non-request callers (the CLI, role management, etc.)
// that have no credential scope to consult.
func (r *RBAC) HasEffectivePermission(user *middleware.User, permissionID string) bool {
	if user == nil {
		return false
	}
	if user.Scope == "read" && !isReadPermission(permissionID) {
		return false
	}
	return r.HasPermission(user.UserID, permissionID)
}

// isReadPermission reports whether a static permission ID represents a
// read-only action ("read" or "search"). Permissions not found in the
// static table (e.g. permissions on custom roles) are treated as
// non-read, the conservative default for scope enforcement.
func isReadPermission(permissionID string) bool {
	perm, ok := staticPermissions[permissionID]
	if !ok {
		return false
	}
	return perm.Action == "read" || perm.Action == "search"
}

// CanReadRegistry reports whether user (nil = anonymous) may read/pull
// from reg. Public registries are open to everyone, including anonymous
// callers; private registries require an explicit grant (or the
// registry:write admin bypass).
func (r *RBAC) CanReadRegistry(user *middleware.User, reg *database.RegistryConfig) bool {
	if reg == nil || !reg.Private {
		return true
	}
	if user == nil {
		return false
	}
	if r.HasEffectivePermission(user, "registry:write") {
		return true
	}
	access, err := r.db.GetRegistryAccess(reg.ID, user.UserID)
	if err == nil && access != nil && access.CanRead {
		return true
	}
	groups, err := r.db.ListGroupsForUser(user.UserID)
	if err != nil {
		return false
	}
	for _, g := range groups {
		if ga, err := r.db.GetGroupRegistryAccess(reg.ID, g.ID); err == nil && ga != nil && ga.CanRead {
			return true
		}
	}
	return false
}

// CanPublishRegistry reports whether user may push to reg. Publishing to a
// public/upstream registry is never allowed — only private registries
// accept pushes, and only from explicitly granted users (or admins). A
// read-scoped personal access token can never publish, regardless of the
// underlying user's roles.
func (r *RBAC) CanPublishRegistry(user *middleware.User, reg *database.RegistryConfig) bool {
	if reg == nil || user == nil || !reg.Private || user.Scope == "read" {
		return false
	}
	if r.HasPermission(user.UserID, "registry:write") {
		return true
	}
	access, err := r.db.GetRegistryAccess(reg.ID, user.UserID)
	if err == nil && access != nil && access.CanPublish {
		return true
	}
	groups, err := r.db.ListGroupsForUser(user.UserID)
	if err != nil {
		return false
	}
	for _, g := range groups {
		if ga, err := r.db.GetGroupRegistryAccess(reg.ID, g.ID); err == nil && ga != nil && ga.CanPublish {
			return true
		}
	}
	return false
}

// hasPermissionInRole checks if a role has a specific permission
func (r *RBAC) hasPermissionInRole(roleID, permissionID string) bool {
	role, exists := r.roleCache[roleID]
	if !exists {
		return false
	}
	return slices.Contains(role.Permissions, permissionID)
}

// HasPermissionInResource checks if a user has permission on a specific resource
func (r *RBAC) HasPermissionInResource(userID, permissionID, resourceType, resourceID string) bool {
	// Check if user has the base permission
	if !r.HasPermission(userID, permissionID) {
		return false
	}

	return true
}

// RequirePermission middleware requires a specific permission
func (r *RBAC) RequirePermission(permissionID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			user := middleware.GetUser(req)
			if user == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			if !r.HasEffectivePermission(user, permissionID) {
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

// CanReadArtifact checks if a user can read a specific artifact
func (r *RBAC) CanReadArtifact(userID, registryID, namespace, artifactName string) bool {
	return r.HasPermissionInResource(userID, "artifact:read", "artifact", registryID)
}

// CanWriteArtifact checks if a user can write a specific artifact
func (r *RBAC) CanWriteArtifact(userID, registryID, namespace, artifactName string) bool {
	return r.HasPermissionInResource(userID, "artifact:write", "artifact", registryID)
}

// CanDeleteArtifact checks if a user can delete a specific artifact
func (r *RBAC) CanDeleteArtifact(userID, registryID, namespace, artifactName string) bool {
	return r.HasPermissionInResource(userID, "artifact:delete", "artifact", registryID)
}

// CanSignArtifact checks if a user can sign a specific artifact
func (r *RBAC) CanSignArtifact(userID, registryID, namespace, artifactName string) bool {
	return r.HasPermissionInResource(userID, "artifact:sign", "artifact", registryID)
}

// CanReplicateArtifact checks if a user can replicate artifacts
func (r *RBAC) CanReplicateArtifact(userID string) bool {
	return r.HasPermission(userID, "artifact:replicate")
}

// CanManageUser checks if a user can manage other users
func (r *RBAC) CanManageUser(userID, targetUserID string) bool {
	// Admin can manage all users
	if r.HasPermission(userID, "user:admin") {
		return true
	}

	// User can manage themselves
	if userID == targetUserID {
		return true
	}

	return false
}

// CreateRole creates a new role
func (r *RBAC) CreateRole(name, description string, permissions []string) (*RoleDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	roleID := fmt.Sprintf("role_%s", strings.ReplaceAll(name, " ", "_"))

	role := &RoleDefinition{
		ID:          roleID,
		Name:        name,
		Description: description,
		Permissions: permissions,
		IsSystem:    false,
	}

	r.roleCache[roleID] = role

	// Save to database
	err := r.db.SaveRole(role.ID, role.Name, role.Description, role.IsSystem, role.Permissions)
	if err != nil {
		delete(r.roleCache, roleID)
		return nil, err
	}

	return role, nil
}

// UpdateRole updates an existing role
func (r *RBAC) UpdateRole(roleID string, name, description string, permissions []string) (*RoleDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	role, exists := r.roleCache[roleID]
	if !exists {
		return nil, fmt.Errorf("role not found")
	}

	if role.IsSystem {
		return nil, fmt.Errorf("cannot modify system role")
	}

	role.Name = name
	role.Description = description
	role.Permissions = permissions

	// Save to database
	err := r.db.SaveRole(role.ID, role.Name, role.Description, role.IsSystem, role.Permissions)
	if err != nil {
		return nil, err
	}

	return role, nil
}

// DeleteRole deletes a role
func (r *RBAC) DeleteRole(roleID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	role, exists := r.roleCache[roleID]
	if !exists {
		return fmt.Errorf("role not found")
	}

	if role.IsSystem {
		return fmt.Errorf("cannot delete system role")
	}

	delete(r.roleCache, roleID)
	return r.db.DeleteRole(roleID)
}

// AssignRoleToUser assigns a role to a user. roleID may be a static system
// role (admin/developer/viewer/publisher/auditor, held only in-memory) or a
// custom role persisted to the roles table.
func (r *RBAC) AssignRoleToUser(userID, roleID string) error {
	if r.GetRole(roleID) == nil {
		return fmt.Errorf("role not found")
	}

	return r.db.AssignRoleToUser(userID, roleID)
}

// RevokeRoleFromUser revokes a role from a user
func (r *RBAC) RevokeRoleFromUser(userID, roleID string) error {
	return r.db.RevokeRoleFromUser(userID, roleID)
}

// GetUserRoles returns all roles for a user
func (r *RBAC) GetUserRoles(userID string) ([]string, error) {
	return r.db.GetUserRoles(userID)
}

// HasAnyPermission checks if user has any of the given permissions
func (r *RBAC) HasAnyPermission(userID string, permissions ...string) bool {
	for _, perm := range permissions {
		if r.HasPermission(userID, perm) {
			return true
		}
	}
	return false
}

// HasAllPermissions checks if user has all of the given permissions
func (r *RBAC) HasAllPermissions(userID string, permissions ...string) bool {
	for _, perm := range permissions {
		if !r.HasPermission(userID, perm) {
			return false
		}
	}
	return true
}

// GetRolePermissions returns all permissions for a role
func (r *RBAC) GetRolePermissions(roleID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	role, exists := r.roleCache[roleID]
	if !exists {
		return nil
	}

	return role.Permissions
}

// GetUserRolePermissions returns all permissions for a user via their roles
func (r *RBAC) GetUserRolePermissions(userID string) []string {
	// Get user's roles from database
	roles, err := r.db.GetUserRoles(userID)
	if err != nil {
		return nil
	}

	// Collect all permissions from user's roles
	permissions := make([]string, 0)
	seen := make(map[string]bool)

	for _, roleID := range roles {
		role, exists := r.roleCache[roleID]
		if !exists {
			continue
		}

		for _, perm := range role.Permissions {
			if !seen[perm] {
				seen[perm] = true
				permissions = append(permissions, perm)
			}
		}
	}

	return permissions
}

// AddPermissionToRole adds a permission to a role
func (r *RBAC) AddPermissionToRole(roleID, permission string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	role, exists := r.roleCache[roleID]
	if !exists {
		return fmt.Errorf("role not found")
	}

	if role.IsSystem {
		return fmt.Errorf("cannot modify system role")
	}

	// Check if permission already exists
	for _, p := range role.Permissions {
		if p == permission {
			return nil // Already has this permission
		}
	}

	role.Permissions = append(role.Permissions, permission)

	// Save to database
	err := r.db.SaveRole(role.ID, role.Name, role.Description, role.IsSystem, role.Permissions)
	if err != nil {
		// Remove from cache if DB update fails
		return err
	}

	return nil
}

// RemovePermissionFromRole removes a permission from a role
func (r *RBAC) RemovePermissionFromRole(roleID, permission string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	role, exists := r.roleCache[roleID]
	if !exists {
		return fmt.Errorf("role not found")
	}

	if role.IsSystem {
		return fmt.Errorf("cannot modify system role")
	}

	// Find and remove the permission
	found := false
	newPermissions := make([]string, 0)
	for _, p := range role.Permissions {
		if p == permission {
			found = true
		} else {
			newPermissions = append(newPermissions, p)
		}
	}

	if !found {
		return nil // Permission not in role
	}

	role.Permissions = newPermissions

	// Save to database
	err := r.db.SaveRole(role.ID, role.Name, role.Description, role.IsSystem, role.Permissions)
	if err != nil {
		return err
	}

	return nil
}
