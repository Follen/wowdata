package cascindex

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

type SourceKey struct {
	Region   string
	Product  string
	Locale   string
	BuildKey string
}

func ReplaceIndex(ctx context.Context, db *sql.DB, sourceVersion string, roots []RootMapping, encodings []EncodingMapping, archives []ArchiveMapping) error {
	return ReplaceIndexForSource(ctx, db, SourceKey{}, sourceVersion, roots, encodings, archives)
}

func ReplaceIndexForSource(ctx context.Context, db *sql.DB, source SourceKey, sourceVersion string, roots []RootMapping, encodings []EncodingMapping, archives []ArchiveMapping) error {
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
		`DELETE FROM server_casc_archive_entries WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		`DELETE FROM server_casc_encoding_entries WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		`DELETE FROM server_casc_root_entries WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, source.Region, source.Product, source.Locale, source.BuildKey); err != nil {
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

	rootStmt, err := tx.PrepareContext(ctx, `INSERT INTO server_casc_root_entries(region, product, locale, build_key, file_data_id, content_key, source_version) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() {
		_ = rootStmt.Close()
	}()
	for _, mapping := range roots {
		if _, err := rootStmt.ExecContext(ctx, source.Region, source.Product, source.Locale, source.BuildKey, mapping.FileDataID, mapping.ContentKey, sourceVersion); err != nil {
			return err
		}
	}

	encodingStmt, err := tx.PrepareContext(ctx, `INSERT INTO server_casc_encoding_entries(region, product, locale, build_key, content_key, encoding_key, size, source_version) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() {
		_ = encodingStmt.Close()
	}()
	for _, mapping := range encodings {
		if _, err := encodingStmt.ExecContext(ctx, source.Region, source.Product, source.Locale, source.BuildKey, mapping.ContentKey, mapping.EncodingKey, mapping.Size, sourceVersion); err != nil {
			return err
		}
	}

	archiveStmt, err := tx.PrepareContext(ctx, `INSERT INTO server_casc_archive_entries(region, product, locale, build_key, encoding_key, archive_key, offset, size, source_version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() {
		_ = archiveStmt.Close()
	}()
	for _, mapping := range archives {
		if _, err := archiveStmt.ExecContext(ctx, source.Region, source.Product, source.Locale, source.BuildKey, mapping.EncodingKey, mapping.ArchiveKey, mapping.Offset, mapping.Size, sourceVersion); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func ResolveFileDataID(ctx context.Context, db *sql.DB, fileDataID uint32) (ArchiveSpan, error) {
	return ResolveFileDataIDForSource(ctx, db, SourceKey{}, fileDataID)
}

func HasUsableIndexForSource(ctx context.Context, db *sql.DB, source SourceKey) (bool, error) {
	if db == nil {
		return false, errors.New("casc index: nil db")
	}
	if err := ensureSchema(ctx, db); err != nil {
		return false, err
	}

	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM server_casc_source s
		 WHERE s.id = 1
		   AND s.state = ?
		   AND EXISTS (
		       SELECT 1
		         FROM server_casc_root_entries r
		         JOIN server_casc_encoding_entries e
		           ON e.region = r.region AND e.product = r.product AND e.locale = r.locale AND e.build_key = r.build_key
		          AND e.source_version = r.source_version AND e.content_key = r.content_key
		         JOIN server_casc_archive_entries a
		           ON a.region = e.region AND a.product = e.product AND a.locale = e.locale AND a.build_key = e.build_key
		          AND a.source_version = e.source_version AND a.encoding_key = e.encoding_key
		        WHERE r.region = ? AND r.product = ? AND r.locale = ? AND r.build_key = ?
		        LIMIT 1
		   )`,
		StateValid,
		source.Region, source.Product, source.Locale, source.BuildKey,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func ResolveFileDataIDForSource(ctx context.Context, db *sql.DB, source SourceKey, fileDataID uint32) (ArchiveSpan, error) {
	spans, err := ResolveFileDataIDSpansForSource(ctx, db, source, fileDataID)
	if err != nil {
		return ArchiveSpan{}, err
	}
	return spans[0], nil
}

func ResolveFileDataIDSpans(ctx context.Context, db *sql.DB, fileDataID uint32) ([]ArchiveSpan, error) {
	return ResolveFileDataIDSpansForSource(ctx, db, SourceKey{}, fileDataID)
}

func ResolveFileDataIDSpansForSource(ctx context.Context, db *sql.DB, source SourceKey, fileDataID uint32) ([]ArchiveSpan, error) {
	if db == nil {
		return nil, errors.New("casc index: nil db")
	}
	if err := ensureSchema(ctx, db); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT r.file_data_id, r.content_key, e.encoding_key, a.archive_key, a.offset, a.size
		  FROM server_casc_root_entries r
		  JOIN server_casc_encoding_entries e
		    ON e.region = r.region AND e.product = r.product AND e.locale = r.locale AND e.build_key = r.build_key
		   AND e.source_version = r.source_version AND e.content_key = r.content_key
		  JOIN server_casc_archive_entries a
		    ON a.region = e.region AND a.product = e.product AND a.locale = e.locale AND a.build_key = e.build_key
		   AND a.source_version = e.source_version AND a.encoding_key = e.encoding_key
		 WHERE r.region = ? AND r.product = ? AND r.locale = ? AND r.build_key = ?
		   AND r.file_data_id = ?
		 ORDER BY r.content_key ASC, e.encoding_key ASC, a.archive_key ASC, a.offset ASC, a.size ASC`,
		source.Region, source.Product, source.Locale, source.BuildKey, fileDataID,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var spans []ArchiveSpan
	for rows.Next() {
		var span ArchiveSpan
		if err := rows.Scan(&span.FileDataID, &span.ContentKey, &span.EncodingKey, &span.ArchiveKey, &span.Offset, &span.Size); err != nil {
			return nil, err
		}
		spans = append(spans, span)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return nil, ErrNotFound
	}
	return spans, nil
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
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			region TEXT NOT NULL DEFAULT '',
			product TEXT NOT NULL DEFAULT '',
			locale TEXT NOT NULL DEFAULT '',
			build_key TEXT NOT NULL DEFAULT '',
			file_data_id INTEGER NOT NULL,
			content_key TEXT NOT NULL,
			source_version TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS server_casc_encoding_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			region TEXT NOT NULL DEFAULT '',
			product TEXT NOT NULL DEFAULT '',
			locale TEXT NOT NULL DEFAULT '',
			build_key TEXT NOT NULL DEFAULT '',
			content_key TEXT NOT NULL,
			encoding_key TEXT NOT NULL,
			size INTEGER NOT NULL,
			source_version TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS server_casc_archive_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			region TEXT NOT NULL DEFAULT '',
			product TEXT NOT NULL DEFAULT '',
			locale TEXT NOT NULL DEFAULT '',
			build_key TEXT NOT NULL DEFAULT '',
			encoding_key TEXT NOT NULL,
			archive_key TEXT NOT NULL,
			offset INTEGER NOT NULL,
			size INTEGER NOT NULL,
			source_version TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_root_file_data_id ON server_casc_root_entries(file_data_id)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_root_content_key ON server_casc_root_entries(content_key)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_encoding_content_key ON server_casc_encoding_entries(content_key)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_encoding_encoding_key ON server_casc_encoding_entries(encoding_key)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_archive_encoding_key ON server_casc_archive_entries(encoding_key)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_archive_archive_key ON server_casc_archive_entries(archive_key)`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, table := range []string{"server_casc_root_entries", "server_casc_encoding_entries", "server_casc_archive_entries"} {
		for _, column := range []string{"region", "product", "locale", "build_key"} {
			if err := addTextColumnIfMissing(ctx, db, table, column); err != nil {
				return err
			}
		}
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_server_casc_root_source_file_data_id ON server_casc_root_entries(region, product, locale, build_key, file_data_id)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_encoding_source_content_key ON server_casc_encoding_entries(region, product, locale, build_key, source_version, content_key)`,
		`CREATE INDEX IF NOT EXISTS idx_server_casc_archive_source_encoding_key ON server_casc_archive_entries(region, product, locale, build_key, source_version, encoding_key)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func addTextColumnIfMissing(ctx context.Context, db schemaExec, table string, column string) error {
	_, err := db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` TEXT NOT NULL DEFAULT ''`)
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "duplicate column name") {
		return nil
	}
	return err
}
