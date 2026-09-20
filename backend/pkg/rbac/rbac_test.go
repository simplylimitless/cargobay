package rbac

import (
	"testing"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"github.com/stretchr/testify/assert"
)

// newTestRBAC builds an RBAC manager against an unconnected database.Database.
// RBAC's role/permission lookups are served entirely from the static
// in-memory maps (see staticRoles/staticPermissions) and never touch the
// DB, so a real-but-unconnected instance is safe here — see
// pkg/integration/integration_test.go for the same pattern.
func newTestRBAC() *RBAC {
	return New(database.New("postgres://localhost:5432/test"))
}

// TestRBACNew tests RBAC manager creation
func TestRBACNew(t *testing.T) {
	rbac := newTestRBAC()
	assert.NotNil(t, rbac)
	assert.NotNil(t, rbac.roleCache)
	assert.NotNil(t, rbac.permissionCache)
}

// TestRBACHasPermission tests permission checking
func TestRBACHasPermission(t *testing.T) {
	rbac := newTestRBAC()

	perm := rbac.GetPermission("artifact:read")
	assert.NotNil(t, perm)
	assert.Equal(t, "Read Artifacts", perm.Name)

	perm = rbac.GetPermission("artifact:write")
	assert.NotNil(t, perm)
	assert.Equal(t, "Write Artifacts", perm.Name)
}

// TestRBACListPermissions tests listing all permissions
func TestRBACListPermissions(t *testing.T) {
	rbac := newTestRBAC()

	perms := rbac.ListPermissions()
	assert.GreaterOrEqual(t, len(perms), 15) // At least 15 static permissions

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
	rbac := newTestRBAC()

	role := rbac.GetRole("admin")
	assert.NotNil(t, role)
	assert.Equal(t, "Administrator", role.Name)
	assert.True(t, role.IsSystem)
	assert.GreaterOrEqual(t, len(role.Permissions), 10)

	role = rbac.GetRole("publisher")
	assert.NotNil(t, role)
	assert.Equal(t, "Publisher", role.Name)
}

// TestRBACListRoles tests listing all roles
func TestRBACListRoles(t *testing.T) {
	rbac := newTestRBAC()

	roles := rbac.ListRoles()
	assert.GreaterOrEqual(t, len(roles), 3) // admin, viewer, publisher

	roleNames := make(map[string]bool)
	for _, r := range roles {
		roleNames[r.Name] = true
	}

	assert.True(t, roleNames["Administrator"])
	assert.True(t, roleNames["Viewer"])
	assert.True(t, roleNames["Publisher"])
}

// TestRBACPermissionDefinition tests permission definitions
func TestRBACPermissionDefinition(t *testing.T) {
	rbac := newTestRBAC()

	perm := rbac.GetPermission("artifact:read")
	assert.NotNil(t, perm)
	assert.Equal(t, "artifact", perm.Resource)
	assert.Equal(t, "read", perm.Action)

	perm = rbac.GetPermission("artifact:write")
	assert.NotNil(t, perm)
	assert.Equal(t, "artifact", perm.Resource)
	assert.Equal(t, "write", perm.Action)

	perm = rbac.GetPermission("user:admin")
	assert.NotNil(t, perm)
	assert.Equal(t, "user", perm.Resource)
	assert.Equal(t, "admin", perm.Action)
}

// TestRBACRolePermissions tests role permissions
func TestRBACRolePermissions(t *testing.T) {
	rbac := newTestRBAC()

	role := rbac.GetRole("admin")
	assert.NotNil(t, role)
	assert.Contains(t, role.Permissions, "artifact:read")
	assert.Contains(t, role.Permissions, "artifact:write")
	assert.Contains(t, role.Permissions, "artifact:delete")
	assert.Contains(t, role.Permissions, "user:admin")

	role = rbac.GetRole("viewer")
	assert.NotNil(t, role)
	assert.Contains(t, role.Permissions, "artifact:read")
	assert.NotContains(t, role.Permissions, "artifact:write")
	assert.NotContains(t, role.Permissions, "artifact:delete")

	role = rbac.GetRole("publisher")
	assert.NotNil(t, role)
	assert.Contains(t, role.Permissions, "artifact:write")
	assert.Contains(t, role.Permissions, "artifact:sign")
}

// TestRBACSystemRoles tests system role protection
func TestRBACSystemRoles(t *testing.T) {
	rbac := newTestRBAC()

	admin := rbac.GetRole("admin")
	assert.True(t, admin.IsSystem)

	viewer := rbac.GetRole("viewer")
	assert.True(t, viewer.IsSystem)
}

// TestRBACNonExistentRole tests non-existent role handling
func TestRBACNonExistentRole(t *testing.T) {
	rbac := newTestRBAC()

	role := rbac.GetRole("non-existent")
	assert.Nil(t, role)
}

// TestRBACPermissionByID tests permission lookup
func TestRBACPermissionByID(t *testing.T) {
	rbac := newTestRBAC()

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
	rbac := newTestRBAC()

	roles := map[string]string{
		"admin":     "Full administrative access to all resources",
		"viewer":    "Read-only access to artifacts and registries",
		"publisher": "Can upload and sign artifacts but not delete",
	}

	for roleID, expectedDesc := range roles {
		role := rbac.GetRole(roleID)
		assert.NotNil(t, role, "Role %s should exist", roleID)
		assert.Equal(t, expectedDesc, role.Description, "Role %s should have correct description", roleID)
	}
}

// TestRBACPermissionResourceType tests permission resource types
func TestRBACPermissionResourceType(t *testing.T) {
	rbac := newTestRBAC()

	artifactPerms := []string{"artifact:read", "artifact:write", "artifact:delete", "artifact:search", "artifact:sign", "artifact:replicate"}
	for _, permID := range artifactPerms {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, "artifact", perm.Resource, "Permission %s should be for artifact resource", permID)
	}

	registryPerms := []string{"registry:read", "registry:write", "registry:delete"}
	for _, permID := range registryPerms {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, "registry", perm.Resource, "Permission %s should be for registry resource", permID)
	}

	userPerms := []string{"user:read", "user:write", "user:admin"}
	for _, permID := range userPerms {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, "user", perm.Resource, "Permission %s should be for user resource", permID)
	}
}

// TestRBACPermissionAction tests permission actions
func TestRBACPermissionAction(t *testing.T) {
	rbac := newTestRBAC()

	readPerms := []string{"artifact:read", "registry:read", "user:read", "audit:read", "system:read"}
	for _, permID := range readPerms {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, "read", perm.Action, "Permission %s should have read action", permID)
	}

	writePerms := []string{"artifact:write", "registry:write", "user:write", "system:write"}
	for _, permID := range writePerms {
		perm := rbac.GetPermission(permID)
		assert.Equal(t, "write", perm.Action, "Permission %s should have write action", permID)
	}
}

// TestRBACPermissionName tests permission names
func TestRBACPermissionName(t *testing.T) {
	rbac := newTestRBAC()

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
	rbac := newTestRBAC()

	admin := rbac.GetRole("admin")
	assert.GreaterOrEqual(t, len(admin.Permissions), 15)

	publisher := rbac.GetRole("publisher")
	assert.GreaterOrEqual(t, len(publisher.Permissions), 3)
	assert.LessOrEqual(t, len(publisher.Permissions), 6)

	viewer := rbac.GetRole("viewer")
	assert.GreaterOrEqual(t, len(viewer.Permissions), 1)
	assert.LessOrEqual(t, len(viewer.Permissions), 4)
}

// TestHasEffectivePermissionNilUser tests that a nil user is always denied.
func TestHasEffectivePermissionNilUser(t *testing.T) {
	rbac := newTestRBAC()
	assert.False(t, rbac.HasEffectivePermission(nil, "artifact:read"))
}

// TestHasEffectivePermissionReadScopeDeniesWrite tests that a read-scoped
// credential (a read-only PAT) can never satisfy a write/delete/admin
// permission, regardless of the underlying user's roles — this check
// short-circuits before ever touching the database, so it's safe to run
// against an unconnected RBAC.
func TestHasEffectivePermissionReadScopeDeniesWrite(t *testing.T) {
	rbac := newTestRBAC()
	user := &middleware.User{UserID: "user-1", Scope: "read"}

	assert.False(t, rbac.HasEffectivePermission(user, "artifact:write"))
	assert.False(t, rbac.HasEffectivePermission(user, "artifact:delete"))
	assert.False(t, rbac.HasEffectivePermission(user, "user:admin"))
}

// TestHasEffectivePermissionReadScopeUnknownPermissionDenied tests that a
// permission ID not present in the static table is treated as non-read (the
// conservative default) and denied for a read-scoped credential.
func TestHasEffectivePermissionReadScopeUnknownPermissionDenied(t *testing.T) {
	rbac := newTestRBAC()
	user := &middleware.User{UserID: "user-1", Scope: "read"}
	assert.False(t, rbac.HasEffectivePermission(user, "custom:action"))
}

// TestCanReadRegistryPublicAlwaysAllowed tests that a public (non-private)
// registry is always readable, including by anonymous (nil) callers.
func TestCanReadRegistryPublicAlwaysAllowed(t *testing.T) {
	rbac := newTestRBAC()
	reg := &database.RegistryConfig{ID: "reg-1", Private: false}

	assert.True(t, rbac.CanReadRegistry(nil, reg))
	assert.True(t, rbac.CanReadRegistry(&middleware.User{UserID: "user-1"}, reg))
}

// TestCanReadRegistryPrivateAnonymousDenied tests that an anonymous caller
// is denied read access to a private registry.
func TestCanReadRegistryPrivateAnonymousDenied(t *testing.T) {
	rbac := newTestRBAC()
	reg := &database.RegistryConfig{ID: "reg-1", Private: true}
	assert.False(t, rbac.CanReadRegistry(nil, reg))
}

// TestCanPublishRegistryReadScopeAlwaysDenied tests that a read-scoped
// credential can never publish, even to a private registry with a nominal
// admin role attached — this is the scoping guarantee a read-only PAT
// depends on.
func TestCanPublishRegistryReadScopeAlwaysDenied(t *testing.T) {
	rbac := newTestRBAC()
	reg := &database.RegistryConfig{ID: "reg-1", Private: true}
	user := &middleware.User{UserID: "user-1", Scope: "read"}

	assert.False(t, rbac.CanPublishRegistry(user, reg))
}

// TestCanPublishRegistryNonPrivateAlwaysDenied tests that publishing to a
// public/upstream registry is never allowed, regardless of scope.
func TestCanPublishRegistryNonPrivateAlwaysDenied(t *testing.T) {
	rbac := newTestRBAC()
	reg := &database.RegistryConfig{ID: "reg-1", Private: false}
	user := &middleware.User{UserID: "user-1", Scope: "full"}

	assert.False(t, rbac.CanPublishRegistry(user, reg))
}

// TestCanPublishRegistryNilCasesDenied tests the nil-registry and nil-user
// guard clauses.
func TestCanPublishRegistryNilCasesDenied(t *testing.T) {
	rbac := newTestRBAC()
	reg := &database.RegistryConfig{ID: "reg-1", Private: true}
	user := &middleware.User{UserID: "user-1", Scope: "full"}

	assert.False(t, rbac.CanPublishRegistry(nil, reg))
	assert.False(t, rbac.CanPublishRegistry(user, nil))
}

// connectTestDB returns a live, connected database.Database, skipping the
// test if Postgres isn't reachable in this environment. A real pool is
// required here (unlike newTestRBAC's unconnected instance) because
// GetUserRoles/GetRegistryAccess/ListGroupsForUser all execute a live query
// against db.pool, which panics if the pool is nil rather than erroring.
func connectTestDB(t *testing.T) *database.Database {
	t.Helper()
	db := database.New("postgres://cargobay:password@localhost:5432/cargobay")
	if err := db.Connect(); err != nil {
		t.Skipf("skipping: postgres not reachable: %v", err)
	}
	t.Cleanup(func() { db.Disconnect() })
	return db
}

// TestCanReadRegistryGroupLookupErrorDenied tests that CanReadRegistry fails
// closed when neither a per-user grant nor a group grant exists: a
// non-privileged, unrecognized user on a private registry must be denied,
// not silently allowed through, exercising the fallback path added for
// group-based access alongside the existing per-user grant check.
func TestCanReadRegistryGroupLookupErrorDenied(t *testing.T) {
	db := connectTestDB(t)
	rbac := New(db)
	reg := &database.RegistryConfig{ID: "reg-nogrant-1", Private: true}
	user := &middleware.User{UserID: "user-nogrant-1"}

	assert.False(t, rbac.CanReadRegistry(user, reg))
}

// TestCanPublishRegistryGroupLookupErrorDenied is the publish-side analogue
// of TestCanReadRegistryGroupLookupErrorDenied: no registry:write, no
// per-user publish grant, and no group grant must all fail closed.
func TestCanPublishRegistryGroupLookupErrorDenied(t *testing.T) {
	db := connectTestDB(t)
	rbac := New(db)
	reg := &database.RegistryConfig{ID: "reg-nogrant-2", Private: true}
	user := &middleware.User{UserID: "user-nogrant-2", Scope: "full"}

	assert.False(t, rbac.CanPublishRegistry(user, reg))
}
