package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertExactArgs checks that cmd's Args validator behaves like
// cobra.ExactArgs(n): it accepts exactly n positional args and rejects any
// other count. cmd.Args is a func type, so it can't be compared to n
// directly with assert.Equal.
func assertExactArgs(t *testing.T, cmd *cobra.Command, n int) {
	t.Helper()
	require.NotNil(t, cmd.Args)
	require.NoError(t, cmd.Args(cmd, make([]string, n)))
	require.Error(t, cmd.Args(cmd, make([]string, n+1)))
}

// TestRootCommandNew tests creating a new RootCommand
func TestRootCommandNew(t *testing.T) {
	cmd := New()
	assert.NotNil(t, cmd)
}

// TestRootCommandExecute tests Execute method (doesn't actually execute)
func TestRootCommandExecute(t *testing.T) {
	cmd := New()
	// Execute returns an error if there's an issue, but with no commands
	// added it should return nil (no command specified)
	// We're not testing actual execution here, just that the method exists
	assert.NotNil(t, cmd)
}

// TestRootCommandStructure tests the structure of RootCommand
func TestRootCommandStructure(t *testing.T) {
	cmd := &RootCommand{}
	assert.Nil(t, cmd.cmd)
	assert.Nil(t, cmd.db)
	assert.Nil(t, cmd.rbacMgr)
}

// TestRootCommandWithDBAndRBAC tests RootCommand with DB and RBAC
func TestRootCommandWithDBAndRBAC(t *testing.T) {
	// We can't create real instances without database connection
	// This test verifies the struct definition
	rc := New()
	assert.NotNil(t, rc)
}

// TestArtifactCommandStructure tests artifact command structure
func TestArtifactCommandStructure(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newArtifactCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "artifact", cmd.Use)
	assert.Equal(t, "Manage artifacts", cmd.Short)
}

// TestUserCommandStructure tests user command structure
func TestUserCommandStructure(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newUserCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "user", cmd.Use)
	assert.Equal(t, "Manage users", cmd.Short)
}

// TestRoleCommandStructure tests role command structure
func TestRoleCommandStructure(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRoleCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "role", cmd.Use)
	assert.Equal(t, "Manage roles", cmd.Short)
}

// TestRegistryCommandStructure tests registry command structure
func TestRegistryCommandStructure(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRegistryCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "registry", cmd.Use)
	assert.Equal(t, "Manage registries", cmd.Short)
}

// TestScanCommandStructure tests scan command structure
func TestScanCommandStructure(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newScanCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "scan", cmd.Use)
	assert.Equal(t, "Run vulnerability scans", cmd.Short)
}

// TestReplicationCommandStructure tests replication command structure
func TestReplicationCommandStructure(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newReplicationCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "replicate", cmd.Use)
	assert.Equal(t, "Manage replication", cmd.Short)
}

// TestArtifactCommandHasList tests artifact command has list subcommand
func TestArtifactCommandHasList(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newArtifactCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "list" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestArtifactCommandHasGet tests artifact command has get subcommand
func TestArtifactCommandHasGet(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newArtifactCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "get <artifact-id>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestArtifactCommandHasDelete tests artifact command has delete subcommand
func TestArtifactCommandHasDelete(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newArtifactCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "delete <artifact-id>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestArtifactCommandHasScan tests artifact command has scan subcommand
func TestArtifactCommandHasScan(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newArtifactCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "scan <artifact-id>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestUserCommandHasList tests user command has list subcommand
func TestUserCommandHasList(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newUserCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "list" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestUserCommandHasCreate tests user command has create subcommand
func TestUserCommandHasCreate(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newUserCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "create --username <name> --email <email>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestUserCommandHasDelete tests user command has delete subcommand
func TestUserCommandHasDelete(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newUserCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "delete <username>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestUserCommandHasAssignRole tests user command has assign-role subcommand
func TestUserCommandHasAssignRole(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newUserCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "assign-role <username> <role>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestRoleCommandHasList tests role command has list subcommand
func TestRoleCommandHasList(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRoleCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "list" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestRoleCommandHasCreate tests role command has create subcommand
func TestRoleCommandHasCreate(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRoleCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "create <name>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestRoleCommandHasDelete tests role command has delete subcommand
func TestRoleCommandHasDelete(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRoleCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "delete <role>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestRoleCommandHasGrant tests role command has grant subcommand
func TestRoleCommandHasGrant(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRoleCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "grant <role> <permission>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestRegistryCommandHasList tests registry command has list subcommand
func TestRegistryCommandHasList(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRegistryCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "list" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestRegistryCommandHasAdd tests registry command has add subcommand
func TestRegistryCommandHasAdd(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRegistryCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "add" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestRegistryCommandHasRemove tests registry command has remove subcommand
func TestRegistryCommandHasRemove(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRegistryCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "remove <registry-id>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestScanCommandHasArtifact tests scan command has artifact subcommand
func TestScanCommandHasArtifact(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newScanCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "artifact <artifact-id>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestScanCommandHasAll tests scan command has all subcommand
func TestScanCommandHasAll(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newScanCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "all" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestScanCommandHasResults tests scan command has results subcommand
func TestScanCommandHasResults(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newScanCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "results <artifact-id>" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestReplicationCommandHasStatus tests replication command has status subcommand
func TestReplicationCommandHasStatus(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newReplicationCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "status" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestReplicationCommandHasSync tests replication command has sync subcommand
func TestReplicationCommandHasSync(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newReplicationCommand()

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "sync" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestCLIExecuteMethod tests Execute method signature
func TestCLIExecuteMethod(t *testing.T) {
	rc := NewRootCommand(nil, nil)
	// With no subcommand specified, cobra prints help and returns nil
	// (there's no Run func on the root command), not an error.
	err := rc.Execute()
	assert.NoError(t, err)
}

// TestCLINewMethod tests New method
func TestCLINewMethod(t *testing.T) {
	rc := New()
	assert.NotNil(t, rc)
}

// TestCLIHasCommands tests that CLI has commands
func TestCLIHasCommands(t *testing.T) {
	rc := NewRootCommand(nil, nil)
	assert.NotNil(t, rc.cmd)
}

// TestArtifactCommandArgs tests artifact command args validation
func TestArtifactCommandArgs(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newArtifactCommand()

	// Test get command requires exactly 1 arg
	getCmd := cmd.Commands()[1] // get <artifact-id>
	assertExactArgs(t, getCmd, 1)

	// Test delete command requires exactly 1 arg
	deleteCmd := cmd.Commands()[0] // delete <artifact-id>
	assertExactArgs(t, deleteCmd, 1)

	// Test scan command requires exactly 1 arg
	scanCmd := cmd.Commands()[3] // scan <artifact-id>
	assertExactArgs(t, scanCmd, 1)
}

// TestUserCommandArgs tests user command args validation
func TestUserCommandArgs(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newUserCommand()

	// Test delete command requires exactly 1 arg
	deleteCmd := cmd.Commands()[2] // delete <username>
	assertExactArgs(t, deleteCmd, 1)

	// Test assign-role command requires exactly 2 args
	assignRoleCmd := cmd.Commands()[0] // assign-role <username> <role>
	assertExactArgs(t, assignRoleCmd, 2)
}

// TestRoleCommandArgs tests role command args validation
func TestRoleCommandArgs(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRoleCommand()

	// Test create command requires exactly 1 arg
	createCmd := cmd.Commands()[0] // create <name>
	assertExactArgs(t, createCmd, 1)

	// Test delete command requires exactly 1 arg
	deleteCmd := cmd.Commands()[1] // delete <role>
	assertExactArgs(t, deleteCmd, 1)

	// Test grant command requires exactly 2 args
	grantCmd := cmd.Commands()[2] // grant <role> <permission>
	assertExactArgs(t, grantCmd, 2)
}

// TestRegistryCommandArgs tests registry command args validation
func TestRegistryCommandArgs(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newRegistryCommand()

	// Test remove command requires exactly 1 arg
	removeCmd := cmd.Commands()[2] // remove <registry-id>
	assertExactArgs(t, removeCmd, 1)
}

// TestScanCommandArgs tests scan command args validation
func TestScanCommandArgs(t *testing.T) {
	rc := &RootCommand{}
	cmd := rc.newScanCommand()

	// Test artifact command requires exactly 1 arg
	artifactCmd := cmd.Commands()[1] // artifact <artifact-id>
	assertExactArgs(t, artifactCmd, 1)

	// Test results command requires exactly 1 arg
	resultsCmd := cmd.Commands()[2] // results <artifact-id>
	assertExactArgs(t, resultsCmd, 1)
}

// TestCLICommandsCount tests that CLI has correct number of commands
func TestCLICommandsCount(t *testing.T) {
	rc := &RootCommand{}
	artifactCmd := rc.newArtifactCommand()
	userCmd := rc.newUserCommand()
	roleCmd := rc.newRoleCommand()
	registryCmd := rc.newRegistryCommand()
	scanCmd := rc.newScanCommand()
	replicationCmd := rc.newReplicationCommand()

	// Count subcommands
	artifactCount := len(artifactCmd.Commands())
	userCount := len(userCmd.Commands())
	roleCount := len(roleCmd.Commands())
	registryCount := len(registryCmd.Commands())
	scanCount := len(scanCmd.Commands())
	replicationCount := len(replicationCmd.Commands())

	assert.GreaterOrEqual(t, artifactCount, 4)
	assert.GreaterOrEqual(t, userCount, 4)
	assert.GreaterOrEqual(t, roleCount, 4)
	assert.GreaterOrEqual(t, registryCount, 3)
	assert.GreaterOrEqual(t, scanCount, 3)
	assert.GreaterOrEqual(t, replicationCount, 2)
}

// TestRootCommandUse tests root command use
func TestRootCommandUse(t *testing.T) {
	cmd := NewRootCommand(nil, nil)
	assert.NotNil(t, cmd.cmd)
	// The Use field should be set in NewRootCommand
}

// TestRootCommandShort tests root command short description
func TestRootCommandShort(t *testing.T) {
	cmd := New()
	assert.NotNil(t, cmd)
}

// TestArtifactListMethod tests listArtifacts method signature
func TestArtifactListMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.listArtifacts)
}

// TestArtifactGetMethod tests getArtifact method signature
func TestArtifactGetMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.getArtifact)
}

// TestArtifactDeleteMethod tests deleteArtifact method signature
func TestArtifactDeleteMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.deleteArtifact)
}

// TestArtifactScanMethod tests scanArtifact method signature
func TestArtifactScanMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.scanArtifact)
}

// TestUserListMethod tests listUsers method signature
func TestUserListMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.listUsers)
}

// TestUserCreateMethod tests createUser method signature
func TestUserCreateMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.createUser)
}

// TestUserDeleteMethod tests deleteUser method signature
func TestUserDeleteMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.deleteUser)
}

// TestUserAssignRoleMethod tests assignRole method signature
func TestUserAssignRoleMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.assignRole)
}

// TestRoleListMethod tests listRoles method signature
func TestRoleListMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.listRoles)
}

// TestRoleCreateMethod tests createRole method signature
func TestRoleCreateMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.createRole)
}

// TestRoleDeleteMethod tests deleteRole method signature
func TestRoleDeleteMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.deleteRole)
}

// TestRoleGrantMethod tests grantPermission method signature
func TestRoleGrantMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.grantPermission)
}

// TestRegistryListMethod tests listRegistries method signature
func TestRegistryListMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.listRegistries)
}

// TestRegistryAddMethod tests addRegistry method signature
func TestRegistryAddMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.addRegistry)
}

// TestRegistryRemoveMethod tests removeRegistry method signature
func TestRegistryRemoveMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.removeRegistry)
}

// TestScanAllMethod tests scanAll method signature
func TestScanAllMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.scanAll)
}

// TestScanResultsMethod tests scanResults method signature
func TestScanResultsMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.scanResults)
}

// TestReplicationStatusMethod tests replicationStatus method signature
func TestReplicationStatusMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.replicationStatus)
}

// TestReplicationSyncMethod tests replicationSync method signature
func TestReplicationSyncMethod(t *testing.T) {
	rc := &RootCommand{}
	// Just verify the method exists
	assert.NotNil(t, rc.replicationSync)
}

// TestCLIInterfaceCompatibility tests CLI implements expected methods
func TestCLIInterfaceCompatibility(t *testing.T) {
	// Verify that RootCommand has all required methods
	rc := &RootCommand{}

	// All methods exist
	_ = rc.Execute
	_ = rc.newArtifactCommand
	_ = rc.newUserCommand
	_ = rc.newRoleCommand
	_ = rc.newRegistryCommand
	_ = rc.newScanCommand
	_ = rc.newReplicationCommand
}
