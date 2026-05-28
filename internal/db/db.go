package db

import (
	"database/sql"
	"time"
)

type File struct {
	ID          int64
	Path        string
	FileType    string
	ContentHash string
	SizeBytes   int64
	ModifiedAt  time.Time
	ScannedAt   time.Time
	Ignored     bool
}

type Summary struct {
	ID          int64
	FileID      int64
	SummaryText string
	SummaryKind string
	ModelName   string
	SourceHash  string
	CreatedAt   time.Time
	Stale       bool
}

type Decision struct {
	ID             int64
	Title          string
	DecisionText   string
	Rationale      string
	Consequences   string
	AuthorityLevel string
	CreatedAt      time.Time
	SourcePath     string
}

type Packet struct {
	ID          int64
	PacketType  string
	Task        string
	TokenBudget int
	PacketText  string
	CreatedAt   time.Time
	Stale       bool
}

type Authority struct {
	ID             int64
	PathPattern    string
	AuthorityLevel string
	Reason         string
	ReviewedAt     time.Time
}

type Handoff struct {
	ID               int64
	SourcePath       string
	ExtractedSummary string
	CreatedAt        time.Time
	Reviewed         bool
}

// UpsertFile inserts or updates file metadata
func UpsertFile(db *sql.DB, f *File) error {
	query := `
	INSERT INTO files (path, file_type, content_hash, size_bytes, modified_at, scanned_at, ignored)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(path) DO UPDATE SET
		file_type = excluded.file_type,
		content_hash = excluded.content_hash,
		size_bytes = excluded.size_bytes,
		modified_at = excluded.modified_at,
		scanned_at = excluded.scanned_at,
		ignored = excluded.ignored
	`
	res, err := db.Exec(query, f.Path, f.FileType, f.ContentHash, f.SizeBytes, f.ModifiedAt, f.ScannedAt, f.Ignored)
	if err != nil {
		return err
	}
	if f.ID == 0 {
		id, err := res.LastInsertId()
		if err == nil {
			f.ID = id
		}
	}
	return nil
}

// GetFileByPath retrieves file metadata by its relative path
func GetFileByPath(db *sql.DB, path string) (*File, error) {
	query := `SELECT id, path, file_type, content_hash, size_bytes, modified_at, scanned_at, ignored FROM files WHERE path = ?`
	row := db.QueryRow(query, path)
	var f File
	err := row.Scan(&f.ID, &f.Path, &f.FileType, &f.ContentHash, &f.SizeBytes, &f.ModifiedAt, &f.ScannedAt, &f.Ignored)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// DeleteFile deletes a file record by path
func DeleteFile(db *sql.DB, path string) error {
	_, err := db.Exec(`DELETE FROM files WHERE path = ?`, path)
	return err
}

// DeleteFilesNotScannedSince removes files from the DB that were not found in the current scan
func DeleteFilesNotScannedSince(db *sql.DB, threshold time.Time) (int64, error) {
	res, err := db.Exec(`DELETE FROM files WHERE scanned_at < ?`, threshold)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetAllFiles retrieves all file records
func GetAllFiles(db *sql.DB, includeIgnored bool) ([]File, error) {
	query := `SELECT id, path, file_type, content_hash, size_bytes, modified_at, scanned_at, ignored FROM files`
	if !includeIgnored {
		query += ` WHERE ignored = 0`
	}
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.Path, &f.FileType, &f.ContentHash, &f.SizeBytes, &f.ModifiedAt, &f.ScannedAt, &f.Ignored); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// UpsertSummary inserts or updates file summary
func UpsertSummary(db *sql.DB, s *Summary) error {
	query := `
	INSERT INTO summaries (file_id, summary_text, summary_kind, model_name, source_hash, created_at, stale)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(file_id) DO UPDATE SET
		summary_text = excluded.summary_text,
		summary_kind = excluded.summary_kind,
		model_name = excluded.model_name,
		source_hash = excluded.source_hash,
		created_at = excluded.created_at,
		stale = excluded.stale
	`
	// Wait, we need summaries table to have a UNIQUE constraint on file_id for ON CONFLICT(file_id) to work!
	// Let's modify migration to have UNIQUE(file_id) or check if we can do custom upsert.
	// Let's check how summaries table was defined. Oh, we defined `file_id INTEGER NOT NULL`.
	// Let's alter table to make file_id UNIQUE, or we can just delete previous summary and insert.
	// Deleting and inserting is extremely robust and doesn't require UNIQUE index updates. Let's do that!
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM summaries WHERE file_id = ?`, s.FileID)
	if err != nil {
		return err
	}

	query = `INSERT INTO summaries (file_id, summary_text, summary_kind, model_name, source_hash, created_at, stale) VALUES (?, ?, ?, ?, ?, ?, ?)`
	res, err := tx.Exec(query, s.FileID, s.SummaryText, s.SummaryKind, s.ModelName, s.SourceHash, s.CreatedAt, s.Stale)
	if err != nil {
		return err
	}

	id, err := res.LastInsertId()
	if err == nil {
		s.ID = id
	}

	return tx.Commit()
}

// GetSummaryByFileID retrieves the summary for a file
func GetSummaryByFileID(db *sql.DB, fileID int64) (*Summary, error) {
	query := `SELECT id, file_id, summary_text, summary_kind, model_name, source_hash, created_at, stale FROM summaries WHERE file_id = ?`
	row := db.QueryRow(query, fileID)
	var s Summary
	err := row.Scan(&s.ID, &s.FileID, &s.SummaryText, &s.SummaryKind, &s.ModelName, &s.SourceHash, &s.CreatedAt, &s.Stale)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpsertDecision inserts or updates a decision
func UpsertDecision(db *sql.DB, d *Decision) error {
	query := `
	INSERT INTO decisions (title, decision_text, rationale, consequences, authority_level, created_at, source_path)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(source_path) DO UPDATE SET
		title = excluded.title,
		decision_text = excluded.decision_text,
		rationale = excluded.rationale,
		consequences = excluded.consequences,
		authority_level = excluded.authority_level,
		created_at = excluded.created_at
	`
	// decisions table needs UNIQUE(source_path) for this to work. We set UNIQUE in schema!
	res, err := db.Exec(query, d.Title, d.DecisionText, d.Rationale, d.Consequences, d.AuthorityLevel, d.CreatedAt, d.SourcePath)
	if err != nil {
		return err
	}
	if d.ID == 0 {
		id, err := res.LastInsertId()
		if err == nil {
			d.ID = id
		}
	}
	return nil
}

// GetAllDecisions retrieves all decisions
func GetAllDecisions(db *sql.DB) ([]Decision, error) {
	rows, err := db.Query(`SELECT id, title, decision_text, rationale, consequences, authority_level, created_at, source_path FROM decisions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []Decision
	for rows.Next() {
		var d Decision
		var rationale, consequences sql.NullString
		if err := rows.Scan(&d.ID, &d.Title, &d.DecisionText, &rationale, &consequences, &d.AuthorityLevel, &d.CreatedAt, &d.SourcePath); err != nil {
			return nil, err
		}
		d.Rationale = rationale.String
		d.Consequences = consequences.String
		decisions = append(decisions, d)
	}
	return decisions, nil
}

// DeleteDecisionByPath deletes a decision record by its source file path
func DeleteDecisionByPath(db *sql.DB, path string) error {
	_, err := db.Exec(`DELETE FROM decisions WHERE source_path = ?`, path)
	return err
}

// UpsertAuthority inserts or updates authority configuration reviews
func UpsertAuthority(db *sql.DB, a *Authority) error {
	query := `
	INSERT INTO authority (path_pattern, authority_level, reason, reviewed_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(path_pattern) DO UPDATE SET
		authority_level = excluded.authority_level,
		reason = excluded.reason,
		reviewed_at = excluded.reviewed_at
	`
	res, err := db.Exec(query, a.PathPattern, a.AuthorityLevel, a.Reason, a.ReviewedAt)
	if err != nil {
		return err
	}
	if a.ID == 0 {
		id, err := res.LastInsertId()
		if err == nil {
			a.ID = id
		}
	}
	return nil
}

// GetAllAuthority retrieves all custom authority configurations
func GetAllAuthority(db *sql.DB) ([]Authority, error) {
	rows, err := db.Query(`SELECT id, path_pattern, authority_level, reason, reviewed_at FROM authority`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var auths []Authority
	for rows.Next() {
		var a Authority
		var reason sql.NullString
		if err := rows.Scan(&a.ID, &a.PathPattern, &a.AuthorityLevel, &reason, &a.ReviewedAt); err != nil {
			return nil, err
		}
		a.Reason = reason.String
		auths = append(auths, a)
	}
	return auths, nil
}

// SavePacket saves a generated packet
func SavePacket(db *sql.DB, p *Packet) error {
	query := `INSERT INTO packets (packet_type, task, token_budget, packet_text, created_at, stale) VALUES (?, ?, ?, ?, ?, ?)`
	res, err := db.Exec(query, p.PacketType, p.Task, p.TokenBudget, p.PacketText, p.CreatedAt, p.Stale)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		p.ID = id
	}
	return nil
}

// IngestHandoff inserts or updates handoff tracking
func IngestHandoff(db *sql.DB, h *Handoff) error {
	query := `
	INSERT INTO handoffs (source_path, extracted_summary, created_at, reviewed)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(source_path) DO UPDATE SET
		extracted_summary = excluded.extracted_summary,
		created_at = excluded.created_at,
		reviewed = excluded.reviewed
	`
	res, err := db.Exec(query, h.SourcePath, h.ExtractedSummary, h.CreatedAt, h.Reviewed)
	if err != nil {
		return err
	}
	if h.ID == 0 {
		id, err := res.LastInsertId()
		if err == nil {
			h.ID = id
		}
	}
	return nil
}
