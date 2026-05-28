package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile   string
	workspace string
	jsonOut   bool
	quiet     bool
	verbose   bool
	noLLM     bool
	dryRun    bool
)

var rootCmd = &cobra.Command{
	Use:   "context-steward",
	Short: "Context Steward manages AI workspace context",
	Long: `Context Steward is a local-first CLI application that manages AI workspace context
by scanning project files, summarizing relevant material with a local LLM, tracking
authority and freshness, and generating compact context packets for premium AI models.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .context-steward/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&workspace, "workspace", "", "workspace root path (default is current directory)")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "suppress non-error output")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable verbose logging")
	rootCmd.PersistentFlags().BoolVar(&noLLM, "no-llm", false, "disable local LLM calls")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "dry run (do not write to disk/db)")
}
