package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"context-steward/internal/config"
	"context-steward/internal/db"
	"context-steward/internal/packer"
)

var authorityReason string

var authorityCmd = &cobra.Command{
	Use:   "authority",
	Short: "Manage file authority levels",
}

var authorityReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review and update file authority levels interactively",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve workspace root
		wsPath := workspace
		var err error
		if wsPath == "" {
			wsPath, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}
		}
		wsPath, err = filepath.Abs(wsPath)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute workspace path: %w", err)
		}

		stewardDir := filepath.Join(wsPath, ".context-steward")
		dbPath := filepath.Join(stewardDir, "index.sqlite")

		// Load config
		cfg, err := config.LoadConfig(cfgFile, wsPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration (run 'init' first): %w", err)
		}

		// Open DB connection
		dbConn, err := db.InitDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer dbConn.Close()

		// Get all indexed files
		files, err := db.GetAllFiles(dbConn, false)
		if err != nil {
			return fmt.Errorf("failed to load indexed files: %w", err)
		}

		if len(files) == 0 {
			if !quiet {
				fmt.Println("No indexed files found in the database. Run 'scan' or 'build' first.")
			}
			return nil
		}

		if quiet || jsonOut {
			// In non-interactive/quiet/json modes, list files and their resolved authority levels
			type FileAuthInfo struct {
				Path           string `json:"path"`
				AuthorityLevel string `json:"authority_level"`
			}
			var list []FileAuthInfo
			for _, f := range files {
				auth := packer.ResolveAuthority(dbConn, cfg, f.Path)
				list = append(list, FileAuthInfo{Path: f.Path, AuthorityLevel: auth})
			}
			
			if jsonOut {
				data, _ := json.MarshalIndent(list, "", "  ")
				fmt.Println(string(data))
			} else {
				for _, item := range list {
					fmt.Printf("%s: %s\n", item.Path, item.AuthorityLevel)
				}
			}
			return nil
		}

		// Loop interactive review
		for {
			var options []huh.Option[string]
			for _, f := range files {
				auth := packer.ResolveAuthority(dbConn, cfg, f.Path)
				label := fmt.Sprintf("%s [%s]", f.Path, auth)
				options = append(options, huh.NewOption(label, f.Path))
			}
			options = append(options, huh.NewOption("← Exit Review", "exit"))

			var selectedFile string
			fileSelect := huh.NewSelect[string]().
				Title("Select a file to review authority level").
				Options(options...).
				Value(&selectedFile)

			if err := fileSelect.Run(); err != nil {
				return err
			}

			if selectedFile == "exit" {
				break
			}

			// Resolve current authority
			currAuth := packer.ResolveAuthority(dbConn, cfg, selectedFile)

			var newAuth string
			authSelect := huh.NewSelect[string]().
				Title(fmt.Sprintf("Assign authority level for '%s'", selectedFile)).
				Description(fmt.Sprintf("Current level: %s", currAuth)).
				Options(
					huh.NewOption("high (always prioritized, full content if budget allows)", "high"),
					huh.NewOption("medium (standard)", "medium"),
					huh.NewOption("low (summaries only)", "low"),
					huh.NewOption("archival (lowest priority)", "archival"),
					huh.NewOption("excluded (skip indexing/packing)", "excluded"),
				).
				Value(&newAuth)

			if err := authSelect.Run(); err != nil {
				return err
			}

			reason := ""
			reasonInput := huh.NewInput().
				Title("Reason for override (optional)").
				Value(&reason)

			if err := reasonInput.Run(); err != nil {
				return err
			}

			// Save to database
			authOverride := db.Authority{
				PathPattern:    selectedFile,
				AuthorityLevel: newAuth,
				Reason:         reason,
				ReviewedAt:     time.Now(),
			}

			if !dryRun {
				err = db.UpsertAuthority(dbConn, &authOverride)
				if err != nil {
					return fmt.Errorf("failed to save authority: %w", err)
				}
			}

			fmt.Printf("Updated '%s' to '%s' authority.\n\n", selectedFile, newAuth)
		}

		return nil
	},
}

var authoritySetCmd = &cobra.Command{
	Use:   "set [path-pattern] [level]",
	Short: "Set authority level for a file or pattern",
	Long:  `Create or update an authority override in the database for a specific file path or glob pattern. Levels: high, medium, low, archival, excluded.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		pattern := args[0]
		level := args[1]

		validLevels := map[string]bool{
			"high": true, "medium": true, "low": true, "archival": true, "excluded": true,
		}
		if !validLevels[level] {
			return fmt.Errorf("invalid authority level '%s'. Must be: high, medium, low, archival, or excluded", level)
		}

		// Resolve workspace root
		wsPath := workspace
		var err error
		if wsPath == "" {
			wsPath, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}
		}
		wsPath, err = filepath.Abs(wsPath)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute workspace path: %w", err)
		}

		stewardDir := filepath.Join(wsPath, ".context-steward")
		dbPath := filepath.Join(stewardDir, "index.sqlite")

		dbConn, err := db.InitDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open SQLite database: %w", err)
		}
		defer dbConn.Close()

		authOverride := db.Authority{
			PathPattern:    pattern,
			AuthorityLevel: level,
			Reason:         authorityReason,
			ReviewedAt:     time.Now(),
		}

		if !dryRun {
			err = db.UpsertAuthority(dbConn, &authOverride)
			if err != nil {
				return fmt.Errorf("failed to save authority override: %w", err)
			}
		}

		if !quiet {
			fmt.Printf("Successfully set '%s' to '%s' authority.\n", pattern, level)
		}

		return nil
	},
}

func init() {
	authoritySetCmd.Flags().StringVar(&authorityReason, "reason", "", "reason for override setting")
	authorityCmd.AddCommand(authorityReviewCmd)
	authorityCmd.AddCommand(authoritySetCmd)
	rootCmd.AddCommand(authorityCmd)
}
