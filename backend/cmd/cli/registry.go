package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage registries in the registry",
}

var registryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registries",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Listing registries...")
	},
}

var registryGetCmd = &cobra.Command{
	Use:   "get <registry-id>",
	Short: "Get registry details",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		fmt.Printf("Getting registry: %s\n", id)
	},
}

var registryAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new registry",
	Run: func(cmd *cobra.Command, args []string) {
		name, _ := cmd.Flags().GetString("name")
		url, _ := cmd.Flags().GetString("url")
		registryType, _ := cmd.Flags().GetString("type")
		fmt.Printf("Adding registry: %s (%s) at %s\n", name, registryType, url)
	},
}

var registryDeleteCmd = &cobra.Command{
	Use:   "delete <registry-id>",
	Short: "Delete a registry",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		id := args[0]
		fmt.Printf("Deleting registry: %s\n", id)
	},
}

func init() {
	registryAddCmd.Flags().String("name", "", "Registry name (required)")
	registryAddCmd.Flags().String("url", "", "Registry URL (required)")
	registryAddCmd.Flags().String("type", "generic", "Registry type (npm, maven, docker, etc.)")

	registryCmd.AddCommand(
		registryListCmd,
		registryGetCmd,
		registryAddCmd,
		registryDeleteCmd,
	)
	rootCmd.AddCommand(registryCmd)
}
