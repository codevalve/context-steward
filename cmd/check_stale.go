package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"context-steward/internal/db"
)

var checkStaleCmd = &cobra.Command{
	Use:   "check-stale",
	Short: "Detect stale summaries and packets",
	Long:  `Scan the workspace and check if any file summaries or context packets are outdated compared to current file modifications.`,
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

		// Run silent scan first to ensure file hashes in database match physical files
		oldQuiet := quiet
		quiet = true
		err = scanCmd.RunE(cmd, []string{})
		quiet = oldQuiet
		if err != nil {
			return fmt.Errorf("pre-check scan failed: %w", err)
		}

		stewardDir := filepath.Join(wsPath, ".context-steward")
		dbPath := filepath.Join(stewardDir, "index.sqlite")

		// Open DB connection
		dbConn, err := db.InitDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer dbConn.Close()

		// Query for files where the summary is explicitly marked stale or has a mismatching source hash
		query := `
		SELECT f.path FROM files f
		JOIN summaries s ON f.id = s.file_id
		WHERE f.ignored = 0 AND (s.stale = 1 OR s.source_hash != f.content_hash)
		`
		rows, err := dbConn.Query(query)
		if err != nil {
			return fmt.Errorf("failed to query stale summaries: %w", err)
		}
		defer rows.Close()

		var staleFiles []string
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); err != nil {
				return err
			}
			staleFiles = append(staleFiles, path)
		}

		if len(staleFiles) == 0 {
			if !quiet {
				fmt.Println("✓ All context summaries are fresh and up to date.")
			}
			return nil
		}

		if !quiet {
			fmt.Printf("Found %d stale summaries:\n", len(staleFiles))
			for _, f := range staleFiles {
				fmt.Printf("  - %s\n", f)
			}
			fmt.Println("\nRecommended action: Run 'context-steward rebuild' to regenerate these summaries.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(checkStaleCmd)
}
