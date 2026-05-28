package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"context-steward/internal/config"
	"context-steward/internal/db"
	"context-steward/internal/llm"
	"context-steward/internal/scanner"
)

var harvestCmd = &cobra.Command{
	Use:   "harvest [file]",
	Short: "Ingest a session handoff file",
	Long:  `Ingest a markdown file containing AI session summaries or handoffs, extract decisions and open questions using the local LLM, and prompt for human approval before saving.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		handoffFilePath := args[0]

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

		// Read handoff file
		handoffContent, err := os.ReadFile(handoffFilePath)
		if err != nil {
			return fmt.Errorf("failed to read handoff file %s: %w", handoffFilePath, err)
		}

		if !quiet {
			fmt.Println("Extracting decisions and questions using local LLM...")
		}

		if noLLM {
			return fmt.Errorf("harvest command requires local LLM calls; cannot run with --no-llm")
		}

		// Extract with Ollama
		llmClient := llm.NewClient(cfg)
		extraction, err := llmClient.ExtractHandoff(string(handoffContent))
		if err != nil {
			return fmt.Errorf("LLM extraction failed: %w", err)
		}

		// Open DB connection
		dbConn, err := db.InitDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open SQLite database: %w", err)
		}
		defer dbConn.Close()

		// Save handoff record
		extSummaryBytes, _ := json.Marshal(extraction)
		h := db.Handoff{
			SourcePath:       handoffFilePath,
			ExtractedSummary: string(extSummaryBytes),
			CreatedAt:        time.Now(),
			Reviewed:         false,
		}
		if !dryRun {
			_ = db.IngestHandoff(dbConn, &h)
		}

		if len(extraction.Decisions) == 0 && len(extraction.OpenQuestions) == 0 {
			if !quiet {
				fmt.Println("No decisions or open questions were extracted from the handoff file.")
			}
			return nil
		}

		if !quiet {
			fmt.Printf("Extracted %d decisions and %d open questions.\n\n", len(extraction.Decisions), len(extraction.OpenQuestions))
		}

		// Human in the loop review for decisions
		decisionsSaved := 0
		for _, dec := range extraction.Decisions {
			if quiet || jsonOut {
				// Non-interactive mode: auto-accept to avoid blocking scripts
				if !quiet {
					fmt.Printf("Auto-accepting decision: %s\n", dec.Title)
				}
				err = saveHarvestedDecision(dbConn, cfg, wsPath, dec)
				if err == nil {
					decisionsSaved++
				}
				continue
			}

			// Interactive review
			var action string
			prompt := huh.NewSelect[string]().
				Title("Review Extracted Decision").
				Description(fmt.Sprintf("Title: %s\n\nDecision:\n%s\n\nRationale:\n%s\n\nConsequences:\n%s", 
					dec.Title, dec.DecisionText, dec.Rationale, dec.Consequences)).
				Options(
					huh.NewOption("Accept and Save as is", "save"),
					huh.NewOption("Edit and Save", "edit"),
					huh.NewOption("Discard/Skip", "discard"),
				).
				Value(&action)

			if err := prompt.Run(); err != nil {
				return err
			}

			if action == "discard" {
				continue
			}

			if action == "edit" {
				form := huh.NewForm(
					huh.NewGroup(
						huh.NewInput().Title("Edit Decision Title").Value(&dec.Title),
						huh.NewText().Title("Edit Decision Details").Value(&dec.DecisionText),
						huh.NewText().Title("Edit Rationale").Value(&dec.Rationale),
						huh.NewText().Title("Edit Consequences").Value(&dec.Consequences),
					),
				)
				if err := form.Run(); err != nil {
					return err
				}
			}

			err = saveHarvestedDecision(dbConn, cfg, wsPath, dec)
			if err != nil {
				return fmt.Errorf("failed to save decision: %w", err)
			}
			decisionsSaved++
		}

		// Print open questions
		if len(extraction.OpenQuestions) > 0 && !quiet && !jsonOut {
			fmt.Println("\n--- Extracted Open Questions ---")
			for _, q := range extraction.OpenQuestions {
				fmt.Printf("- %s\n", q)
			}
			fmt.Println("--------------------------------")
		}

		// Mark handoff as reviewed
		h.Reviewed = true
		if !dryRun {
			_ = db.IngestHandoff(dbConn, &h)
		}

		if !quiet {
			fmt.Printf("\nHarvest complete! Saved %d new decisions from session handoff.\n", decisionsSaved)
		}

		return nil
	},
}

func saveHarvestedDecision(dbConn *sql.DB, cfg *config.Config, wsPath string, dec llm.DecisionExtraction) error {
	decisionsDir := filepath.Join(wsPath, "decisions")
	if err := os.MkdirAll(decisionsDir, 0755); err != nil {
		return err
	}

	seq := 1
	files, err := os.ReadDir(decisionsDir)
	if err == nil {
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
				seq++
			}
		}
	}

	slug := slugify(dec.Title)
	fileName := fmt.Sprintf("%04d-%s.md", seq, slug)
	relPath := filepath.Join("decisions", fileName)
	fullPath := filepath.Join(wsPath, relPath)

	now := time.Now()
	mdContent := fmt.Sprintf(`# Decision: %s
Date: %s

## Status
Approved

## Decision
%s

## Rationale
%s

## Consequences
%s
`, dec.Title, now.Format("2006-01-02"), dec.DecisionText, dec.Rationale, dec.Consequences)

	err = os.WriteFile(fullPath, []byte(mdContent), 0644)
	if err != nil {
		return err
	}

	d := db.Decision{
		Title:          dec.Title,
		DecisionText:   dec.DecisionText,
		Rationale:      dec.Rationale,
		Consequences:   dec.Consequences,
		AuthorityLevel: "high", // Decisions are always high authority
		CreatedAt:      now,
		SourcePath:     relPath,
	}
	err = db.UpsertDecision(dbConn, &d)
	if err != nil {
		return err
	}

	// Auto-index the decision file
	fHash, _ := scanner.ComputeHash(fullPath)
	fi, _ := os.Stat(fullPath)
	fSize := int64(0)
	if fi != nil {
		fSize = fi.Size()
	}
	dbFile := db.File{
		Path:        relPath,
		FileType:    "md",
		ContentHash: fHash,
		SizeBytes:   fSize,
		ModifiedAt:  now,
		ScannedAt:   now,
		Ignored:     false,
	}
	_ = db.UpsertFile(dbConn, &dbFile)

	sText := fmt.Sprintf("Decision: %s. Details: %s. Rationale: %s.", dec.Title, dec.DecisionText, dec.Rationale)
	s := db.Summary{
		FileID:      dbFile.ID,
		SummaryText: sText,
		SummaryKind: "decision",
		ModelName:   cfg.LLM.Model,
		SourceHash:  fHash,
		CreatedAt:   now,
		Stale:       false,
	}
	_ = db.UpsertSummary(dbConn, &s)

	return nil
}

func init() {
	rootCmd.AddCommand(harvestCmd)
}
