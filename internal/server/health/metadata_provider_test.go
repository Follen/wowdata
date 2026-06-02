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
