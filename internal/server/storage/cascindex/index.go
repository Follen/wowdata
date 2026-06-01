package cascindex

import (
	"context"
	"database/sql"
	"errors"
)

const (
	StateValid = "valid"
	StateStale = "stale"
)

var ErrNotFound = errors.New("casc index: not found")

type RootMapping struct {
	FileDataID uint32
	ContentKey string
}

type EncodingMapping struct {
	ContentKey  string
	EncodingKey string
	Size        int64
}

type ArchiveMapping struct {
	EncodingKey string
	ArchiveKey  string
	Offset      int64
	Size        int64
}

type ArchiveSpan struct {
	FileDataID  uint32
	ContentKey  string
	EncodingKey string
	ArchiveKey  string
	Offset      int64
	Size        int64
}

func ReplaceIndex(ctx context.Context, db *sql.DB, sourceVersion string, roots []RootMapping, encodings []EncodingMapping, archives []ArchiveMapping) error {
	if db == nil {
		return errors.New("casc index: nil db")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := ensureSchema(ctx, tx); err != nil {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM server_casc_archive_entries`,
		`DELETE FROM server_casc_encoding_entries`,
		`DELETE FROM server_casc_root_entries`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO server_casc_source(id, source_version, state, updated_at)
		 VALUES (1, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(id) DO UPDATE SET source_version = excluded.source_version, state = excluded.state, updated_at = CURRENT_TIMESTAMP`,
		sourceVersion,
		StateValid,
	); err != nil {
		return err
	}

	rootStmt, err := tx.PrepareContext(ctx, `INSERT INTO server_casc_root_entries(file_data_id, content_key, source_version) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() {
		_ = rootStmt.Close()
	}()
	for _, mapping := range roots {
		if _, err := rootStmt.ExecContext(ctx, mapping.FileDataID, mapping.ContentKey, sourceVersion); err != nil {
			return err
		}
	}

	encodingStmt, err := tx.PrepareContext(ctx, `INSERT INTO server_casc_encoding_entries(content_key, encoding_key, size, source_version) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() {
		_ = encodingStmt.Close()
	}()
	for _, mapping := range encodings {
		if _, err := encodingStmt.ExecContext(ctx, mapping.ContentKey, mapping.EncodingKey, mapping.Size, sourceVersion); err != nil {
			return err
		}
	}

	archiveStmt, err := tx.PrepareContext(ctx, `INSERT INTO server_casc_archive_entries(encoding_key, archive_key, offset, size, source_version) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() {
		_ = archiveStmt.Close()
	}()
	for _, mapping := range archives {
		if _, err := archiveStmt.ExecContext(ctx, mapping.EncodingKey, mapping.ArchiveKey, mapping.Offset, mapping.Size, sourceVersion); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func ResolveFileDataID(ctx context.Context, db *sql.DB, fileDataID uint32) (ArchiveSpan, error) {
	if db == nil {
		return ArchiveSpan{}, errors.New("casc index: nil db")
	}
	if err := ensureSchema(ctx, db); err != nil {
		return ArchiveSpan{}, err
	}

	var span ArchiveSpan
	if err := db.QueryRowContext(ctx, `
		SELECT r.file_data_id, r.content_key, e.encoding_key, a.archive_key, a.offset, a.size
		  FROM server_casc_root_entries r
		  JOIN server_casc_encoding_entries e ON e.content_key = r.content_key
		  JOIN server_casc_archive_entries a ON a.encoding_key = e.encoding_key
		 WHERE r.file_data_id = ?`,
		fileDataID,
	).Scan(&span.FileDataID, &span.ContentKey, &span.EncodingKey, &span.ArchiveKey, &span.Offset, &span.Size); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ArchiveSpan{}, ErrNotFound
		}
		return ArchiveSpan{}, err
	}
	return span, nil
}

func UpsertSourceVersion(ctx context.Context, db *sql.DB, sourceVersion string) (bool, error) {
	if db == nil {
		return false, errors.New("casc index: nil db")
	}
	if err := ensureSchema(ctx, db); err != nil {
		return false, err
	}

	var current string
	err := db.QueryRowContext(ctx, `SELECT source_version FROM server_casc_source WHERE id = 1`).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := db.ExecContext(ctx, `INSERT INTO server_casc_source(id, source_version, state) VALUES (1, ?, ?)`, sourceVersion, StateStale); err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if current == sourceVersion {
		return false, nil
	}

	if _, err := db.ExecContext(
		ctx,
		`UPDATE server_casc_source SET source_version = ?, state = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1`,
		sourceVersion,
		StateStale,
	); err != nil {
		return false, err
	}
	return true, nil
}

func IndexState(ctx context.Context, db *sql.DB) (string, error) {
	if db == nil {
		return "", errors.New("casc index: nil db")
	}
	if err := ensureSchema(ctx, db); err != nil {
		return "", err
	}

	var state string
	err := db.QueryRowContext(ctx, `SELECT state FROM server_casc_source WHERE id = 1`).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return StateStale, nil
	}
	if err != nil {
		return "", err
	}
	return state, nil
}

type schemaExec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func ensureSchema(ctx context.Context, db schemaExec) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS server_casc_source (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			source_version TEXT NOT NULL,
			state TEXT NOT NULL CHECK (state IN ('valid', 'stale')),
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS server_casc_root_entries (
			file_data_id INTEGER PRIMARY KEY,
			content_key TEXT NOT NULL,
			source_version TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS server_casc_encoding_entries (
			content_key TEXT PRIMARY KEY,
			encoding_key TEXT NOT NULL,
			size INTEGER NOT NULL,
			source_version TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS server_casc_archive_entries (
			encoding_key TEXT PRIMARY KEY,
			archive_key TEXT NOT NULL,
			offset INTEGER NOT NULL,
			size INTEGER NOT NULL,
			source_version TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_root_content_key ON server_casc_root_entries(content_key)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_encoding_encoding_key ON server_casc_encoding_entries(encoding_key)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_archive_archive_key ON server_casc_archive_entries(archive_key)`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
