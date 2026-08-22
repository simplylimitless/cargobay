package main

import (
	"context"
	"fmt"
	"os"

	"github.com/simplylimitless/cargobay/backend/pkg/migrate"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Manage the database schema",
	Long: `Manage the database schema.

The server also applies pending migrations automatically on startup (taking
a backup first if the database already has data), so this command is only
needed for manual/CI use -- e.g. migrating ahead of a deploy, or checking
status without starting the server.`,
}

var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply the baseline schema and any pending migrations",
	Run: func(cmd *cobra.Command, args []string) {
		db := mustInitDB()
		defer db.Disconnect()

		ctx := context.Background()
		applied, err := migrate.New(db).Apply(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error applying migrations: %v\n", err)
			os.Exit(1)
		}

		if len(applied) == 0 {
			fmt.Println("Database is already up to date")
			return
		}
		fmt.Println("Applied migrations:")
		for _, name := range applied {
			fmt.Printf("  %s\n", name)
		}
	},
}

var migrateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "List pending migrations without applying them",
	Run: func(cmd *cobra.Command, args []string) {
		db := mustInitDB()
		defer db.Disconnect()

		pending, err := migrate.New(db).Pending(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking migration status: %v\n", err)
			os.Exit(1)
		}

		if len(pending) == 0 {
			fmt.Println("Database is up to date")
			return
		}
		fmt.Println("Pending migrations:")
		for _, name := range pending {
			fmt.Printf("  %s\n", name)
		}
	},
}

func init() {
	migrateCmd.AddCommand(migrateUpCmd, migrateStatusCmd)
	rootCmd.AddCommand(migrateCmd)
}
