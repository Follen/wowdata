package metadata

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenWithMigrationsUsesExplicitRuntimePath(t *testing.T) {
	migrationsDir := filepath.Join(t.TempDir(), "migrations", "server")
	copyServerMigrations(t, migrationsDir)

	db, err := OpenWithMigrations(filepath.Join(t.TempDir(), "metadata.sqlite"), migrationsDir)
	if err != nil {
		t.Fatalf("open with explicit runtime migrations: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'server_builds'`).Scan(&name)
	if err != nil {
		t.Fatalf("server_builds missing after explicit migrations open: %v", err)
	}
	if name != "server_builds" {
		t.Fatalf("table name = %q, want server_builds", name)
	}
}

func TestMigrationsCreateRequiredTables(t *testing.T) {
	path := dbPath(t)
	db := reopenTestDB(t, path)
	t.Cleanup(func() { _ = db.Close() })

	wantTables := []string{
		"schema_migrations",
		"server_builds",
		"server_materialized_tables",
		"server_listfile_sources",
		"server_casc_sources",
		"server_artifacts",
		"server_refresh_runs",
	}
	for _, table := range wantTables {
		t.Run(table, func(t *testing.T) {
			var name string
			err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
			if err != nil {
				t.Fatalf("table %s missing: %v", table, err)
			}
			if name != table {
				t.Fatalf("table name = %q, want %q", name, table)
			}
		})
	}

	var applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied != 7 {
		t.Fatalf("applied migrations = %d, want 7", applied)
	}

	db.Close()
	db = reopenTestDB(t, path)
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count migrations after reopen: %v", err)
	}
	if applied != 7 {
		t.Fatalf("applied migrations after reopen = %d, want 7", applied)
	}

	artifact := Artifact{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1",
		Path:        "icons/spell.png",
		DownloadURL: "http://example.test/files/icons/spell.png",
		MIMEType:    "image/png",
		Size:        128,
		SHA256:      "sha256-a",
	}
	if err := UpsertArtifact(context.Background(), db, artifact); err != nil {
		t.Fatalf("upsert artifact: %v", err)
	}
	var artifactPath, artifactHash string
	if err := db.QueryRow(`SELECT artifact_path, sha256 FROM server_artifacts WHERE artifact_path = ?`, artifact.Path).Scan(&artifactPath, &artifactHash); err != nil {
		t.Fatalf("query artifact: %v", err)
	}
	if artifactPath != artifact.Path || artifactHash != artifact.SHA256 {
		t.Fatalf("artifact row = %q/%q, want %q/%q", artifactPath, artifactHash, artifact.Path, artifact.SHA256)
	}
}

func TestBuildCandidateActivationIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	oldBuild := Build{
		Key:       BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "old"},
		BuildName: "old-build",
		State:     StateValid,
	}
	newBuild := Build{
		Key:       BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "new"},
		BuildName: "new-build",
		State:     StatePreparing,
	}

	if err := UpsertDiscoveredBuild(ctx, db, oldBuild); err != nil {
		t.Fatalf("upsert old build: %v", err)
	}
	if err := MarkBuildReady(ctx, db, oldBuild.Key); err != nil {
		t.Fatalf("mark old ready: %v", err)
	}
	if err := ActivateBuild(ctx, db, oldBuild.Key); err != nil {
		t.Fatalf("activate old build: %v", err)
	}
	if err := UpsertDiscoveredBuild(ctx, db, newBuild); err != nil {
		t.Fatalf("upsert new build: %v", err)
	}
	if err := MarkBuildReady(ctx, db, newBuild.Key); err != nil {
		t.Fatalf("mark new ready: %v", err)
	}
	if err := ActivateBuild(ctx, db, newBuild.Key); err != nil {
		t.Fatalf("activate new build: %v", err)
	}

	var activeCount int
	if err := db.QueryRow(`
SELECT COUNT(*) FROM server_builds
WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND active = 1`).Scan(&activeCount); err != nil {
		t.Fatalf("count active builds: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active build rows = %d, want 1", activeCount)
	}

	active, err := ActiveBuild(ctx, db, "us", "wow", "enUS")
	if err != nil {
		t.Fatalf("active build: %v", err)
	}
	if active.Key.BuildKey != "new" || active.State != StateValid {
		t.Fatalf("active build = %#v, want new valid", active)
	}
}

func TestFailedCandidateDoesNotReplaceActiveBuild(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	oldBuild := Build{
		Key:       BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "old"},
		BuildName: "old-build",
		State:     StateValid,
	}
	newBuild := Build{
		Key:       BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "new"},
		BuildName: "new-build",
		State:     StatePreparing,
	}

	if err := UpsertDiscoveredBuild(ctx, db, oldBuild); err != nil {
		t.Fatalf("upsert old build: %v", err)
	}
	if err := MarkBuildReady(ctx, db, oldBuild.Key); err != nil {
		t.Fatalf("mark old ready: %v", err)
	}
	if err := ActivateBuild(ctx, db, oldBuild.Key); err != nil {
		t.Fatalf("activate old build: %v", err)
	}
	if err := UpsertDiscoveredBuild(ctx, db, newBuild); err != nil {
		t.Fatalf("upsert new build: %v", err)
	}
	if err := MarkBuildFailed(ctx, db, newBuild.Key, "prepare failed"); err != nil {
		t.Fatalf("mark new failed: %v", err)
	}
	if err := ActivateBuild(ctx, db, newBuild.Key); !errors.Is(err, ErrBuildNotReady) {
		t.Fatalf("activate failed build error = %v, want %v", err, ErrBuildNotReady)
	}

	active, err := ActiveBuild(ctx, db, "us", "wow", "enUS")
	if err != nil {
		t.Fatalf("active build: %v", err)
	}
	if active.Key.BuildKey != "old" {
		t.Fatalf("active build key = %q, want old", active.Key.BuildKey)
	}

	var failedState, failedError string
	if err := db.QueryRow(`
SELECT state, error FROM server_builds
WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND build_key = 'new'`).Scan(&failedState, &failedError); err != nil {
		t.Fatalf("query failed candidate: %v", err)
	}
	if failedState != StateFailed || failedError != "prepare failed" {
		t.Fatalf("failed candidate state/error = %q/%q, want failed/prepare failed", failedState, failedError)
	}
}

func TestMaterializedTableStateTransitions(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	key := TableKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", TableName: "Spell",
	}
	table := MaterializedTable{
		Key:                 key,
		DB2FileDataID:       123,
		DBDHash:             "dbd-a",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/db2/spell.parquet",
		RowCount:            42,
		State:               StatePreparing,
	}

	if err := UpsertMaterializedTable(ctx, db, table); err != nil {
		t.Fatalf("upsert materialized table: %v", err)
	}
	if err := MarkMaterializedTableState(ctx, db, key, StateValid, ""); err != nil {
		t.Fatalf("mark table valid: %v", err)
	}

	latest, err := LatestValidMaterializedTable(ctx, db, TableLookup{
		Region: "us", Product: "wow", Locale: "enUS", TableName: "Spell",
	})
	if err != nil {
		t.Fatalf("latest valid materialized table: %v", err)
	}
	if latest.Key.BuildKey != "build-1" || latest.State != StateValid || latest.RowCount != 42 {
		t.Fatalf("latest table = %#v, want build-1 valid row_count 42", latest)
	}

	if err := MarkMaterializedTableState(ctx, db, key, StateStale, "source changed"); err != nil {
		t.Fatalf("mark table stale: %v", err)
	}
	if _, err := LatestValidMaterializedTable(ctx, db, TableLookup{
		Region: "us", Product: "wow", Locale: "enUS", TableName: "Spell",
	}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("latest stale table error = %v, want sql.ErrNoRows", err)
	}

	var state, message string
	if err := db.QueryRow(`
SELECT state, error FROM server_materialized_tables
WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND build_key = 'build-1' AND table_name = 'Spell'`).Scan(&state, &message); err != nil {
		t.Fatalf("query materialized table: %v", err)
	}
	if state != StateStale || message != "source changed" {
		t.Fatalf("table state/error = %q/%q, want stale/source changed", state, message)
	}
}

func TestLatestValidMaterializedTableUsesUpsertSequenceWithinSameSecond(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	lookup := TableLookup{Region: "us", Product: "wow", Locale: "enUS", TableName: "Spell"}

	first := MaterializedTable{
		Key:                 TableKey{Region: lookup.Region, Product: lookup.Product, Locale: lookup.Locale, BuildKey: "build-2", TableName: lookup.TableName},
		DB2FileDataID:       123,
		DBDHash:             "dbd-a",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/db2/spell-build-2.parquet",
		RowCount:            2,
		State:               StateValid,
	}
	second := first
	second.Key.BuildKey = "build-10"
	second.ParquetPath = "cache/db2/spell-build-10.parquet"
	second.RowCount = 10

	if err := UpsertMaterializedTable(ctx, db, first); err != nil {
		t.Fatalf("upsert first table: %v", err)
	}
	if err := UpsertMaterializedTable(ctx, db, second); err != nil {
		t.Fatalf("upsert second table: %v", err)
	}
	if _, err := db.Exec(`
UPDATE server_materialized_tables
SET updated_at = '2026-06-02 00:00:00'
WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND table_name = 'Spell'`); err != nil {
		t.Fatalf("force same-second updated_at: %v", err)
	}

	latest, err := LatestValidMaterializedTable(ctx, db, lookup)
	if err != nil {
		t.Fatalf("latest valid materialized table: %v", err)
	}
	if latest.Key.BuildKey != "build-10" {
		t.Fatalf("latest build key = %q, want build-10", latest.Key.BuildKey)
	}
	if latest.RowCount != 10 {
		t.Fatalf("latest row count = %d, want 10", latest.RowCount)
	}
}

func TestListfileSourceHashUpdateMarksIndexStale(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	source := ListfileSource{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1",
		SourceHash: "hash-a",
		State:      StateValid,
	}

	changed, err := UpsertListfileSource(ctx, db, source)
	if err != nil {
		t.Fatalf("upsert listfile source: %v", err)
	}
	if changed {
		t.Fatalf("initial listfile source changed = true, want false")
	}
	if err := MarkListfileSourceState(ctx, db, source.Key(), StateValid, ""); err != nil {
		t.Fatalf("mark listfile valid: %v", err)
	}
	if err := UpsertListfileIndexState(ctx, db, source.Key(), "main", StateValid, ""); err != nil {
		t.Fatalf("upsert listfile index state: %v", err)
	}
	if err := MarkListfileSourceState(ctx, db, source.Key(), StateFailed, "old failure"); err != nil {
		t.Fatalf("mark listfile failed before unchanged upsert: %v", err)
	}
	changed, err = UpsertListfileSource(ctx, db, source)
	if err != nil {
		t.Fatalf("upsert unchanged listfile source: %v", err)
	}
	if changed {
		t.Fatalf("unchanged listfile source changed = true, want false")
	}
	assertListfileState(t, db, source.Key(), StateValid)
	assertListfileSourceError(t, db, source.Key(), "")
	assertListfileIndexState(t, ctx, db, source.Key(), "main", StateValid)

	source.SourceHash = "hash-b"
	changed, err = UpsertListfileSource(ctx, db, source)
	if err != nil {
		t.Fatalf("upsert changed listfile source: %v", err)
	}
	if !changed {
		t.Fatalf("changed listfile source changed = false, want true")
	}
	assertListfileState(t, db, source.Key(), StateValid)
	assertListfileSourceHash(t, db, source.Key(), "hash-b")
	assertListfileIndexState(t, ctx, db, source.Key(), "main", StateStale)
}

func TestCASCIndexVersionUpdateMarksIndexStale(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	source := CASCSource{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1",
		BuildConfig: "build-config-a",
		CDNConfig:   "cdn-config-a",
		State:       StateValid,
	}

	changed, err := UpsertCASCSource(ctx, db, source)
	if err != nil {
		t.Fatalf("upsert casc source: %v", err)
	}
	if changed {
		t.Fatalf("initial casc source changed = true, want false")
	}
	if err := MarkCASCSourceState(ctx, db, source.Key(), StateValid, ""); err != nil {
		t.Fatalf("mark casc valid: %v", err)
	}
	if err := UpsertCASCIndexState(ctx, db, source.Key(), "root-encoding-archive", StateValid, ""); err != nil {
		t.Fatalf("upsert casc index state: %v", err)
	}
	if err := MarkCASCSourceState(ctx, db, source.Key(), StateFailed, "old failure"); err != nil {
		t.Fatalf("mark casc failed before unchanged upsert: %v", err)
	}
	changed, err = UpsertCASCSource(ctx, db, source)
	if err != nil {
		t.Fatalf("upsert unchanged casc source: %v", err)
	}
	if changed {
		t.Fatalf("unchanged casc source changed = true, want false")
	}
	assertCASCState(t, db, source.Key(), StateValid)
	assertCASCSourceError(t, db, source.Key(), "")
	assertCASCIndexState(t, ctx, db, source.Key(), "root-encoding-archive", StateValid)

	source.CDNConfig = "cdn-config-b"
	changed, err = UpsertCASCSource(ctx, db, source)
	if err != nil {
		t.Fatalf("upsert changed casc source: %v", err)
	}
	if !changed {
		t.Fatalf("changed casc source changed = false, want true")
	}
	assertCASCState(t, db, source.Key(), StateValid)
	assertCASCSourceConfigs(t, db, source.Key(), "build-config-a", "cdn-config-b")
	assertCASCIndexState(t, ctx, db, source.Key(), "root-encoding-archive", StateStale)
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := reopenTestDB(t, dbPath(t))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func reopenTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	return db
}

func dbPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "metadata.sqlite")
}

func copyServerMigrations(t *testing.T, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatalf("create runtime migrations dir: %v", err)
	}
	src := filepath.Join("..", "..", "..", "..", "migrations", "server")
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read source migrations: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), data, 0644); err != nil {
			t.Fatalf("write runtime migration %s: %v", entry.Name(), err)
		}
	}
}

func assertListfileState(t *testing.T, db *sql.DB, key SourceKey, want string) {
	t.Helper()
	var state string
	err := db.QueryRow(`
SELECT state FROM server_listfile_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&state)
	if err != nil {
		t.Fatalf("query listfile state: %v", err)
	}
	if state != want {
		t.Fatalf("listfile state = %q, want %q", state, want)
	}
}

func assertListfileSourceHash(t *testing.T, db *sql.DB, key SourceKey, want string) {
	t.Helper()
	var hash string
	err := db.QueryRow(`
SELECT source_hash FROM server_listfile_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&hash)
	if err != nil {
		t.Fatalf("query listfile source hash: %v", err)
	}
	if hash != want {
		t.Fatalf("listfile source hash = %q, want %q", hash, want)
	}
}

func assertListfileSourceError(t *testing.T, db *sql.DB, key SourceKey, want string) {
	t.Helper()
	var message string
	err := db.QueryRow(`
SELECT error FROM server_listfile_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&message)
	if err != nil {
		t.Fatalf("query listfile source error: %v", err)
	}
	if message != want {
		t.Fatalf("listfile source error = %q, want %q", message, want)
	}
}

func assertListfileIndexState(t *testing.T, ctx context.Context, db *sql.DB, key SourceKey, indexName string, want string) {
	t.Helper()
	state, err := ListfileIndexState(ctx, db, key, indexName)
	if err != nil {
		t.Fatalf("query listfile index state: %v", err)
	}
	if state != want {
		t.Fatalf("listfile index state = %q, want %q", state, want)
	}
}

func assertCASCState(t *testing.T, db *sql.DB, key SourceKey, want string) {
	t.Helper()
	var state string
	err := db.QueryRow(`
SELECT state FROM server_casc_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&state)
	if err != nil {
		t.Fatalf("query casc state: %v", err)
	}
	if state != want {
		t.Fatalf("casc state = %q, want %q", state, want)
	}
}

func assertCASCSourceConfigs(t *testing.T, db *sql.DB, key SourceKey, wantBuildConfig string, wantCDNConfig string) {
	t.Helper()
	var buildConfig, cdnConfig string
	err := db.QueryRow(`
SELECT build_config, cdn_config FROM server_casc_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&buildConfig, &cdnConfig)
	if err != nil {
		t.Fatalf("query casc source configs: %v", err)
	}
	if buildConfig != wantBuildConfig || cdnConfig != wantCDNConfig {
		t.Fatalf("casc configs = %q/%q, want %q/%q", buildConfig, cdnConfig, wantBuildConfig, wantCDNConfig)
	}
}

func assertCASCSourceError(t *testing.T, db *sql.DB, key SourceKey, want string) {
	t.Helper()
	var message string
	err := db.QueryRow(`
SELECT error FROM server_casc_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&message)
	if err != nil {
		t.Fatalf("query casc source error: %v", err)
	}
	if message != want {
		t.Fatalf("casc source error = %q, want %q", message, want)
	}
}

func assertCASCIndexState(t *testing.T, ctx context.Context, db *sql.DB, key SourceKey, indexName string, want string) {
	t.Helper()
	state, err := CASCIndexState(ctx, db, key, indexName)
	if err != nil {
		t.Fatalf("query casc index state: %v", err)
	}
	if state != want {
		t.Fatalf("casc index state = %q, want %q", state, want)
	}
}
