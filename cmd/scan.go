package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"context-steward/internal/config"
	"context-steward/internal/db"
	"context-steward/internal/scanner"
)

type ScanStats struct {
	TotalFiles int `json:"total_files"`
	New        int `json:"new_files"`
	Modified   int `json:"modified_files"`
	Unchanged  int `json:"unchanged_files"`
	Ignored    int `json:"ignored_files"`
	Deleted    int `json:"deleted_files"`
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan the workspace files and index them",
	Long:  `Scan the workspace recursively, discover files, compute content hashes, detect changes, and update the local database index.`,
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
		dbPath := filepath.Join(stewardDir, "index.sqlite")

		// Load config
		cfg, err := config.LoadConfig(cfgFile, wsPath)
		if err != nil {
			return fmt.Errorf("failed to load configuration (run 'init' first): %w", err)
		}

		// Open DB connection
		dbConn, err := db.InitDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open SQLite database: %w", err)
		}
		defer dbConn.Close()

		scanStartTime := time.Now()

		// Walk/scan files
		results, err := scanner.ScanWorkspace(cfg)
		if err != nil {
			return fmt.Errorf("failed to scan workspace: %w", err)
		}

		stats := ScanStats{}
		stats.TotalFiles = len(results)

		for _, fileResult := range results {
			if fileResult.Ignored {
				stats.Ignored++
			}

			// Check if file already exists in DB
			existing, err := db.GetFileByPath(dbConn, fileResult.Path)
			if err != nil {
				return fmt.Errorf("database query error for %s: %w", fileResult.Path, err)
			}

			dbFile := db.File{
				Path:        fileResult.Path,
				FileType:    fileResult.FileType,
				ContentHash: fileResult.ContentHash,
				SizeBytes:   fileResult.SizeBytes,
				ModifiedAt:  fileResult.ModifiedAt,
				ScannedAt:   scanStartTime,
				Ignored:     fileResult.Ignored,
			}

			if existing == nil {
				if !fileResult.Ignored {
					stats.New++
				}
				if !dryRun {
					if err := db.UpsertFile(dbConn, &dbFile); err != nil {
						return fmt.Errorf("failed to save file %s to database: %w", fileResult.Path, err)
					}
				}
			} else {
				if fileResult.Ignored {
					// File is now ignored
					dbFile.ID = existing.ID
					if !dryRun {
						if err := db.UpsertFile(dbConn, &dbFile); err != nil {
							return fmt.Errorf("failed to update ignored file %s: %w", fileResult.Path, err)
						}
					}
				} else if existing.ContentHash != fileResult.ContentHash {
					stats.Modified++
					dbFile.ID = existing.ID
					if !dryRun {
						if err := db.UpsertFile(dbConn, &dbFile); err != nil {
							return fmt.Errorf("failed to update modified file %s: %w", fileResult.Path, err)
						}
						// Mark summary as stale if it exists
						summary, err := db.GetSummaryByFileID(dbConn, existing.ID)
						if err == nil && summary != nil {
							summary.Stale = true
							_ = db.UpsertSummary(dbConn, summary)
						}
					}
				} else {
					stats.Unchanged++
					// Update scanned_at timestamp to mark file as seen
					dbFile.ID = existing.ID
					if !dryRun {
						if err := db.UpsertFile(dbConn, &dbFile); err != nil {
							return fmt.Errorf("failed to update file timestamp %s: %w", fileResult.Path, err)
						}
					}
				}
			}
		}

		// Delete files not seen in this scan (i.e. deleted from workspace)
		if !dryRun {
			deletedCount, err := db.DeleteFilesNotScannedSince(dbConn, scanStartTime)
			if err != nil {
				return fmt.Errorf("failed to clean deleted files: %w", err)
			}
			stats.Deleted = int(deletedCount)
		}

		if jsonOut {
			data, err := json.MarshalIndent(stats, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else if !quiet {
			fmt.Println("Workspace scan complete:")
			fmt.Printf("  Total files discovered: %d\n", stats.TotalFiles)
			fmt.Printf("  New files indexed:      %d\n", stats.New)
			fmt.Printf("  Modified files updated: %d\n", stats.Modified)
			fmt.Printf("  Unchanged files:        %d\n", stats.Unchanged)
			fmt.Printf("  Ignored files:          %d\n", stats.Ignored)
			fmt.Printf("  Deleted files removed:  %d\n", stats.Deleted)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
}
