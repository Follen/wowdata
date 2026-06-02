package listfile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
)

type Entry struct {
	FileDataID uint32
	Path       string
	Extension  string
}

func ReplaceSource(ctx context.Context, db *sql.DB, sourceHash string, entries []Entry) error {
	if db == nil {
		return errors.New("listfile index: nil db")
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM server_listfile_entries`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM server_listfile_source`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO server_listfile_source(id, source_hash) VALUES (1, ?)`, sourceHash); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO server_listfile_entries(file_data_id, path, extension) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() {
		_ = stmt.Close()
	}()

	for _, entry := range entries {
		normalized := normalizeEntry(entry)
		if _, err := stmt.ExecContext(ctx, normalized.FileDataID, normalized.Path, normalized.Extension); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func SourceHash(ctx context.Context, db *sql.DB) (string, error) {
	if db == nil {
		return "", errors.New("listfile index: nil db")
	}
	if err := ensureSchema(ctx, db); err != nil {
		return "", err
	}
	var hash string
	if err := db.QueryRowContext(ctx, `SELECT source_hash FROM server_listfile_source WHERE id = 1`).Scan(&hash); err != nil {
		return "", err
	}
	return hash, nil
}

func LookupByFileDataID(ctx context.Context, db *sql.DB, id uint32) (Entry, error) {
	if db == nil {
		return Entry{}, errors.New("listfile index: nil db")
	}
	if err := ensureSchema(ctx, db); err != nil {
		return Entry{}, err
	}

	return queryEntry(ctx, db, `SELECT file_data_id, path, extension FROM server_listfile_entries WHERE file_data_id = ?`, id)
}

func LookupByFilename(ctx context.Context, db *sql.DB, filePath string) (Entry, error) {
	if db == nil {
		return Entry{}, errors.New("listfile index: nil db")
	}
	if err := ensureSchema(ctx, db); err != nil {
		return Entry{}, err
	}

	return queryEntry(ctx, db, `SELECT file_data_id, path, extension FROM server_listfile_entries WHERE path = ?`, normalizePath(filePath))
}

func SearchPath(ctx context.Context, db *sql.DB, contains string, limit int) ([]Entry, error) {
	if db == nil {
		return nil, errors.New("listfile index: nil db")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("listfile index: invalid limit %d", limit)
	}
	if err := ensureSchema(ctx, db); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT file_data_id, path, extension
		   FROM server_listfile_entries
		  WHERE path LIKE ? ESCAPE '\'
		  ORDER BY path ASC, file_data_id ASC
		  LIMIT ?`,
		"%"+escapeLike(normalizePath(contains))+"%",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	return scanEntries(rows)
}

func SearchExtension(ctx context.Context, db *sql.DB, ext string, limit int) ([]Entry, error) {
	if db == nil {
		return nil, errors.New("listfile index: nil db")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("listfile index: invalid limit %d", limit)
	}
	if err := ensureSchema(ctx, db); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT file_data_id, path, extension
		   FROM server_listfile_entries
		  WHERE extension = ?
		  ORDER BY path ASC, file_data_id ASC
		  LIMIT ?`,
		normalizeExtension(ext),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	return scanEntries(rows)
}

type schemaExec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type entryQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ensureSchema(ctx context.Context, db schemaExec) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS server_listfile_source (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			source_hash TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS server_listfile_entries (
			file_data_id INTEGER PRIMARY KEY,
			path TEXT NOT NULL UNIQUE,
			extension TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_server_listfile_entries_path ON server_listfile_entries(path)`,
		`CREATE INDEX IF NOT EXISTS idx_server_listfile_entries_extension_path ON server_listfile_entries(extension, path)`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func queryEntry(ctx context.Context, db entryQueryer, query string, args ...any) (Entry, error) {
	var entry Entry
	var id uint32
	if err := db.QueryRowContext(ctx, query, args...).Scan(&id, &entry.Path, &entry.Extension); err != nil {
		return Entry{}, err
	}
	entry.FileDataID = id
	return entry, nil
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var entries []Entry
	for rows.Next() {
		var entry Entry
		var id uint32
		if err := rows.Scan(&id, &entry.Path, &entry.Extension); err != nil {
			return nil, err
		}
		entry.FileDataID = id
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func normalizeEntry(entry Entry) Entry {
	entry.Path = normalizePath(entry.Path)
	entry.Extension = normalizeExtension(entry.Extension)
	if entry.Extension == "" {
		entry.Extension = normalizeExtension(path.Ext(entry.Path))
	}
	return entry
}

func normalizePath(filePath string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(filePath), "\\", "/"))
}

func normalizeExtension(ext string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}
