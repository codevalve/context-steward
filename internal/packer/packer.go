package packer

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"context-steward/internal/config"
	"context-steward/internal/db"
)

// ResolveAuthority returns the authority level ("high", "medium", "low", "archival", "excluded") for a file path
func ResolveAuthority(dbConn *sql.DB, cfg *config.Config, path string) string {
	// 1. Check database overrides first if DB connection is active
	if dbConn != nil {
		auths, err := db.GetAllAuthority(dbConn)
		if err == nil {
			for _, a := range auths {
				if matchPattern(a.PathPattern, path) {
					return a.AuthorityLevel
				}
			}
		}
	}

	// 2. Check config file defaults
	defaults := cfg.Authority.Defaults
	for _, p := range defaults.High {
		if matchPattern(p, path) {
			return "high"
		}
	}
	for _, p := range defaults.Medium {
		if matchPattern(p, path) {
			return "medium"
		}
	}
	for _, p := range defaults.Low {
		if matchPattern(p, path) {
			return "low"
		}
	}
	for _, p := range defaults.Archival {
		if matchPattern(p, path) {
			return "archival"
		}
	}

	// 3. Defaults based on file type
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".md" || ext == ".txt" {
		return "medium"
	}

	return "low"
}

func matchPattern(pattern, path string) bool {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)

	// Direct match
	if pattern == path {
		return true
	}

	// Handle ** (recursive directory matching)
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) > 0 && parts[0] != "" {
			return strings.HasPrefix(path, parts[0])
		}
	}

	// Standard glob match
	match, err := filepath.Match(pattern, path)
	if err == nil && match {
		return true
	}

	// Check if base matches or path contains
	return strings.Contains(path, pattern) || filepath.Base(path) == pattern
}

// BuildSessionStartPacket generates a general-purpose overview of the workspace
func BuildSessionStartPacket(dbConn *sql.DB, cfg *config.Config, tokenizer Tokenizer, budget int) (string, error) {
	var sb strings.Builder

	sb.WriteString("# Context Packet: Session Start\n\n")
	sb.WriteString("Generated on: " + time.Now().Format("2006-01-02 15:04:05") + "\n\n")

	// 1. Project Purpose (Readme file contents if high authority)
	sb.WriteString("## Project Purpose & README\n\n")
	files, err := db.GetAllFiles(dbConn, false)
	if err == nil {
		var readmePath string
		for _, f := range files {
			base := strings.ToLower(filepath.Base(f.Path))
			if base == "readme.md" || base == "readme.txt" {
				readmePath = f.Path
				break
			}
		}

		if readmePath != "" {
			fullPath := filepath.Join(cfg.Workspace.Root, readmePath)
			content, err := os.ReadFile(fullPath)
			if err == nil {
				sb.WriteString("File: `" + readmePath + "`\n```markdown\n")
				sb.WriteString(string(content))
				sb.WriteString("\n```\n\n")
			} else {
				sb.WriteString("README file found at `" + readmePath + "` but could not be read.\n\n")
			}
		} else {
			sb.WriteString("No README file detected in the index. Update your root directory config if needed.\n\n")
		}
	}

	// 2. Active Decisions
	sb.WriteString("## Current Workspace Decisions\n\n")
	decisions, err := db.GetAllDecisions(dbConn)
	if err == nil && len(decisions) > 0 {
		for _, d := range decisions {
			sb.WriteString(fmt.Sprintf("### %s (%s authority)\n", d.Title, d.AuthorityLevel))
			sb.WriteString(fmt.Sprintf("**Decision**: %s\n\n", d.DecisionText))
			if d.Rationale != "" {
				sb.WriteString(fmt.Sprintf("**Rationale**: %s\n\n", d.Rationale))
			}
			if d.Consequences != "" {
				sb.WriteString(fmt.Sprintf("**Consequences**: %s\n\n", d.Consequences))
			}
			sb.WriteString(fmt.Sprintf("*Source: %s*\n\n", d.SourcePath))
		}
	} else {
		sb.WriteString("No recorded decisions found in workspace.\n\n")
	}

	// 3. Recent Changes
	sb.WriteString("## Recent Workspace Changes\n\n")
	recentThreshold := time.Now().Add(-72 * time.Hour) // Last 3 days
	var changedFiles []db.File
	if err == nil {
		for _, f := range files {
			if f.ModifiedAt.After(recentThreshold) && !f.Ignored {
				changedFiles = append(changedFiles, f)
			}
		}
	}
	if len(changedFiles) > 0 {
		for _, f := range changedFiles {
			sb.WriteString(fmt.Sprintf("- `%s` (modified: %s, size: %d bytes)\n", f.Path, f.ModifiedAt.Format("2006-01-02 15:04"), f.SizeBytes))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("No files modified in the last 72 hours.\n\n")
	}

	// 4. Staleness Warnings
	sb.WriteString("## Context Health\n\n")
	staleCount := 0
	if err == nil {
		for _, f := range files {
			s, err := db.GetSummaryByFileID(dbConn, f.ID)
			if err == nil && s != nil {
				if s.Stale || s.SourceHash != f.ContentHash {
					staleCount++
					sb.WriteString(fmt.Sprintf("- Warning: Summary for `%s` is stale (last compiled: %s).\n", f.Path, s.CreatedAt.Format("2006-01-02")))
				}
			}
		}
	}
	if staleCount == 0 {
		sb.WriteString("✓ All context summaries are fresh and matching current file hashes.\n\n")
	} else {
		sb.WriteString("\n*Recommended: Run `context-steward rebuild` to refresh stale summaries.*\n\n")
	}

	packetText := sb.String()

	// Truncate to budget if needed (very simple session start budget fallback)
	tokens := tokenizer.CountTokens(packetText)
	if tokens > budget {
		// Just a simple safety truncation banner
		packetText = packetText[:int(float64(budget)*4.2)] + "\n\n... [TRUNCATED DUE TO TOKEN BUDGET LIMIT] ..."
	}

	return packetText, nil
}

type RankedFile struct {
	File      db.File
	Summary   *db.Summary
	Authority string
	Score     float64
}

// BuildTaskSpecificPacket creates a focused context packet for a specific task prompt
func BuildTaskSpecificPacket(dbConn *sql.DB, cfg *config.Config, tokenizer Tokenizer, task string, budget int) (string, error) {
	// 1. Gather all files and summaries
	files, err := db.GetAllFiles(dbConn, false)
	if err != nil {
		return "", fmt.Errorf("failed to load files from db: %w", err)
	}

	// 2. Score relevance to the task query
	// Simple keyword match score
	queryTerms := strings.Fields(strings.ToLower(task))
	var candidates []RankedFile

	for _, f := range files {
		auth := ResolveAuthority(dbConn, cfg, f.Path)
		if auth == "excluded" {
			continue
		}

		s, err := db.GetSummaryByFileID(dbConn, f.ID)
		if err != nil {
			continue
		}

		// Calculate term match score
		score := 0.0
		pathLower := strings.ToLower(f.Path)
		var summaryLower string
		if s != nil {
			summaryLower = strings.ToLower(s.SummaryText)
		}

		for _, term := range queryTerms {
			if len(term) < 3 {
				continue // Skip small words
			}
			if strings.Contains(pathLower, term) {
				score += 5.0 // path match gets high priority
			}
			if s != nil && strings.Contains(summaryLower, term) {
				score += 2.0 // summary match
			}
		}

		if score == 0 {
			// If no direct keyword match, but it is high authority, give it a tiny baseline score so we can still consider it
			if auth == "high" {
				score = 0.5
			} else {
				continue // skip low/medium files with zero keyword overlaps
			}
		}

		// Multiply by authority rank
		authMult := 1.0
		switch auth {
		case "high":
			authMult = 3.0
		case "medium":
			authMult = 2.0
		case "low":
			authMult = 1.0
		case "archival":
			authMult = 0.5
		}
		score *= authMult

		candidates = append(candidates, RankedFile{
			File:      f,
			Summary:   s,
			Authority: auth,
			Score:     score,
		})
	}

	// Sort candidates by Score descending
	// Inline bubble sort or selection sort since candidates is typically small to medium
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Score > candidates[i].Score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// 3. Assemble components under budget
	var included []string
	var excluded []string
	var textBuilder strings.Builder

	textBuilder.WriteString("# Context Packet\n\n")
	textBuilder.WriteString("## Task\n" + task + "\n\n")
	textBuilder.WriteString(fmt.Sprintf("## Token Budget\nTarget Budget: %d tokens\n\n", budget))

	// Pre-assemble decisions to see how much budget they occupy (always included)
	var decBuilder strings.Builder
	decisions, err := db.GetAllDecisions(dbConn)
	if err == nil && len(decisions) > 0 {
		decBuilder.WriteString("## Relevant Decisions\n")
		for _, d := range decisions {
			decBuilder.WriteString(fmt.Sprintf("- **%s**: %s\n  *Rationale: %s*\n", d.Title, d.DecisionText, d.Rationale))
		}
		decBuilder.WriteString("\n")
	}

	decText := decBuilder.String()
	decTokens := tokenizer.CountTokens(decText)

	// Keep track of current budget usage
	currentTokens := tokenizer.CountTokens(textBuilder.String()) + decTokens
	
	var contextBuilder strings.Builder
	contextBuilder.WriteString("## Relevant Context\n\n")

	for _, c := range candidates {
		// Read full content if high authority, otherwise use summary
		var blockText string
		useFullContent := false

		if c.Authority == "high" {
			fullPath := filepath.Join(cfg.Workspace.Root, c.File.Path)
			content, err := os.ReadFile(fullPath)
			if err == nil {
				blockText = fmt.Sprintf("### File: `%s` (Authority: %s, Full Text)\n```\n%s\n```\n\n", c.File.Path, c.Authority, string(content))
				useFullContent = true
			}
		}

		// Fallback to summary if not high, or if full read failed
		if blockText == "" {
			summaryVal := "No summary generated. Run build first."
			if c.Summary != nil {
				summaryVal = c.Summary.SummaryText
			}
			blockText = fmt.Sprintf("### File Summary: `%s` (Authority: %s)\n%s\n\n", c.File.Path, c.Authority, summaryVal)
		}

		blockTokens := tokenizer.CountTokens(blockText)

		// Check if it fits in budget
		if currentTokens+blockTokens <= budget {
			contextBuilder.WriteString(blockText)
			currentTokens += blockTokens
			included = append(included, fmt.Sprintf("%s (%s, Score: %.1f, Full: %v)", c.File.Path, c.Authority, c.Score, useFullContent))
		} else if useFullContent {
			// If full content of High authority file didn't fit, try fitting just its summary!
			summaryVal := "No summary generated. Run build first."
			if c.Summary != nil {
				summaryVal = c.Summary.SummaryText
			}
			fallbackText := fmt.Sprintf("### File Summary: `%s` (Authority: %s - Full content exceeded budget)\n%s\n\n", c.File.Path, c.Authority, summaryVal)
			fallbackTokens := tokenizer.CountTokens(fallbackText)
			
			if currentTokens+fallbackTokens <= budget {
				contextBuilder.WriteString(fallbackText)
				currentTokens += fallbackTokens
				included = append(included, fmt.Sprintf("%s (%s, Score: %.1f, Summary Fallback)", c.File.Path, c.Authority, c.Score))
			} else {
				excluded = append(excluded, c.File.Path)
			}
		} else {
			excluded = append(excluded, c.File.Path)
		}
	}

	textBuilder.WriteString(contextBuilder.String())
	textBuilder.WriteString(decText)

	// Add excluded context section if there are any
	if len(excluded) > 0 {
		textBuilder.WriteString("## Excluded Context (Exceeded Budget)\n")
		for _, ex := range excluded {
			textBuilder.WriteString(fmt.Sprintf("- `%s`\n", ex))
		}
		textBuilder.WriteString("\n")
	}

	// Add sources section
	if cfg.Packets.IncludeSources && len(included) > 0 {
		textBuilder.WriteString("## Source References\n")
		for _, inc := range included {
			textBuilder.WriteString(fmt.Sprintf("- %s\n", inc))
		}
		textBuilder.WriteString("\n")
	}

	// Add estimation meta
	textBuilder.WriteString(fmt.Sprintf("<!-- estimated_tokens: %d -->\n", currentTokens))

	return textBuilder.String(), nil
}
