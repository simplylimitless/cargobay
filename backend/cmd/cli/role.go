package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var roleCmd = &cobra.Command{
	Use:   "role",
	Short: "Manage roles in the registry",
}

var roleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all roles",
	Run: func(cmd *cobra.Command, args []string) {
		db := mustInitDB()
		defer db.Disconnect()

		rbacMgr := mustInitRBAC()

		roles := rbacMgr.ListRoles()
		if len(roles) == 0 {
			fmt.Println("No roles found")
			return
		}

		fmt.Println("Roles:")
		for _, r := range roles {
			roleType := "custom"
			if r.IsSystem {
				roleType = "system"
			}
			fmt.Printf("  %s (%s)\n", r.Name, r.ID)
			fmt.Printf("    Type: %s\n", roleType)
			if r.Description != "" {
				fmt.Printf("    Description: %s\n", r.Description)
			}
		}
	},
}

var roleGetCmd = &cobra.Command{
	Use:   "get <role-id>",
	Short: "Get role details",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		db := mustInitDB()
		defer db.Disconnect()

		rbacMgr := mustInitRBAC()

		role := rbacMgr.GetRole(id)
		if role == nil {
			fmt.Fprintf(os.Stderr, "Role not found: %s\n", id)
			os.Exit(1)
		}

		roleType := "custom"
		if role.IsSystem {
			roleType = "system"
		}

		fmt.Printf("Role: %s\n", role.Name)
		fmt.Printf("  ID: %s\n", role.ID)
		fmt.Printf("  Type: %s\n", roleType)
		if role.Description != "" {
			fmt.Printf("  Description: %s\n", role.Description)
		}
	},
}

var roleCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new role",
	Run: func(cmd *cobra.Command, args []string) {
		name, _ := cmd.Flags().GetString("name")
		description, _ := cmd.Flags().GetString("description")
		permissionsStr, _ := cmd.Flags().GetString("permissions")

		if name == "" {
			fmt.Fprintf(os.Stderr, "Error: --name is required\n")
			os.Exit(1)
		}

		var permissions []string
		if permissionsStr != "" {
			permissions = splitCSV(permissionsStr)
		}

		db := mustInitDB()
		defer db.Disconnect()

		rbacMgr := mustInitRBAC()

		role, err := rbacMgr.CreateRole(name, description, permissions)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating role: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Role created successfully:\n")
		fmt.Printf("  ID: %s\n", role.ID)
		fmt.Printf("  Name: %s\n", role.Name)
		if role.Description != "" {
			fmt.Printf("  Description: %s\n", role.Description)
		}
	},
}

var roleDeleteCmd = &cobra.Command{
	Use:   "delete <role-id>",
	Short: "Delete a role",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		db := mustInitDB()
		defer db.Disconnect()

		rbacMgr := mustInitRBAC()

		role := rbacMgr.GetRole(id)
		if role == nil {
			fmt.Fprintf(os.Stderr, "Role not found: %s\n", id)
			os.Exit(1)
		}

		if role.IsSystem {
			fmt.Fprintf(os.Stderr, "Error: Cannot delete system role: %s\n", role.Name)
			os.Exit(1)
		}

		err := rbacMgr.DeleteRole(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting role: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Role %s (%s) has been deleted\n", role.Name, role.ID)
	},
}

var roleAddPermissionCmd = &cobra.Command{
	Use:   "add-permission <role-id> <permission>",
	Short: "Add a permission to a role",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		roleID := args[0]
		permission := args[1]
		db := mustInitDB()
		defer db.Disconnect()

		rbacMgr := mustInitRBAC()

		role := rbacMgr.GetRole(roleID)
		if role == nil {
			fmt.Fprintf(os.Stderr, "Role not found: %s\n", roleID)
			os.Exit(1)
		}

		err := rbacMgr.AddPermissionToRole(roleID, permission)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding permission: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Permission '%s' added to role %s (%s)\n", permission, role.Name, role.ID)
	},
}

var roleRemovePermissionCmd = &cobra.Command{
	Use:   "remove-permission <role-id> <permission>",
	Short: "Remove a permission from a role",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		roleID := args[0]
		permission := args[1]
		db := mustInitDB()
		defer db.Disconnect()

		rbacMgr := mustInitRBAC()

		role := rbacMgr.GetRole(roleID)
		if role == nil {
			fmt.Fprintf(os.Stderr, "Role not found: %s\n", roleID)
			os.Exit(1)
		}

		err := rbacMgr.RemovePermissionFromRole(roleID, permission)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error removing permission: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Permission '%s' removed from role %s (%s)\n", permission, role.Name, role.ID)
	},
}

func init() {
	roleCreateCmd.Flags().String("name", "", "Role name (required)")
	roleCreateCmd.Flags().String("description", "", "Role description")
	roleCreateCmd.Flags().String("permissions", "", "Comma-separated list of permissions")

	roleCmd.AddCommand(
		roleListCmd,
		roleGetCmd,
		roleCreateCmd,
		roleDeleteCmd,
		roleAddPermissionCmd,
		roleRemovePermissionCmd,
	)
	rootCmd.AddCommand(roleCmd)
}
