package metadata

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestOpenAllowsReadDuringHeldWriteTransaction(t *testing.T) {
	dir, err := migrationsDir()
	if err != nil {
		t.Fatalf("migrations dir: %v", err)
	}
	db, err := OpenWithMigrations(filepath.Join(t.TempDir(), "metadata.sqlite"), dir)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin write transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES ('held-write-test', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("hold write transaction: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("read during held write transaction: %v", err)
	}
}

func TestOpenInMemoryDatabaseUsesSingleConnection(t *testing.T) {
	dir, err := migrationsDir()
	if err != nil {
		t.Fatalf("migrations dir: %v", err)
	}
	db, err := OpenWithMigrations(":memory:", dir)
	if err != nil {
		t.Fatalf("open in-memory metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("in-memory max open connections = %d, want 1", got)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM server_builds`).Scan(&count); err != nil {
		t.Fatalf("query in-memory schema: %v", err)
	}
}

func TestSQLiteOpenDSNAppendsPragmasToExistingQuery(t *testing.T) {
	dsn := sqliteOpenDSN("file:metadata.sqlite?cache=shared")

	if strings.Count(dsn, "?") != 1 {
		t.Fatalf("dsn = %q, want a single query separator", dsn)
	}
	if !strings.Contains(dsn, "cache=shared") {
		t.Fatalf("dsn = %q, want existing query preserved", dsn)
	}
	if !strings.Contains(dsn, "_pragma=busy_timeout%3D5000") {
		t.Fatalf("dsn = %q, want busy_timeout pragma appended", dsn)
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
	if applied != 9 {
		t.Fatalf("applied migrations = %d, want 9", applied)
	}

	db.Close()
	db = reopenTestDB(t, path)
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count migrations after reopen: %v", err)
	}
	if applied != 9 {
		t.Fatalf("applied migrations after reopen = %d, want 9", applied)
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

func TestNoBuildStatePersistsThroughMigrations(t *testing.T) {
	ctx := context.Background()
	path := dbPath(t)
	db := reopenTestDB(t, path)
	key := BuildKey{Region: "us", Product: "wow_classic_titan", Locale: "enUS", BuildKey: ""}
	if err := UpsertDiscoveredBuild(ctx, db, Build{
		Key:   key,
		State: StateNoBuild,
		Error: "no build found for us/wow_classic_titan",
	}); err != nil {
		t.Fatalf("upsert no_build build: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close no_build DB: %v", err)
	}

	db = reopenTestDB(t, path)
	t.Cleanup(func() { _ = db.Close() })
	latest, err := LatestBuildForTarget(ctx, db, "us", "wow_classic_titan", "enUS")
	if err != nil {
		t.Fatalf("latest no_build build: %v", err)
	}
	if latest.State != StateNoBuild {
		t.Fatalf("latest state = %q, want %q", latest.State, StateNoBuild)
	}
	if latest.Error != "no build found for us/wow_classic_titan" {
		t.Fatalf("latest error = %q, want no-build error", latest.Error)
	}
}

func TestMarkBuildPreparingMessageUpdatesPreparingErrorText(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	key := BuildKey{Region: "us", Product: "wowxptr", Locale: "enUS", BuildKey: "beta-build"}
	if err := UpsertDiscoveredBuild(ctx, db, Build{
		Key:       key,
		BuildName: "12.0.7.67808",
		State:     StatePreparing,
	}); err != nil {
		t.Fatalf("upsert preparing build: %v", err)
	}

	if err := MarkBuildPreparingMessage(ctx, db, key, "resource preparation started"); err != nil {
		t.Fatalf("mark preparing message: %v", err)
	}

	latest, err := LatestBuildForTarget(ctx, db, "us", "wowxptr", "enUS")
	if err != nil {
		t.Fatalf("latest build: %v", err)
	}
	if latest.State != StatePreparing || latest.Error != "resource preparation started" {
		t.Fatalf("latest state/error = %q/%q, want preparing/resource preparation started", latest.State, latest.Error)
	}
}

func TestOpenWithMigrationsConvertsLegacyFailedNoBuildRows(t *testing.T) {
	ctx := context.Background()
	migrationsDir := filepath.Join(t.TempDir(), "migrations", "server")
	copyServerMigrations(t, migrationsDir)
	path := filepath.Join(t.TempDir(), "legacy-no-build.sqlite")
	seedLegacyFailedNoBuildMetadataDB(t, path)

	db, err := OpenWithMigrations(path, migrationsDir)
	if err != nil {
		t.Fatalf("open legacy failed no-build metadata DB: %v", err)
	}
	latest, err := LatestBuildForTarget(ctx, db, "tw", "wow_classic_titan", "enUS")
	if err != nil {
		t.Fatalf("latest converted no-build row: %v", err)
	}
	if latest.State != StateNoBuild {
		t.Fatalf("latest converted state = %q, want %q", latest.State, StateNoBuild)
	}
	if latest.Error != "no build found for tw/wow_classic_titan" {
		t.Fatalf("latest converted error = %q, want no-build error", latest.Error)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close converted no-build DB: %v", err)
	}

	db, err = OpenWithMigrations(path, migrationsDir)
	if err != nil {
		t.Fatalf("reopen converted no-build metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	latest, err = LatestBuildForTarget(ctx, db, "tw", "wow_classic_titan", "enUS")
	if err != nil {
		t.Fatalf("latest converted no-build row after reopen: %v", err)
	}
	if latest.State != StateNoBuild {
		t.Fatalf("latest converted state after reopen = %q, want %q", latest.State, StateNoBuild)
	}
	latest, err = LatestBuildForTarget(ctx, db, "kr", "wow_classic_titan", "enUS")
	if err != nil {
		t.Fatalf("latest converted no-build-key row after reopen: %v", err)
	}
	if latest.State != StateNoBuild {
		t.Fatalf("latest converted no-build-key state after reopen = %q, want %q", latest.State, StateNoBuild)
	}
	if latest.Error != "no build key found for kr/wow_classic_titan" {
		t.Fatalf("latest converted no-build-key error = %q, want no-build-key error", latest.Error)
	}
}

func TestOpenWithMigrationsSkipsNoBuildMigrationWhenLegacyBuildTableMissing(t *testing.T) {
	migrationsDir := filepath.Join(t.TempDir(), "migrations", "server")
	copyServerMigrations(t, migrationsDir)
	path := filepath.Join(t.TempDir(), "legacy-materialized-only.sqlite")
	seedLegacyMaterializedOnlyMetadataDB(t, path)

	db, err := OpenWithMigrations(path, migrationsDir)
	if err != nil {
		t.Fatalf("open legacy materialized-only metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	assertSchemaMigrationVersionExists(t, db, "0009_no_build_state.sql")
	assertTableMissing(t, db, "server_builds")
	assertLegacyMaterializedTablePreserved(t, db)
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

func TestListValidMaterializedTablesFiltersByBuildKey(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	lookup := TableCatalogLookup{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2"}

	tables := []MaterializedTable{
		{
			Key:                 TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", TableName: "Spell"},
			DB2FileDataID:       123,
			DBDHash:             "dbd-a",
			DecoderVersion:      "decoder-1",
			MaterializerVersion: "materializer-1",
			ParquetPath:         "cache/db2/spell-build-1.parquet",
			RowCount:            2,
			State:               StateValid,
		},
		{
			Key:                 TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2", TableName: "SpellBuild2Only"},
			DB2FileDataID:       123,
			DBDHash:             "dbd-b",
			DecoderVersion:      "decoder-1",
			MaterializerVersion: "materializer-1",
			ParquetPath:         "cache/db2/spell-build-2-only.parquet",
			RowCount:            4,
			State:               StateValid,
		},
		{
			Key:                 TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", TableName: "Item"},
			DB2FileDataID:       456,
			DBDHash:             "dbd-a",
			DecoderVersion:      "decoder-1",
			MaterializerVersion: "materializer-1",
			ParquetPath:         "cache/db2/item-build-1.parquet",
			RowCount:            6,
			State:               StateValid,
		},
		{
			Key:                 TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2", TableName: "Item"},
			DB2FileDataID:       456,
			DBDHash:             "dbd-b",
			DecoderVersion:      "decoder-1",
			MaterializerVersion: "materializer-1",
			ParquetPath:         "cache/db2/item-build-2.parquet",
			RowCount:            7,
			State:               StateValid,
		},
		{
			Key:                 TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", TableName: "OldBuildOnly"},
			DB2FileDataID:       654,
			DBDHash:             "dbd-a",
			DecoderVersion:      "decoder-1",
			MaterializerVersion: "materializer-1",
			ParquetPath:         "cache/db2/old-build-only.parquet",
			RowCount:            9,
			State:               StateValid,
		},
		{
			Key:                 TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", TableName: "GoOnlyStale"},
			DB2FileDataID:       789,
			DBDHash:             "dbd-a",
			DecoderVersion:      "decoder-1",
			MaterializerVersion: "materializer-1",
			ParquetPath:         "cache/db2/stale.parquet",
			RowCount:            8,
			State:               StateStale,
		},
		{
			Key:                 TableKey{Region: "eu", Product: "wow", Locale: "enUS", BuildKey: "build-1", TableName: "OtherRegion"},
			DB2FileDataID:       999,
			DBDHash:             "dbd-a",
			DecoderVersion:      "decoder-1",
			MaterializerVersion: "materializer-1",
			ParquetPath:         "cache/db2/other.parquet",
			RowCount:            10,
			State:               StateValid,
		},
	}
	for _, table := range tables {
		if err := UpsertMaterializedTable(ctx, db, table); err != nil {
			t.Fatalf("upsert table %s/%s: %v", table.Key.BuildKey, table.Key.TableName, err)
		}
	}

	got, err := ListValidMaterializedTables(ctx, db, lookup)
	if err != nil {
		t.Fatalf("list valid materialized tables: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("table count = %d, want 2: %#v", len(got), got)
	}
	if got[0].Key.TableName != "Item" || got[1].Key.TableName != "SpellBuild2Only" {
		t.Fatalf("table order = %#v, want Item then SpellBuild2Only", got)
	}
	for _, table := range got {
		if table.Key.BuildKey != "build-2" {
			t.Fatalf("returned table from wrong build: %#v", table)
		}
	}
}

func TestOpenWithMigrationsUpgradesLegacyMaterializedTableSchema(t *testing.T) {
	ctx := context.Background()
	migrationsDir := filepath.Join(t.TempDir(), "migrations", "server")
	copyServerMigrations(t, migrationsDir)
	path := filepath.Join(t.TempDir(), "legacy-metadata.sqlite")
	seedLegacyMaterializedMetadataDB(t, path)

	db, err := OpenWithMigrations(path, migrationsDir)
	if err != nil {
		t.Fatalf("open legacy metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

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
		t.Fatalf("upsert first table after legacy upgrade: %v", err)
	}
	if err := UpsertMaterializedTable(ctx, db, second); err != nil {
		t.Fatalf("upsert second table after legacy upgrade: %v", err)
	}
	if _, err := db.Exec(`
UPDATE server_materialized_tables
SET updated_at = '2026-06-02 00:00:00'
WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND table_name = 'Spell'`); err != nil {
		t.Fatalf("force same-second updated_at after legacy upgrade: %v", err)
	}

	latest, err := LatestValidMaterializedTable(ctx, db, lookup)
	if err != nil {
		t.Fatalf("latest valid table after legacy upgrade: %v", err)
	}
	if latest.Key.BuildKey != "build-10" {
		t.Fatalf("latest build key after legacy upgrade = %q, want build-10", latest.Key.BuildKey)
	}

	var seq int64
	if err := db.QueryRow(`
SELECT updated_seq FROM server_materialized_tables
WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND table_name = 'Spell' AND build_key = 'build-10'`).Scan(&seq); err != nil {
		t.Fatalf("query upgraded updated_seq: %v", err)
	}
	if seq <= 0 {
		t.Fatalf("updated_seq after legacy upgrade = %d, want positive", seq)
	}
}

func TestOpenWithMigrationsNormalizesLegacySchemaMigrationsNameColumn(t *testing.T) {
	migrationsDir := filepath.Join(t.TempDir(), "migrations", "server")
	copyServerMigrations(t, migrationsDir)
	path := filepath.Join(t.TempDir(), "legacy-metadata.sqlite")
	seedLegacySchemaMigrationsNameDB(t, path)

	db, err := OpenWithMigrations(path, migrationsDir)
	if err != nil {
		t.Fatalf("open legacy schema_migrations metadata DB: %v", err)
	}
	assertServerBuildsExists(t, db)
	assertSchemaMigrationVersionExists(t, db, "0001_init.sql")
	assertLegacySchemaMigrationNamePreserved(t, db, "0001_init.sql")
	assertLegacyTablePreserved(t, db)
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy metadata DB: %v", err)
	}

	db, err = OpenWithMigrations(path, migrationsDir)
	if err != nil {
		t.Fatalf("reopen normalized legacy schema_migrations metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	assertServerBuildsExists(t, db)
	assertSchemaMigrationVersionExists(t, db, "0001_init.sql")
	assertLegacySchemaMigrationNamePreserved(t, db, "0001_init.sql")
	assertLegacyTablePreserved(t, db)
}

func TestOpenWithMigrationsBackfillsLegacyMaterializedRowOrder(t *testing.T) {
	ctx := context.Background()
	migrationsDir := filepath.Join(t.TempDir(), "migrations", "server")
	copyServerMigrations(t, migrationsDir)
	path := filepath.Join(t.TempDir(), "legacy-metadata.sqlite")
	seedLegacyMaterializedMetadataDB(t, path)
	insertLegacyMaterializedRow(t, path, "build-old", "2026-06-02 00:00:01", 1)
	insertLegacyMaterializedRow(t, path, "build-new", "2026-06-02 00:00:02", 2)

	db, err := OpenWithMigrations(path, migrationsDir)
	if err != nil {
		t.Fatalf("open legacy metadata DB with rows: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	latest, err := LatestValidMaterializedTable(ctx, db, TableLookup{
		Region: "us", Product: "wow", Locale: "enUS", TableName: "Spell",
	})
	if err != nil {
		t.Fatalf("latest valid table after legacy row backfill: %v", err)
	}
	if latest.Key.BuildKey != "build-new" {
		t.Fatalf("latest legacy build key = %q, want build-new", latest.Key.BuildKey)
	}
	if latest.RowCount != 2 {
		t.Fatalf("latest legacy row count = %d, want 2", latest.RowCount)
	}

	var oldSeq, newSeq, seededSeq int64
	if err := db.QueryRow(`SELECT updated_seq FROM server_materialized_tables WHERE build_key = 'build-old'`).Scan(&oldSeq); err != nil {
		t.Fatalf("query old legacy updated_seq: %v", err)
	}
	if err := db.QueryRow(`SELECT updated_seq FROM server_materialized_tables WHERE build_key = 'build-new'`).Scan(&newSeq); err != nil {
		t.Fatalf("query new legacy updated_seq: %v", err)
	}
	if err := db.QueryRow(`SELECT value FROM server_metadata_sequences WHERE name = 'server_materialized_tables'`).Scan(&seededSeq); err != nil {
		t.Fatalf("query materialized sequence seed: %v", err)
	}
	if !(oldSeq > 0 && newSeq > oldSeq && seededSeq == newSeq) {
		t.Fatalf("legacy sequence backfill old/new/seed = %d/%d/%d, want positive increasing and seeded to new", oldSeq, newSeq, seededSeq)
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

func seedLegacyMaterializedMetadataDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy metadata DB for seed: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
CREATE TABLE schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE server_builds (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  locale TEXT NOT NULL,
  build_key TEXT NOT NULL,
  build_name TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN ('valid', 'stale', 'preparing', 'failed')),
  active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0, 1)),
  error TEXT NOT NULL DEFAULT '',
  discovered_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product, locale, build_key)
);

CREATE UNIQUE INDEX server_builds_one_active
ON server_builds(region, product, locale)
WHERE active = 1;

CREATE TABLE server_materialized_tables (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  locale TEXT NOT NULL,
  build_key TEXT NOT NULL,
  table_name TEXT NOT NULL,
  db2_file_data_id INTEGER NOT NULL,
  dbd_hash TEXT NOT NULL DEFAULT '',
  decoder_version TEXT NOT NULL DEFAULT '',
  materializer_version TEXT NOT NULL DEFAULT '',
  parquet_path TEXT NOT NULL DEFAULT '',
  row_count INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL CHECK(state IN ('valid', 'stale', 'preparing', 'failed')),
  error TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product, locale, build_key, table_name)
);

CREATE INDEX server_materialized_tables_lookup
ON server_materialized_tables(region, product, locale, table_name, state, updated_at);
`); err != nil {
		t.Fatalf("create legacy metadata schema: %v", err)
	}
	for _, version := range []string{
		"0001_init.sql",
		"0002_builds.sql",
		"0003_materialized_tables.sql",
		"0004_listfile.sql",
		"0005_casc_index.sql",
		"0006_artifacts.sql",
		"0007_refresh.sql",
	} {
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, version); err != nil {
			t.Fatalf("record legacy migration %s: %v", version, err)
		}
	}
}

func seedLegacyFailedNoBuildMetadataDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy failed no-build DB for seed: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
CREATE TABLE schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE server_builds (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  locale TEXT NOT NULL,
  build_key TEXT NOT NULL,
  build_name TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN ('valid', 'stale', 'preparing', 'failed')),
  active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0, 1)),
  error TEXT NOT NULL DEFAULT '',
  discovered_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product, locale, build_key)
);

CREATE UNIQUE INDEX server_builds_one_active
ON server_builds(region, product, locale)
WHERE active = 1;

INSERT INTO server_builds(region, product, locale, build_key, build_name, state, active, error)
VALUES
  ('tw', 'wow_classic_titan', 'enUS', '', '', 'failed', 0, 'no build found for tw/wow_classic_titan'),
  ('kr', 'wow_classic_titan', 'enUS', '', '', 'failed', 0, 'no build key found for kr/wow_classic_titan');
`); err != nil {
		t.Fatalf("create legacy failed no-build schema: %v", err)
	}
	for _, version := range []string{
		"0001_init.sql",
		"0002_builds.sql",
		"0003_materialized_tables.sql",
		"0004_listfile.sql",
		"0005_casc_index.sql",
		"0006_artifacts.sql",
		"0007_refresh.sql",
		"0008_materialized_table_sequence.sql",
	} {
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, version); err != nil {
			t.Fatalf("record legacy no-build migration %s: %v", version, err)
		}
	}
}

func seedLegacyMaterializedOnlyMetadataDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy materialized-only metadata DB for seed: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
CREATE TABLE schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE server_materialized_tables (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  locale TEXT NOT NULL,
  build_key TEXT NOT NULL,
  table_name TEXT NOT NULL,
  db2_file_data_id INTEGER NOT NULL,
  dbd_hash TEXT NOT NULL DEFAULT '',
  decoder_version TEXT NOT NULL DEFAULT '',
  materializer_version TEXT NOT NULL DEFAULT '',
  parquet_path TEXT NOT NULL DEFAULT '',
  row_count INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL CHECK(state IN ('valid', 'stale', 'preparing', 'failed')),
  error TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product, locale, build_key, table_name)
);

INSERT INTO server_materialized_tables (
  region, product, locale, build_key, table_name,
  db2_file_data_id, dbd_hash, decoder_version, materializer_version,
  parquet_path, row_count, state, error, updated_at
) VALUES (
  'us', 'wow', 'enUS', 'build-1', 'Spell',
  123, 'dbd-a', 'decoder-1', 'materializer-1',
  'cache/db2/spell-build-1.parquet', 42, 'valid', '', '2026-06-02 00:00:00'
);
`); err != nil {
		t.Fatalf("create legacy materialized-only schema: %v", err)
	}
	for _, version := range []string{
		"0001_init.sql",
		"0002_builds.sql",
		"0003_materialized_tables.sql",
		"0004_listfile.sql",
		"0005_casc_index.sql",
		"0006_artifacts.sql",
		"0007_refresh.sql",
		"0008_materialized_table_sequence.sql",
	} {
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, version); err != nil {
			t.Fatalf("record legacy materialized-only migration %s: %v", version, err)
		}
	}
}

func seedLegacySchemaMigrationsNameDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy schema_migrations DB for seed: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
CREATE TABLE schema_migrations (
  name TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE legacy_cache_marker (
  id INTEGER PRIMARY KEY,
  marker TEXT NOT NULL
);

INSERT INTO schema_migrations(name, applied_at)
VALUES ('0001_init.sql', '2026-06-01 00:00:00');

INSERT INTO legacy_cache_marker(id, marker)
VALUES (1, 'preserved');
`); err != nil {
		t.Fatalf("create legacy schema_migrations DB: %v", err)
	}
}

func assertServerBuildsExists(t *testing.T, db *sql.DB) {
	t.Helper()
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'server_builds'`).Scan(&name); err != nil {
		t.Fatalf("server_builds missing: %v", err)
	}
}

func assertSchemaMigrationVersionExists(t *testing.T, db *sql.DB, version string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count); err != nil {
		t.Fatalf("schema_migrations version query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("schema_migrations version %q count = %d, want 1", version, count)
	}
}

func assertLegacySchemaMigrationNamePreserved(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations_legacy_name WHERE name = ?`, name).Scan(&count); err != nil {
		t.Fatalf("legacy schema_migrations name query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy schema_migrations name %q count = %d, want 1", name, count)
	}
}

func assertLegacyTablePreserved(t *testing.T, db *sql.DB) {
	t.Helper()
	var marker string
	if err := db.QueryRow(`SELECT marker FROM legacy_cache_marker WHERE id = 1`).Scan(&marker); err != nil {
		t.Fatalf("legacy table marker missing: %v", err)
	}
	if marker != "preserved" {
		t.Fatalf("legacy marker = %q, want preserved", marker)
	}
}

func assertTableMissing(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		t.Fatalf("query table %s: %v", table, err)
	}
	if count != 0 {
		t.Fatalf("table %s exists, want missing", table)
	}
}

func assertLegacyMaterializedTablePreserved(t *testing.T, db *sql.DB) {
	t.Helper()
	var rowCount int
	if err := db.QueryRow(`
SELECT row_count FROM server_materialized_tables
WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND build_key = 'build-1' AND table_name = 'Spell'`).Scan(&rowCount); err != nil {
		t.Fatalf("legacy materialized table row missing: %v", err)
	}
	if rowCount != 42 {
		t.Fatalf("legacy materialized table row_count = %d, want 42", rowCount)
	}
}

func insertLegacyMaterializedRow(t *testing.T, path string, buildKey string, updatedAt string, rowCount int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy metadata DB for row insert: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
INSERT INTO server_materialized_tables (
  region, product, locale, build_key, table_name,
  db2_file_data_id, dbd_hash, decoder_version, materializer_version,
  parquet_path, row_count, state, error, updated_at
) VALUES (
  'us', 'wow', 'enUS', ?, 'Spell',
  123, 'dbd-a', 'decoder-1', 'materializer-1',
  ?, ?, 'valid', '', ?
)`,
		buildKey,
		"cache/db2/spell-"+buildKey+".parquet",
		rowCount,
		updatedAt,
	); err != nil {
		t.Fatalf("insert legacy materialized row %s: %v", buildKey, err)
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
