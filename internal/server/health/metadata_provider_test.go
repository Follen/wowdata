package health

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"wowdata/internal/server/config"
	"wowdata/internal/server/storage/metadata"
)

func TestMetadataProviderOpensDBOnceAndReusesHandle(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db, err := metadata.Open(metadataPath)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	buildKey := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"}
	if err := metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{
		Key:       buildKey,
		BuildName: buildKey.BuildKey,
		State:     metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert active build: %v", err)
	}
	if err := metadata.ActivateBuild(ctx, db, buildKey); err != nil {
		t.Fatalf("activate build: %v", err)
	}
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key:                 metadata.TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build", TableName: "Item"},
		DB2FileDataID:       1,
		DBDHash:             "dbd-a",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/db2/item.parquet",
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert materialized table: %v", err)
	}

	cfg := config.Default()
	cfg.Cache.MetadataDB = metadataPath
	cfg.Prepare.Targets = []config.PrepareTarget{{
		Label:   "US Retail",
		Region:  "us",
		Product: "wow",
		Locale:  "enUS",
	}}

	openCalls := 0
	provider, err := newMetadataProviderWithOpener(cfg, metadataPath, func(path string) (*sql.DB, error) {
		openCalls++
		if path != metadataPath {
			t.Fatalf("metadata path = %q, want %q", path, metadataPath)
		}
		return db, nil
	})
	if err != nil {
		t.Fatalf("new metadata provider: %v", err)
	}

	if _, err := provider.HealthSnapshot(ctx); err != nil {
		t.Fatalf("first health snapshot: %v", err)
	}
	if _, err := provider.HealthSnapshot(ctx); err != nil {
		t.Fatalf("second health snapshot: %v", err)
	}
	if openCalls != 1 {
		t.Fatalf("metadata open calls = %d, want 1", openCalls)
	}
}

func TestMetadataProviderReportsFailedLatestBuildWhenNoActiveBuildExists(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db, err := metadata.Open(metadataPath)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{
		Key:       metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "failed-build"},
		BuildName: "failed-build",
		State:     metadata.StateFailed,
		Error:     "Spell decode failed",
	}); err != nil {
		t.Fatalf("upsert failed build: %v", err)
	}

	cfg := testMetadataProviderConfig(metadataPath)
	provider := NewMetadataProviderWithDB(cfg, metadataPath, db)
	snapshot, err := provider.HealthSnapshot(ctx)
	if err != nil {
		t.Fatalf("HealthSnapshot: %v", err)
	}
	if snapshot.Readiness.OK || snapshot.Readiness.RequiredTargetsReady != 0 || snapshot.Readiness.RequiredTargetsTotal != 1 {
		t.Fatalf("readiness = %#v, want failed required target not ready", snapshot.Readiness)
	}
	if len(snapshot.Contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(snapshot.Contexts))
	}
	got := snapshot.Contexts[0]
	if got.State != StateFailed {
		t.Fatalf("context state = %q, want failed", got.State)
	}
	if got.Error != "Spell decode failed" {
		t.Fatalf("context error = %q, want failed build error", got.Error)
	}
	if got.ActiveBuild != "" {
		t.Fatalf("active build = %q, want empty without active build", got.ActiveBuild)
	}
	if snapshot.Matrix.Failed != 1 || snapshot.Matrix.Preparing != 0 {
		t.Fatalf("matrix = %#v, want one failed target", snapshot.Matrix)
	}
}

func TestMetadataProviderReportsNoBuildLatestBuildWhenNoActiveBuildExists(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db, err := metadata.Open(metadataPath)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{
		Key:   metadata.BuildKey{Region: "us", Product: "wow_classic_titan", Locale: "enUS", BuildKey: ""},
		State: metadata.StateNoBuild,
		Error: "no build found for us/wow_classic_titan",
	}); err != nil {
		t.Fatalf("upsert no-build row: %v", err)
	}

	cfg := testMetadataProviderConfig(metadataPath)
	cfg.Prepare.Targets[0].Product = "wow_classic_titan"
	provider := NewMetadataProviderWithDB(cfg, metadataPath, db)
	snapshot, err := provider.HealthSnapshot(ctx)
	if err != nil {
		t.Fatalf("HealthSnapshot: %v", err)
	}
	if !snapshot.Readiness.OK || snapshot.Readiness.RequiredTargetsTotal != 0 {
		t.Fatalf("readiness = %#v, want non-strict no_build not required", snapshot.Readiness)
	}
	if len(snapshot.Contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(snapshot.Contexts))
	}
	got := snapshot.Contexts[0]
	if got.State != StateNoBuild {
		t.Fatalf("context state = %q, want no_build", got.State)
	}
	if got.Error != "no build found for us/wow_classic_titan" {
		t.Fatalf("context error = %q, want no-build error", got.Error)
	}
	if snapshot.Matrix.NoBuild != 1 || snapshot.Matrix.Failed != 0 {
		t.Fatalf("matrix = %#v, want one no_build target", snapshot.Matrix)
	}
}

func TestMetadataProviderLeavesMissingBuildPreparing(t *testing.T) {
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db, err := metadata.Open(metadataPath)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	defer db.Close()

	cfg := testMetadataProviderConfig(metadataPath)
	provider := NewMetadataProviderWithDB(cfg, metadataPath, db)
	snapshot, err := provider.HealthSnapshot(context.Background())
	if err != nil {
		t.Fatalf("HealthSnapshot: %v", err)
	}
	if len(snapshot.Contexts) != 1 || snapshot.Contexts[0].State != StatePreparing {
		t.Fatalf("contexts = %#v, want missing build to remain preparing", snapshot.Contexts)
	}
}

func testMetadataProviderConfig(metadataPath string) config.Config {
	cfg := config.Default()
	cfg.Cache.MetadataDB = metadataPath
	cfg.Prepare.Targets = []config.PrepareTarget{{
		Label: "US Retail", Region: "us", Product: "wow", Locale: "enUS",
	}}
	cfg.Prepare.DefaultTables = []string{"Item"}
	return cfg
}
