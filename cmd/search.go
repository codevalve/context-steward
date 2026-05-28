package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"context-steward/internal/config"
	"context-steward/internal/db"
	"context-steward/internal/packer"
)

type SearchResult struct {
	Path           string `json:"path"`
	SummaryExcerpt string `json:"summary_excerpt"`
	AuthorityLevel string `json:"authority_level"`
	IsStale        bool   `json:"is_stale"`
	MatchReason    string `json:"match_reason"`
}

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search indexed summaries and files",
	Long:  `Perform keyword searches across indexed file paths and their summaries. Displays authority rank and match reasons.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		queryText := args[0]

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
			return fmt.Errorf("failed to open SQLite database: %w", err)
		}
		defer dbConn.Close()

		// Perform simple LIKE search
		likePattern := "%" + queryText + "%"
		sqlQuery := `
		SELECT f.path, s.summary_text, s.stale
		FROM files f
		LEFT JOIN summaries s ON f.id = s.file_id
		WHERE f.ignored = 0 AND (f.path LIKE ? OR s.summary_text LIKE ?)
		`
		rows, err := dbConn.Query(sqlQuery, likePattern, likePattern)
		if err != nil {
			return fmt.Errorf("failed to execute search query: %w", err)
		}
		defer rows.Close()

		var results []SearchResult
		for rows.Next() {
			var path string
			var summaryText sql.NullString
			var isStale sql.NullBool

			if err := rows.Scan(&path, &summaryText, &isStale); err != nil {
				return fmt.Errorf("failed to scan search result row: %w", err)
			}

			// Resolve authority level
			auth := packer.ResolveAuthority(dbConn, cfg, path)

			// Determine match reason
			reason := ""
			if strings.Contains(strings.ToLower(path), strings.ToLower(queryText)) {
				reason = "File path matched"
			}
			if summaryText.Valid && strings.Contains(strings.ToLower(summaryText.String), strings.ToLower(queryText)) {
				if reason != "" {
					reason += " & summary matched"
				} else {
					reason = "Summary content matched"
				}
			}

			// Excerpt the summary
			excerpt := ""
			if summaryText.Valid {
				excerpt = summaryText.String
				excerptClean := strings.ReplaceAll(excerpt, "\n", " ")
				if len(excerptClean) > 120 {
					excerpt = excerptClean[:120] + "..."
				} else {
					excerpt = excerptClean
				}
			} else {
				excerpt = "(No summary generated yet)"
			}

			results = append(results, SearchResult{
				Path:           path,
				SummaryExcerpt: excerpt,
				AuthorityLevel: auth,
				IsStale:        isStale.Bool,
				MatchReason:    reason,
			})
		}

		if jsonOut {
			data, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		if len(results) == 0 {
			if !quiet {
				fmt.Println("No matches found in the workspace index.")
			}
			return nil
		}

		// Lip Gloss styling for terminal output
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")) // Cyanish-blue
		metaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))           // Grey
		staleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208")) // Orange-yellow
		matchStyle := lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("41")) // Green

		for _, r := range results {
			fmt.Printf("%s\n", titleStyle.Render(r.Path))
			
			authTag := fmt.Sprintf("[%s authority]", r.AuthorityLevel)
			staleTag := ""
			if r.IsStale {
				staleTag = staleStyle.Render(" (STALE)")
			}

			fmt.Printf("  %s%s | %s\n", metaStyle.Render(authTag), staleTag, matchStyle.Render(r.MatchReason))
			fmt.Printf("  Excerpt: %s\n\n", r.SummaryExcerpt)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
}
