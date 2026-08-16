package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var replicationCmd = &cobra.Command{
	Use:   "replication",
	Short: "Manage replication between regions",
}

var replicationStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show replication status",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Replication Status:")
		fmt.Println("  Region: us-east-1 - Status: Running")
		fmt.Println("  Region: us-west-2 - Status: Running")
		fmt.Println("  Region: eu-west-1 - Status: Running")
	},
}

var replicationSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Trigger replication sync",
	Run: func(cmd *cobra.Command, args []string) {
		region, _ := cmd.Flags().GetString("region")
		fmt.Printf("Triggering replication sync")
		if region != "" {
			fmt.Printf(" for region: %s", region)
		}
		fmt.Println()
	},
}

var replicationRegionsCmd = &cobra.Command{
	Use:   "regions",
	Short: "List replication regions",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Replication Regions:")
		fmt.Println("  - us-east-1")
		fmt.Println("  - us-west-2")
		fmt.Println("  - eu-west-1")
		fmt.Println("  - ap-southeast-1")
	},
}

func init() {
	replicationSyncCmd.Flags().String("region", "", "Region to sync (optional)")

	replicationCmd.AddCommand(
		replicationStatusCmd,
		replicationSyncCmd,
		replicationRegionsCmd,
	)
	rootCmd.AddCommand(replicationCmd)
}
