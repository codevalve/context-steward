package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"context-steward/internal/config"
	"context-steward/internal/db"
	"context-steward/internal/scanner"
)

var (
	decisionTitle        string
	decisionText         string
	decisionRationale    string
	decisionConsequences string
)

var decisionCmd = &cobra.Command{
	Use:   "decision",
	Short: "Manage workspace decisions",
}

var decisionAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Record a new project decision",
	Long:  `Record a durable decision in the workspace. Writes a Markdown file and registers the decision in the database.`,
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

		// Check if we need interactive inputs
		isInteractive := decisionTitle == "" && decisionText == ""

		if isInteractive {
			if quiet || jsonOut {
				return fmt.Errorf("interactive prompt cannot run in quiet/JSON mode; please supply --title and --decision flags")
			}

			form := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Decision Title").
						Description("Short summary of the decision").
						Value(&decisionTitle).
						Validate(func(str string) error {
							if len(str) == 0 {
								return fmt.Errorf("title cannot be empty")
							}
							return nil
						}),
					huh.NewText().
						Title("The Decision").
						Description("What was decided?").
						Value(&decisionText).
						Validate(func(str string) error {
							if len(str) == 0 {
								return fmt.Errorf("decision details cannot be empty")
							}
							return nil
						}),
					huh.NewText().
						Title("Rationale").
						Description("Why was this decision made?").
						Value(&decisionRationale),
					huh.NewText().
						Title("Consequences").
						Description("What are the outcomes, effects, or next steps?").
						Value(&decisionConsequences),
				),
			)

			if err := form.Run(); err != nil {
				return err
			}
		} else {
			if decisionTitle == "" {
				return fmt.Errorf("missing decision --title")
			}
			if decisionText == "" {
				return fmt.Errorf("missing --decision details")
			}
		}

		// Ensure decisions directory exists
		decisionsDir := filepath.Join(wsPath, "decisions")
		if !dryRun {
			if err := os.MkdirAll(decisionsDir, 0755); err != nil {
				return fmt.Errorf("failed to create decisions directory: %w", err)
			}
		}

		// Determine sequence number
		seq := 1
		files, err := os.ReadDir(decisionsDir)
		if err == nil {
			for _, f := range files {
				if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
					seq++
				}
			}
		}

		slug := slugify(decisionTitle)
		fileName := fmt.Sprintf("%04d-%s.md", seq, slug)
		relPath := filepath.Join("decisions", fileName)
		fullPath := filepath.Join(wsPath, relPath)

		// Create markdown content
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
`, decisionTitle, now.Format("2006-01-02"), decisionText, decisionRationale, decisionConsequences)

		if !dryRun {
			err = os.WriteFile(fullPath, []byte(mdContent), 0644)
			if err != nil {
				return fmt.Errorf("failed to write decision markdown file: %w", err)
			}
		}

		// Open DB and save record
		dbConn, err := db.InitDB(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer dbConn.Close()

		d := db.Decision{
			Title:          decisionTitle,
			DecisionText:   decisionText,
			Rationale:      decisionRationale,
			Consequences:   decisionConsequences,
			AuthorityLevel: "high", // Decisions are always high authority
			CreatedAt:      now,
			SourcePath:     relPath,
		}

		if !dryRun {
			if err := db.UpsertDecision(dbConn, &d); err != nil {
				return fmt.Errorf("failed to save decision in database: %w", err)
			}
			
			// Auto-register and index the decision file in the workspace context tables
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

			// Pre-generate summary text for immediate packet assembly matching
			sText := fmt.Sprintf("Decision: %s. Details: %s. Rationale: %s.", decisionTitle, decisionText, decisionRationale)
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
		}

		if !quiet {
			fmt.Printf("Decision recorded successfully:\n  File:  %s\n  Title: %s\n", relPath, decisionTitle)
		}

		return nil
	},
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	
	// Replace non-alphanumeric with dash
	reg, _ := regexp.Compile("[^a-z0-9-]+")
	s = reg.ReplaceAllString(s, "")
	
	// Remove duplicate dashes
	reg2, _ := regexp.Compile("-+")
	s = reg2.ReplaceAllString(s, "-")
	
	return strings.Trim(s, "-")
}

func init() {
	decisionAddCmd.Flags().StringVar(&decisionTitle, "title", "", "title of the decision")
	decisionAddCmd.Flags().StringVar(&decisionText, "decision", "", "what was decided")
	decisionAddCmd.Flags().StringVar(&decisionRationale, "rationale", "", "rationale behind the decision")
	decisionAddCmd.Flags().StringVar(&decisionConsequences, "consequences", "", "consequences/outcomes of the decision")
	decisionCmd.AddCommand(decisionAddCmd)
	rootCmd.AddCommand(decisionCmd)
}
