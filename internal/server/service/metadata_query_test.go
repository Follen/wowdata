package service

import (
	"context"
	"database/sql"
	"errors"
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

func openMetadataQueryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := metadata.Open(":memory:")
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
