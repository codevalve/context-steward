package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"context-steward/internal/config"
	"context-steward/internal/db"
	"context-steward/internal/llm"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Generate summaries for files",
	Long:  `Scan workspace for changes, identify files missing summaries or containing stale summaries, and use the local LLM to generate/update summaries.`,
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

		// Run workspace scan silently first to sync files state
		if !quiet {
			fmt.Println("Syncing workspace file states...")
		}
		
		// Temporarily silence the scan output
		oldQuiet := quiet
		quiet = true
		err = scanCmd.RunE(cmd, []string{})
		quiet = oldQuiet
		if err != nil {
			return fmt.Errorf("pre-build scan failed: %w", err)
		}

		stewardDir := filepath.Join(wsPath, ".context-steward")
		dbPath := filepath.Join(stewardDir, "index.sqlite")

		// Load config
		cfg, err := config.LoadConfig(cfgFile, wsPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}

		// Open DB connection
		dbConn, err := db.InitDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open SQLite database: %w", err)
		}
		defer dbConn.Close()

		// Query files that lack a summary, have a stale summary, or where the file's current hash doesn't match the summary's source hash
		query := `
		SELECT f.id, f.path, f.content_hash FROM files f
		LEFT JOIN summaries s ON f.id = s.file_id
		WHERE f.ignored = 0 AND (s.id IS NULL OR s.stale = 1 OR s.source_hash != f.content_hash)
		`
		rows, err := dbConn.Query(query)
		if err != nil {
			return fmt.Errorf("failed to query files needing summaries: %w", err)
		}
		defer rows.Close()

		type BuildTarget struct {
			ID          int64
			Path        string
			ContentHash string
		}

		var targets []BuildTarget
		for rows.Next() {
			var t BuildTarget
			if err := rows.Scan(&t.ID, &t.Path, &t.ContentHash); err != nil {
				return fmt.Errorf("failed to scan database build target: %w", err)
			}
			targets = append(targets, t)
		}
		rows.Close()

		if len(targets) == 0 {
			if !quiet {
				fmt.Println("All file summaries are up to date! Nothing to build.")
			}
			return nil
		}

		if !quiet {
			fmt.Printf("Found %d files needing summary generation/update.\n", len(targets))
		}

		if noLLM {
			if !quiet {
				fmt.Println("Skipping LLM summarization because --no-llm is active.")
			}
			return nil
		}

		llmClient := llm.NewClient(cfg)

		successCount := 0
		for i, target := range targets {
			fullPath := filepath.Join(wsPath, target.Path)
			if !quiet {
				fmt.Printf("[%d/%d] Summarizing %s... ", i+1, len(targets), target.Path)
			}

			// Read file content
			contentBytes, err := os.ReadFile(fullPath)
			if err != nil {
				if !quiet {
					fmt.Printf("FAILED: %v\n", err)
				}
				continue
			}

			var summaryText string
			if !dryRun {
				summaryText, err = llmClient.SummarizeFile(string(contentBytes))
				if err != nil {
					if !quiet {
						fmt.Printf("FAILED: Ollama error: %v\n", err)
					}
					continue
				}

				s := db.Summary{
					FileID:      target.ID,
					SummaryText: summaryText,
					SummaryKind: "general",
					ModelName:   cfg.LLM.Model,
					SourceHash:  target.ContentHash,
					CreatedAt:   time.Now(),
					Stale:       false,
				}

				if err := db.UpsertSummary(dbConn, &s); err != nil {
					if !quiet {
						fmt.Printf("FAILED: Database save error: %v\n", err)
					}
					continue
				}
			} else {
				summaryText = "[Dry Run Summary]"
			}

			if !quiet {
				fmt.Println("Done.")
				if verbose {
					fmt.Printf("Summary Text:\n%s\n\n", summaryText)
				}
			}
			successCount++
		}

		if !quiet {
			fmt.Printf("Context build complete: successfully generated %d of %d summaries.\n", successCount, len(targets))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(buildCmd)
}
