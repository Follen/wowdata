package metadata

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	StateValid     = "valid"
	StateStale     = "stale"
	StatePreparing = "preparing"
	StateFailed    = "failed"
	StateNoBuild   = "no_build"
)

var ErrBuildNotReady = errors.New("build is not ready")

func Open(path string) (*sql.DB, error) {
	dir, err := migrationsDir()
	if err != nil {
		return nil, err
	}
	return OpenWithMigrations(path, dir)
}

func OpenWithMigrations(path string, migrationsDir string) (*sql.DB, error) {
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, err
			}
		}
	}

	db, err := sql.Open("sqlite", sqliteOpenDSN(path))
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(4)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 120000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(db, migrationsDir); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func sqliteOpenDSN(path string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout=120000")
	q.Add("_pragma", "foreign_keys=ON")
	if path != ":memory:" {
		q.Add("_pragma", "journal_mode=WAL")
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + q.Encode()
}

func migrate(db *sql.DB, dir string) error {
	if err := ensureSchemaMigrations(db); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, name).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		apply, err := shouldApplyMigration(db, name)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if apply {
			if _, err := tx.Exec(string(body)); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply migration %s: %w", name, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

func shouldApplyMigration(db *sql.DB, name string) (bool, error) {
	if name != "0009_no_build_state.sql" {
		return true, nil
	}
	return tableExists(db, "server_builds")
}

func ensureSchemaMigrations(db *sql.DB) error {
	columns, err := tableColumns(db, "schema_migrations")
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		_, err := db.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`)
		return err
	}
	if columns["version"] {
		return nil
	}
	if !columns["name"] {
		return errors.New("schema_migrations table has no version or name column")
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.Exec(`ALTER TABLE schema_migrations RENAME TO schema_migrations_legacy_name`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

func tableExists(db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func migrationsDir() (string, error) {
	var roots []string
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(executable))
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		roots = append(roots, filepath.Dir(file))
	}

	seen := map[string]bool{}
	for _, root := range roots {
		for dir := root; ; dir = filepath.Dir(dir) {
			if !seen[dir] {
				seen[dir] = true
				candidate := filepath.Join(dir, "migrations", "server")
				if info, err := os.Stat(candidate); err == nil && info.IsDir() {
					return candidate, nil
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", errors.New("migrations/server directory not found")
}
