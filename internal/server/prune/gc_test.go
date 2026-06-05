package prune

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"wowdata/internal/server/storage/metadata"
)

func TestGCDryRunDoesNotDeleteMetadataOrFiles(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	roots := seedGCTestTarget(t, ctx, db)

	var out bytes.Buffer
	result, err := Run(ctx, Options{DB: db, Roots: roots, Writer: &out})
	if err != nil {
		t.Fatalf("dry-run gc: %v", err)
	}
	if result.Applied {
		t.Fatal("dry-run result applied = true")
	}
	if result.Plan.DeleteBuilds != 2 {
		t.Fatalf("delete builds = %d, want 2", result.Plan.DeleteBuilds)
	}
	if !bytes.Contains(out.Bytes(), []byte("dry-run: no metadata or files deleted")) {
		t.Fatalf("dry-run output missing safety message:\n%s", out.String())
	}
	assertBuildExists(t, db, "old-valid")
	assertFileExists(t, filepath.Join(roots.DB2Dir, "us", "wow", "old-valid", "enUS", "Spell.parquet"))
	assertFileExists(t, filepath.Join(roots.RawDir, "casc", "us", "wow", "old-valid", "data", "abc"))
}

func TestGCApplyDeletesOnlyOldUnprotectedBuilds(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	roots := seedGCTestTarget(t, ctx, db)

	var out bytes.Buffer
	result, err := Run(ctx, Options{DB: db, Roots: roots, Apply: true, Writer: &out})
	if err != nil {
		t.Fatalf("apply gc: %v", err)
	}
	if !result.Applied {
		t.Fatal("apply result applied = false")
	}
	if result.DeletedRows == 0 {
		t.Fatal("deleted rows = 0, want metadata deleted")
	}
	if result.DeletedPaths == 0 {
		t.Fatal("deleted paths = 0, want files deleted")
	}
	assertBuildMissing(t, db, "old-valid")
	assertBuildMissing(t, db, "active-old")
	assertBuildExists(t, db, "latest-valid")
	assertBuildExists(t, db, "preparing-old")
	assertFileMissing(t, filepath.Join(roots.DB2Dir, "us", "wow", "old-valid", "enUS", "Spell.parquet"))
	assertFileMissing(t, filepath.Join(roots.RawDir, "casc", "us", "wow", "old-valid", "data", "abc"))
	assertFileMissing(t, filepath.Join(roots.DB2Dir, "us", "wow", "active-old", "enUS", "Item.parquet"))
	assertFileExists(t, filepath.Join(roots.DB2Dir, "us", "wow", "latest-valid", "enUS", "Item.parquet"))
	assertFileExists(t, filepath.Join(roots.DB2Dir, "us", "wow", "preparing-old", "enUS", "Item.parquet"))
}

func TestGCApplyNeverDeletesActiveBuild(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	root := t.TempDir()
	roots := Roots{CacheRoot: root, DB2Dir: filepath.Join(root, "db2"), RawDir: filepath.Join(root, "raw")}
	insertBuild(t, db, "us", "wow", "enUS", "active-old", metadata.StateValid, true, "2026-01-01 00:00:00")
	insertBuild(t, db, "us", "wow", "enUS", "failed-newer", metadata.StateFailed, false, "2026-01-02 00:00:00")
	writeParquetForBuild(t, ctx, db, roots, "us", "wow", "enUS", "active-old", "Item")
	writeParquetForBuild(t, ctx, db, roots, "us", "wow", "enUS", "failed-newer", "Item")

	if _, err := Run(ctx, Options{DB: db, Roots: roots, Apply: true}); err != nil {
		t.Fatalf("apply gc: %v", err)
	}
	assertBuildExists(t, db, "active-old")
	assertBuildExists(t, db, "failed-newer")
	assertFileExists(t, filepath.Join(roots.DB2Dir, "us", "wow", "active-old", "enUS", "Item.parquet"))
}

func TestGCPlansTargetsIndependently(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	roots := seedGCTestTarget(t, ctx, db)

	insertBuild(t, db, "us", "wow_classic", "enUS", "classic-old", metadata.StateValid, false, "2026-01-01 00:00:00")
	insertBuild(t, db, "us", "wow_classic", "enUS", "classic-new", metadata.StateValid, false, "2026-01-02 00:00:00")
	writeParquetForBuild(t, ctx, db, roots, "us", "wow_classic", "enUS", "classic-old", "Spell")
	writeParquetForBuild(t, ctx, db, roots, "us", "wow_classic", "enUS", "classic-new", "Spell")

	plan, err := BuildPlanForLatestOnly(ctx, db, roots)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.DeleteBuilds != 3 {
		t.Fatalf("delete builds = %d, want 3", plan.DeleteBuilds)
	}
	wantDelete := map[string]bool{"active-old": false, "old-valid": false, "classic-old": false}
	for _, target := range plan.Targets {
		for _, build := range target.Delete {
			if _, ok := wantDelete[build.Build.BuildKey]; ok {
				wantDelete[build.Build.BuildKey] = true
			}
		}
	}
	for key, seen := range wantDelete {
		if !seen {
			t.Fatalf("expected delete build %s in plan", key)
		}
	}
}

func seedGCTestTarget(t *testing.T, ctx context.Context, db *sql.DB) Roots {
	t.Helper()
	root := t.TempDir()
	roots := Roots{
		CacheRoot:    root,
		DB2Dir:       filepath.Join(root, "db2"),
		RawDir:       filepath.Join(root, "raw"),
		ArtifactRoot: filepath.Join(root, "artifacts"),
	}
	insertBuild(t, db, "us", "wow", "enUS", "active-old", metadata.StateValid, false, "2026-01-01 00:00:00")
	insertBuild(t, db, "us", "wow", "enUS", "old-valid", metadata.StateValid, false, "2026-01-02 00:00:00")
	insertBuild(t, db, "us", "wow", "enUS", "preparing-old", metadata.StatePreparing, false, "2026-01-03 00:00:00")
	insertBuild(t, db, "us", "wow", "enUS", "latest-valid", metadata.StateValid, true, "2026-01-04 00:00:00")
	writeParquetForBuild(t, ctx, db, roots, "us", "wow", "enUS", "active-old", "Item")
	writeParquetForBuild(t, ctx, db, roots, "us", "wow", "enUS", "old-valid", "Spell")
	writeParquetForBuild(t, ctx, db, roots, "us", "wow", "enUS", "preparing-old", "Item")
	writeParquetForBuild(t, ctx, db, roots, "us", "wow", "enUS", "latest-valid", "Item")
	writeFile(t, filepath.Join(roots.RawDir, "casc", "us", "wow", "old-valid", "data", "abc"), "raw-old")
	writeFile(t, filepath.Join(roots.RawDir, "casc", "us", "wow", "latest-valid", "data", "abc"), "raw-latest")
	return roots
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatalf("open metadata db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertBuild(t *testing.T, db *sql.DB, region, product, locale, buildKey, state string, active bool, timestamp string) {
	t.Helper()
	activeInt := 0
	if active {
		activeInt = 1
	}
	if _, err := db.Exec(`
INSERT INTO server_builds(region, product, locale, build_key, build_name, state, active, discovered_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		region, product, locale, buildKey, buildKey, state, activeInt, timestamp, timestamp,
	); err != nil {
		t.Fatalf("insert build %s: %v", buildKey, err)
	}
}

func writeParquetForBuild(t *testing.T, ctx context.Context, db *sql.DB, roots Roots, region, product, locale, buildKey, table string) {
	t.Helper()
	path := filepath.Join(roots.DB2Dir, region, product, buildKey, locale, table+".parquet")
	writeFile(t, path, "parquet-"+buildKey+"-"+table)
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key: metadata.TableKey{
			Region: region, Product: product, Locale: locale, BuildKey: buildKey, TableName: table,
		},
		DB2FileDataID:       1,
		DBDHash:             "dbd",
		DecoderVersion:      "loader",
		MaterializerVersion: "test",
		ParquetPath:         path,
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert materialized table %s/%s: %v", buildKey, table, err)
	}
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = os.Chtimes(path, now, now)
}

func assertBuildExists(t *testing.T, db *sql.DB, buildKey string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM server_builds WHERE build_key = ?`, buildKey).Scan(&count); err != nil {
		t.Fatalf("count build %s: %v", buildKey, err)
	}
	if count != 1 {
		t.Fatalf("build %s count = %d, want 1", buildKey, count)
	}
}

func assertBuildMissing(t *testing.T, db *sql.DB, buildKey string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM server_builds WHERE build_key = ?`, buildKey).Scan(&count); err != nil {
		t.Fatalf("count build %s: %v", buildKey, err)
	}
	if count != 0 {
		t.Fatalf("build %s count = %d, want 0", buildKey, count)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s to exist: %v", path, err)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file %s missing, err=%v", path, err)
	}
}
