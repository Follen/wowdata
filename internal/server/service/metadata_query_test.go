package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"wowdata/internal/server/storage/metadata"
)

func TestMetadataQueryServiceTablesReadsRequestedBuildCatalog(t *testing.T) {
	ctx := context.Background()
	db := openMetadataQueryTestDB(t)
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key:                 metadata.TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", TableName: "Item"},
		DB2FileDataID:       1,
		DBDHash:             "dbd-a",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/db2/item.parquet",
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert Item metadata: %v", err)
	}
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key:                 metadata.TableKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2", TableName: "OldBuildOnly"},
		DB2FileDataID:       2,
		DBDHash:             "dbd-b",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/db2/old.parquet",
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert OldBuildOnly metadata: %v", err)
	}

	catalog, err := NewMetadataQueryServiceWithDB(db).Tables(ctx, TablesRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"},
	})
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	if len(catalog.Tables) != 1 || catalog.Tables[0].Name != "Item" {
		t.Fatalf("catalog = %#v, want only Item", catalog)
	}
}

func TestMetadataQueryServiceTablesUsesActiveBuildWhenBuildKeyOmitted(t *testing.T) {
	ctx := context.Background()
	db := openMetadataQueryTestDB(t)
	active := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"}
	inactive := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "inactive-build"}
	seedReadyBuild(t, ctx, db, inactive)
	seedReadyBuild(t, ctx, db, active)
	if err := metadata.ActivateBuild(ctx, db, active); err != nil {
		t.Fatalf("activate build: %v", err)
	}
	seedMaterializedTable(t, ctx, db, inactive, "OldBuildOnly")
	seedMaterializedTable(t, ctx, db, active, "Item")

	catalog, err := NewMetadataQueryServiceWithDB(db).Tables(ctx, TablesRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS"},
	})
	if err != nil {
		t.Fatalf("Tables without buildKey: %v", err)
	}
	if len(catalog.Tables) != 1 || catalog.Tables[0].Name != "Item" {
		t.Fatalf("catalog = %#v, want active build Item only", catalog)
	}
}

func TestMetadataQueryServiceTablesWithoutBuildKeyRequiresActiveBuild(t *testing.T) {
	_, err := NewMetadataQueryServiceWithDB(openMetadataQueryTestDB(t)).Tables(context.Background(), TablesRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS"},
	})
	if err == nil {
		t.Fatal("Tables without buildKey error = nil, want active build error")
	}
	if !strings.Contains(err.Error(), "active build") {
		t.Fatalf("Tables error = %q, want active build message", err.Error())
	}
}

func TestMetadataQueryServiceRowsRemainCapabilityUnavailable(t *testing.T) {
	_, err := NewMetadataQueryServiceWithDB(openMetadataQueryTestDB(t)).Rows(context.Background(), QueryRowsRequest{Table: "Item"})
	if err == nil {
		t.Fatal("Rows error = nil, want capability unavailable")
	}
	var unavailable CapabilityUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("Rows error = %T %[1]v, want CapabilityUnavailableError", err)
	}
}

func seedReadyBuild(t *testing.T, ctx context.Context, db *sql.DB, key metadata.BuildKey) {
	t.Helper()
	if err := metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{Key: key, BuildName: key.BuildKey, State: metadata.StatePreparing}); err != nil {
		t.Fatalf("upsert build %s: %v", key.BuildKey, err)
	}
	if err := metadata.MarkBuildReady(ctx, db, key); err != nil {
		t.Fatalf("mark build ready %s: %v", key.BuildKey, err)
	}
}

func seedMaterializedTable(t *testing.T, ctx context.Context, db *sql.DB, key metadata.BuildKey, table string) {
	t.Helper()
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key:                 metadata.TableKey{Region: key.Region, Product: key.Product, Locale: key.Locale, BuildKey: key.BuildKey, TableName: table},
		DB2FileDataID:       1,
		DBDHash:             "dbd-a",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/db2/" + table + ".parquet",
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert table %s/%s: %v", key.BuildKey, table, err)
	}
}

func openMetadataQueryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := metadata.Open(":memory:")
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
