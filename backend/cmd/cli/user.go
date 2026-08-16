package main

import (
	"fmt"
	"os"

	"github.com/anthropics/cargobay/backend/pkg/auth"
	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users in the registry",
}

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all users",
	Run: func(cmd *cobra.Command, args []string) {
		db := mustInitDB()
		defer db.Disconnect()

		users, err := db.ListUsers()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing users: %v\n", err)
			os.Exit(1)
		}

		if len(users) == 0 {
			fmt.Println("No users found")
			return
		}

		fmt.Println("Users:")
		for _, u := range users {
			status := "active"
			if !u.IsActive {
				status = "inactive"
			}
			fmt.Printf("  %s (%s) - %s - roles: %v\n", u.Username, u.UserID, status, u.Roles)
		}
	},
}

var userGetCmd = &cobra.Command{
	Use:   "get <user-id>",
	Short: "Get user details",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		db := mustInitDB()
		defer db.Disconnect()

		user, err := db.GetUser(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting user: %v\n", err)
			os.Exit(1)
		}
		if user == nil {
			fmt.Fprintf(os.Stderr, "User not found: %s\n", id)
			os.Exit(1)
		}

		status := "active"
		if !user.IsActive {
			status = "inactive"
		}

		fmt.Printf("User: %s\n", user.Username)
		fmt.Printf("  ID: %s\n", user.UserID)
		fmt.Printf("  Email: %s\n", user.Email)
		fmt.Printf("  Status: %s\n", status)
		fmt.Printf("  Created: %s\n", user.CreatedAt)
		if user.LastLogin != nil {
			fmt.Printf("  Last Login: %s\n", user.LastLogin)
		}
	},
}

var userCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new user",
	Run: func(cmd *cobra.Command, args []string) {
		username, _ := cmd.Flags().GetString("username")
		email, _ := cmd.Flags().GetString("email")
		password, _ := cmd.Flags().GetString("password")
		rolesStr, _ := cmd.Flags().GetString("roles")

		if username == "" || email == "" || password == "" {
			fmt.Fprintf(os.Stderr, "Error: --username, --email, and --password are required\n")
			os.Exit(1)
		}

		db := mustInitDB()
		defer db.Disconnect()

		// Parse roles
		var roles []string
		if rolesStr != "" {
			for _, r := range splitCSV(rolesStr) {
				roles = append(roles, r)
			}
		}

		passwordHash, err := auth.HashPassword(password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error hashing password: %v\n", err)
			os.Exit(1)
		}

		user, err := db.CreateUser(username, email, passwordHash, roles)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating user: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("User created successfully:\n")
		fmt.Printf("  ID: %s\n", user.UserID)
		fmt.Printf("  Username: %s\n", user.Username)
		fmt.Printf("  Email: %s\n", user.Email)
	},
}

var userDeleteCmd = &cobra.Command{
	Use:   "delete <user-id>",
	Short: "Delete (deactivate) a user",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		db := mustInitDB()
		defer db.Disconnect()

		user, err := db.GetUser(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting user: %v\n", err)
			os.Exit(1)
		}
		if user == nil {
			fmt.Fprintf(os.Stderr, "User not found: %s\n", id)
			os.Exit(1)
		}

		// Deactivate user
		err = db.SetUserActive(id, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deactivating user: %v\n", err)
			os.Exit(1)
		}

		// Invalidate all access keys
		keys, err := db.ListUserAccessKeys(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing access keys: %v\n", err)
			os.Exit(1)
		}
		for _, key := range keys {
			if err := db.InvalidateAccessKey(key.ID); err != nil {
				fmt.Fprintf(os.Stderr, "Error invalidating key %s: %v\n", key.ID, err)
				os.Exit(1)
			}
		}

		fmt.Printf("User %s (%s) has been deactivated\n", user.Username, user.UserID)
	},
}

var userActivateCmd = &cobra.Command{
	Use:   "activate <user-id>",
	Short: "Re-activate a user",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		db := mustInitDB()
		defer db.Disconnect()

		user, err := db.GetUser(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting user: %v\n", err)
			os.Exit(1)
		}
		if user == nil {
			fmt.Fprintf(os.Stderr, "User not found: %s\n", id)
			os.Exit(1)
		}

		err = db.SetUserActive(id, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error activating user: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("User %s (%s) has been re-activated\n", user.Username, user.UserID)
	},
}

var userRolesCmd = &cobra.Command{
	Use:   "roles <user-id>",
	Short: "List user roles",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		db := mustInitDB()
		defer db.Disconnect()

		user, err := db.GetUser(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting user: %v\n", err)
			os.Exit(1)
		}
		if user == nil {
			fmt.Fprintf(os.Stderr, "User not found: %s\n", id)
			os.Exit(1)
		}

		roles, err := db.GetUserRoles(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting roles: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Roles for user %s (%s):\n", user.Username, user.UserID)
		for _, r := range roles {
			fmt.Printf("  - %s\n", r)
		}
	},
}

var userAddRoleCmd = &cobra.Command{
	Use:   "add-role <user-id> <role>",
	Short: "Add a role to a user",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		role := args[1]
		db := mustInitDB()
		defer db.Disconnect()

		user, err := db.GetUser(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting user: %v\n", err)
			os.Exit(1)
		}
		if user == nil {
			fmt.Fprintf(os.Stderr, "User not found: %s\n", id)
			os.Exit(1)
		}

		err = db.AssignRoleToUser(id, role)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding role: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Role '%s' added to user %s (%s)\n", role, user.Username, user.UserID)
	},
}

var userRemoveRoleCmd = &cobra.Command{
	Use:   "remove-role <user-id> <role>",
	Short: "Remove a role from a user",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		role := args[1]
		db := mustInitDB()
		defer db.Disconnect()

		user, err := db.GetUser(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting user: %v\n", err)
			os.Exit(1)
		}
		if user == nil {
			fmt.Fprintf(os.Stderr, "User not found: %s\n", id)
			os.Exit(1)
		}

		err = db.RevokeRoleFromUser(id, role)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error removing role: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Role '%s' removed from user %s (%s)\n", role, user.Username, user.UserID)
	},
}

func init() {
	userCreateCmd.Flags().String("username", "", "Username (required)")
	userCreateCmd.Flags().String("email", "", "Email (required)")
	userCreateCmd.Flags().String("password", "", "Password (required)")
	userCreateCmd.Flags().String("roles", "", "Comma-separated list of roles")

	userCmd.AddCommand(
		userListCmd,
		userGetCmd,
		userCreateCmd,
		userDeleteCmd,
		userActivateCmd,
		userRolesCmd,
		userAddRoleCmd,
		userRemoveRoleCmd,
	)
	rootCmd.AddCommand(userCmd)
}

// splitCSV splits a comma-separated string into a slice
func splitCSV(s string) []string {
	var result []string
	for _, r := range split(s, ',') {
		trimmed := trimSpace(r)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// split splits a string by a separator
func split(s string, sep rune) []string {
	var result []string
	var current []rune
	for _, r := range s {
		if r == sep {
			result = append(result, string(current))
			current = nil
		} else {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		result = append(result, string(current))
	}
	return result
}

// trimSpace removes leading and trailing whitespace from a string
func trimSpace(s string) string {
	start, end := 0, len(s)-1
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end] == ' ' || s[end] == '\t' || s[end] == '\n' || s[end] == '\r') {
		end--
	}
	return s[start : end+1]
}
