package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// InitDB initializes the SQLite database at the specified path and runs migrations/schema setup
func InitDB(dbPath string) (*sql.DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Create tables
	if err := createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return db, nil
}

func createTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT UNIQUE NOT NULL,
			file_type TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			modified_at TIMESTAMP NOT NULL,
			scanned_at TIMESTAMP NOT NULL,
			ignored BOOLEAN NOT NULL DEFAULT 0
		);`,

		`CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id INTEGER NOT NULL,
			summary_text TEXT NOT NULL,
			summary_kind TEXT NOT NULL,
			model_name TEXT NOT NULL,
			source_hash TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			stale BOOLEAN NOT NULL DEFAULT 0,
			FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE
		);`,

		`CREATE TABLE IF NOT EXISTS decisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			decision_text TEXT NOT NULL,
			rationale TEXT,
			consequences TEXT,
			authority_level TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			source_path TEXT UNIQUE NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS packets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			packet_type TEXT NOT NULL,
			task TEXT NOT NULL,
			token_budget INTEGER NOT NULL,
			packet_text TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			stale BOOLEAN NOT NULL DEFAULT 0
		);`,

		`CREATE TABLE IF NOT EXISTS authority (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path_pattern TEXT UNIQUE NOT NULL,
			authority_level TEXT NOT NULL,
			reason TEXT,
			reviewed_at TIMESTAMP NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS handoffs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_path TEXT UNIQUE NOT NULL,
			extracted_summary TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			reviewed BOOLEAN NOT NULL DEFAULT 0
		);`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}

	return nil
}
