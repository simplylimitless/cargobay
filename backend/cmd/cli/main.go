package main

import (
	"fmt"
	"os"

	"github.com/simplylimitless/cargobay/backend/pkg/config"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cargobay",
	Short: "Cargobay - Enterprise Artifact Registry CLI",
	Long: `Cargobay is an enterprise-grade universal artifact registry supporting
millions of users with petabyte-scale storage, high availability, and
comprehensive security.`,
}

func mustInitDB() *database.Database {
	cfg := config.LoadConfig()
	db := database.New(cfg.Database.DSN)
	if err := db.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to database: %v\n", err)
		os.Exit(1)
	}
	return db
}

func mustInitRBAC() *rbac.RBAC {
	db := mustInitDB()
	rbacMgr := rbac.New(db)
	return rbacMgr
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the Cargobay API server",
	Run: func(cmd *cobra.Command, args []string) {
		port, _ := cmd.Flags().GetInt("port")
		host, _ := cmd.Flags().GetString("host")

		fmt.Printf("Starting Cargobay server on %s:%d\n", host, port)
		fmt.Println("Note: Full server startup requires database, cache, and storage configuration.")
		fmt.Println("Use the API package programmatically or run with a proper config.")
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("cargobay CLI version 1.0.0")
	},
}

func init() {
	serverCmd.Flags().IntP("port", "p", 8080, "Server port")
	serverCmd.Flags().StringP("host", "H", "0.0.0.0", "Server host")
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
