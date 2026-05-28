package cmd

import (
	"github.com/spf13/cobra"
)

var rebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild stale summaries and metadata",
	Long:  `Scan the workspace and automatically regenerate summaries for all modified or stale files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Delegate directly to the build command logic
		return buildCmd.RunE(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(rebuildCmd)
}
