package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	cstewardConfig "context-steward/internal/config"
	cstewardDb "context-steward/internal/db"
)

var forceInit bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Context Steward in the workspace",
	Long:  `Create .context-steward/ directory, write default configuration, initialize the SQLite database, and optionally generate STEWARD.md.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve workspace root
		wsPath := workspace
		if wsPath == "" {
			var err error
			wsPath, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}
		}
		wsPath, err := filepath.Abs(wsPath)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute workspace path: %w", err)
		}

		stewardDir := filepath.Join(wsPath, ".context-steward")
		configPath := filepath.Join(stewardDir, "config.yaml")
		dbPath := filepath.Join(stewardDir, "index.sqlite")

		// Check if already initialized
		if _, err := os.Stat(stewardDir); err == nil && !forceInit {
			if !quiet {
				fmt.Printf("Context Steward already initialized in %s.\nUse --force to reinitialize (warning: overwrites config).\n", stewardDir)
			}
			return nil
		}

		// Create directories
		if !dryRun {
			if err := os.MkdirAll(stewardDir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", stewardDir, err)
			}
			if err := os.MkdirAll(filepath.Join(stewardDir, "packets"), 0755); err != nil {
				return fmt.Errorf("failed to create packets directory at %s: %w", stewardDir, err)
			}
		}

		// Write config.yaml
		if !dryRun {
			if _, err := os.Stat(configPath); os.IsNotExist(err) || forceInit {
				err = os.WriteFile(configPath, []byte(cstewardConfig.DefaultConfigYAML()), 0644)
				if err != nil {
					return fmt.Errorf("failed to write config.yaml: %w", err)
				}
				if !quiet {
					fmt.Printf("Created config file: %s\n", configPath)
				}
			}
		}

		// Initialize Database
		if !dryRun {
			dbConn, err := cstewardDb.InitDB(dbPath)
			if err != nil {
				return fmt.Errorf("failed to initialize SQLite database: %w", err)
			}
			dbConn.Close()
			if !quiet {
				fmt.Printf("Initialized database at %s\n", dbPath)
			}
		}

		// STEWARD.md check
		stewardMdPath := filepath.Join(wsPath, "STEWARD.md")
		createStewardMd := false

		if _, err := os.Stat(stewardMdPath); os.IsNotExist(err) {
			if quiet || jsonOut {
				createStewardMd = true // default to true in non-interactive / quiet mode
			} else {
				// Interactive prompt
				prompt := huh.NewConfirm().
					Title("Would you like to create STEWARD.md at the workspace root?").
					Description("STEWARD.md defines guidelines for premium AI models in this workspace.").
					Value(&createStewardMd)
				if err := prompt.Run(); err != nil {
					return err
				}
			}
		}

		if createStewardMd && !dryRun {
			stewardMdContent := `# Context Steward Policy
Before making project-level recommendations, AI agents should:
1. Request or generate a task-specific Context Steward packet.
2. Prefer authority-ranked context over general summaries.
3. Identify stale, missing, or conflicting context.
4. Avoid requesting broad history unless necessary.
5. Keep token usage proportional to the task.
6. Produce a handoff summary after major planning sessions.
7. Suggest decision capture when durable decisions are made.
`
			err = os.WriteFile(stewardMdPath, []byte(stewardMdContent), 0644)
			if err != nil {
				return fmt.Errorf("failed to write STEWARD.md: %w", err)
			}
			if !quiet {
				fmt.Printf("Created policy file: %s\n", stewardMdPath)
			}
		}

		if !quiet {
			fmt.Println("Context Steward initialization completed successfully!")
		}

		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "force initialization (overwrites config)")
	rootCmd.AddCommand(initCmd)
}
