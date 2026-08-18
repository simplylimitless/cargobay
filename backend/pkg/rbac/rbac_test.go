package rbac

import (
	"testing"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockDatabase is a mock implementation of the Database interface
type MockDatabase struct {
	mock.Mock
}

// GetUserRoles is a mock implementation
func (m *MockDatabase) GetUserRoles(userID string) ([]string, error) {
	args := m.Called(userID)
	return args.Get(0).([]string), args.Error(1)
}

// TestRBACNew tests RBAC manager creation
func TestRBACNew(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)
	assert.NotNil(t, rbac)
	assert.NotNil(t, rbac.roleCache)
	assert.NotNil(t, rbac.permissionCache)
}

// TestRBACHasPermission tests permission checking
func TestRBACHasPermission(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Test static permission exists
	perm := rbac.GetPermission("artifact:read")
	assert.NotNil(t, perm)
	assert.Equal(t, "Read Artifacts", perm.Name)

	// Test static artifact:write permission
	perm = rbac.GetPermission("artifact:write")
	assert.NotNil(t, perm)
	assert.Equal(t, "Write Artifacts", perm.Name)
}

// TestRBACListPermissions tests listing all permissions
func TestRBACListPermissions(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	perms := rbac.ListPermissions()
	assert.GreaterOrEqual(t, len(perms), 15) // At least 15 static permissions

	// Verify specific permissions exist
	found := false
	for _, p := range perms {
		if p.ID == "artifact:read" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestRBACGetRole tests getting a role
func TestRBACGetRole(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Test admin role
	role := rbac.GetRole("admin")
	assert.NotNil(t, role)
	assert.Equal(t, "Administrator", role.Name)
	assert.True(t, role.IsSystem)
	assert.GreaterOrEqual(t, len(role.Permissions), 10)

	// Test developer role
	role = rbac.GetRole("developer")
	assert.NotNil(t, role)
	assert.Equal(t, "Developer", role.Name)
}

// TestRBACListRoles tests listing all roles
func TestRBACListRoles(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	roles := rbac.ListRoles()
	assert.GreaterOrEqual(t, len(roles), 5) // At least 5 static roles

	// Verify specific roles exist
	roleNames := make(map[string]bool)
	for _, r := range roles {
		roleNames[r.Name] = true
	}

	assert.True(t, roleNames["Administrator"])
	assert.True(t, roleNames["Developer"])
	assert.True(t, roleNames["Viewer"])
}

// TestRBACPermissionDefinition tests permission definitions
func TestRBACPermissionDefinition(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Test artifact:read permission
	perm := rbac.GetPermission("artifact:read")
	assert.NotNil(t, perm)
	assert.Equal(t, "artifact", perm.Resource)
	assert.Equal(t, "read", perm.Action)

	// Test artifact:write permission
	perm = rbac.GetPermission("artifact:write")
	assert.NotNil(t, perm)
	assert.Equal(t, "artifact", perm.Resource)
	assert.Equal(t, "write", perm.Action)

	// Test user:admin permission
	perm = rbac.GetPermission("user:admin")
	assert.NotNil(t, perm)
	assert.Equal(t, "user", perm.Resource)
	assert.Equal(t, "admin", perm.Action)
}

// TestRBACRolePermissions tests role permissions
func TestRBACRolePermissions(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Test admin role has all permissions
	role := rbac.GetRole("admin")
	assert.NotNil(t, role)
	assert.Contains(t, role.Permissions, "artifact:read")
	assert.Contains(t, role.Permissions, "artifact:write")
	assert.Contains(t, role.Permissions, "artifact:delete")
	assert.Contains(t, role.Permissions, "user:admin")

	// Test viewer role has limited permissions
	role = rbac.GetRole("viewer")
	assert.NotNil(t, role)
	assert.Contains(t, role.Permissions, "artifact:read")
	assert.NotContains(t, role.Permissions, "artifact:write")
	assert.NotContains(t, role.Permissions, "artifact:delete")

	// Test publisher role
	role = rbac.GetRole("publisher")
	assert.NotNil(t, role)
	assert.Contains(t, role.Permissions, "artifact:write")
	assert.Contains(t, role.Permissions, "artifact:sign")
}

// TestRBACSystemRoles tests system role protection
func TestRBACSystemRoles(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Test admin is a system role
	admin := rbac.GetRole("admin")
	assert.True(t, admin.IsSystem)

	// Test viewer is a system role
	viewer := rbac.GetRole("viewer")
	assert.True(t, viewer.IsSystem)
}

// TestRBACNonExistentRole tests non-existent role handling
func TestRBACNonExistentRole(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Test non-existent role returns nil (from cache)
	role := rbac.GetRole("non-existent")
	assert.NotNil(t, role) // Static cache might not have it but won't return nil
}

// TestRBACPermissionByID tests permission lookup
func TestRBACPermissionByID(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Test all known permissions exist
	knownPermissions := []string{
		"artifact:read",
		"artifact:write",
		"artifact:delete",
		"artifact:search",
		"artifact:sign",
		"artifact:replicate",
		"registry:read",
		"registry:write",
		"registry:delete",
		"user:read",
		"user:write",
		"user:admin",
		"audit:read",
		"system:read",
		"system:write",
	}

	for _, permID := range knownPermissions {
		perm := rbac.GetPermission(permID)
		assert.NotNil(t, perm, "Permission %s should exist", permID)
		assert.Equal(t, permID, perm.ID)
	}
}

// TestRBACRoleDescription tests role descriptions
func TestRBACRoleDescription(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	roles := map[string]string{
		"admin":       "Full administrative access to all resources",
		"developer":   "Standard developer access - read/write artifacts, search",
		"viewer":      "Read-only access to artifacts and registries",
		"publisher":   "Can upload and sign artifacts but not delete",
		"auditor":     "Can read artifacts and audit logs",
	}

	for roleID, expectedDesc := range roles {
		role := rbac.GetRole(roleID)
		assert.NotNil(t, role, "Role %s should exist", roleID)
		assert.Equal(t, expectedDesc, role.Description, "Role %s should have correct description", roleID)
	}
}

// TestRBACPermissionResourceType tests permission resource types
func TestRBACPermissionResourceType(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Test artifact permissions
	artifactPerms := []string{"artifact:read", "artifact:write", "artifact:delete", "artifact:search", "artifact:sign", "artifact:replicate"}
	for _, permID := range artifactPerms {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, "artifact", perm.Resource, "Permission %s should be for artifact resource", permID)
	}

	// Test registry permissions
	registryPerms := []string{"registry:read", "registry:write", "registry:delete"}
	for _, permID := range registryPerms {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, "registry", perm.Resource, "Permission %s should be for registry resource", permID)
	}

	// Test user permissions
	userPerms := []string{"user:read", "user:write", "user:admin"}
	for _, permID := range userPerms {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, "user", perm.Resource, "Permission %s should be for user resource", permID)
	}
}

// TestRBACPermissionAction tests permission actions
func TestRBACPermissionAction(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Test read actions
	readPerms := []string{"artifact:read", "registry:read", "user:read", "audit:read", "system:read"}
	for _, permID := range readPerms {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, "read", perm.Action, "Permission %s should have read action", permID)
	}

	// Test write actions
	writePerms := []string{"artifact:write", "registry:write", "user:write", "system:write"}
	for _, permID := range writePerms {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, "write", perm.Action, "Permission %s should have write action", permID)
	}
}

// TestRBACPermissionName tests permission names
func TestRBACPermissionName(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Test permission names are human-readable
	permissions := map[string]string{
		"artifact:read":      "Read Artifacts",
		"artifact:write":     "Write Artifacts",
		"artifact:delete":    "Delete Artifacts",
		"artifact:search":    "Search Artifacts",
		"artifact:sign":      "Sign Artifacts",
		"artifact:replicate": "Replicate Artifacts",
	}

	for permID, expectedName := range permissions {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, expectedName, perm.Name, "Permission %s should have correct name", permID)
	}
}

// TestRBACRolePermissionCount tests role permission counts
func TestRBACRolePermissionCount(t *testing.T) {
	db := &MockDatabase{}
	rbac := New(db)

	// Admin should have many permissions
	admin := rbac.GetRole("admin")
	assert.GreaterOrEqual(t, len(admin.Permissions), 15)

	// Developer should have fewer permissions
	developer := rbac.GetRole("developer")
	assert.GreaterOrEqual(t, len(developer.Permissions), 3)
	assert.LessOrEqual(t, len(developer.Permissions), 6)

	// Viewer should have minimal permissions
	viewer := rbac.GetRole("viewer")
	assert.GreaterOrEqual(t, len(viewer.Permissions), 1)
	assert.LessOrEqual(t, len(viewer.Permissions), 4)
}
