package metadata

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationsAreIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db, err := Open(dbPath, "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open first: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	db, err = Open(dbPath, "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open second: %v", err)
	}
	defer db.Close()

	var migrationCount int
	if err := db.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != 7 {
		t.Fatalf("schema_migrations count = %d, want 7", migrationCount)
	}

	for _, table := range []string{"products", "builds", "materialized_tables", "cache_audit"} {
		if !tableExists(t, db, table) {
			t.Fatalf("table %q does not exist", table)
		}
	}
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query table %q: %v", table, err)
	}
	return name == table
}
