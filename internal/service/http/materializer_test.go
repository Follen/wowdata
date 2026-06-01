package http

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"wowdata/internal/cache/metadata"
	cacheparquet "wowdata/internal/cache/parquet"
	appruntime "wowdata/internal/runtime"
)

func TestDB2MaterializerWritesParquetFooterValidatesAndRecordsSQLite(t *testing.T) {
	db := openMaterializerTestDB(t)
	var validateCalls int
	mat := &DB2Materializer{
		CacheRoot:             t.TempDir(),
		MetadataDB:            db,
		Resolver:              fakeContextResolver{ctx: materializerRuntimeContext()},
		Loader:                fakeTableLoader{loaded: materializerLoadedTable()},
		MaterializerVersion:   "materializer-test",
		MigrationsDescription: "test",
		ValidateParquet: func(path string, want cacheparquet.Metadata) (cacheparquet.Metadata, error) {
			validateCalls++
			return cacheparquet.ValidateExisting(path, want)
		},
	}

	if err := mat.EnsureTable(context.Background(), RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}, "SpellName"); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}

	wantMeta := cacheparquet.Metadata{
		Region:              "cn",
		Product:             "wow",
		BuildKey:            "build-key",
		Locale:              "zhCN",
		Table:               "SpellName",
		DB2FileDataID:       123,
		DBDDefinitionHash:   "dbd-hash",
		DecoderVersion:      "decoder-v1",
		MaterializerVersion: "materializer-test",
	}
	path := cacheparquet.PathFor(mat.CacheRoot, wantMeta)
	if _, err := cacheparquet.ValidateExisting(path, wantMeta); err != nil {
		t.Fatalf("ValidateExisting parquet footer: %v", err)
	}
	if validateCalls != 1 {
		t.Fatalf("ValidateExisting calls = %d, want 1", validateCalls)
	}

	record, err := metadata.GetMaterializedTable(db, "cn", "wow", "build-key", "zhCN", "SpellName")
	if err != nil {
		t.Fatalf("GetMaterializedTable: %v", err)
	}
	if record.State != metadata.StateValid || record.ParquetPath != path || record.RowCount != 42 {
		t.Fatalf("materialized record = %#v", record)
	}
}

func TestDB2MaterializerReusesValidParquetRecordWithoutLoadingDB2(t *testing.T) {
	db := openMaterializerTestDB(t)
	cacheRoot := t.TempDir()
	meta := cacheparquet.Metadata{
		Region:              "cn",
		Product:             "wow",
		BuildKey:            "build-key",
		Locale:              "zhCN",
		Table:               "SpellName",
		DB2FileDataID:       123,
		DBDDefinitionHash:   "dbd-hash",
		DecoderVersion:      "decoder-v1",
		MaterializerVersion: "materializer-test",
	}
	path := cacheparquet.PathFor(cacheRoot, meta)
	if err := cacheparquet.WriteRowsFile(path, meta, materializerLoadedTable().Schema, materializerLoadedTable().Rows); err != nil {
		t.Fatalf("write cached parquet: %v", err)
	}
	if err := metadata.UpsertMaterializedTable(db, metadata.MaterializedTable{
		Region:              meta.Region,
		Product:             meta.Product,
		BuildKey:            meta.BuildKey,
		BuildName:           "12.0.0.61234",
		Locale:              meta.Locale,
		TableName:           meta.Table,
		DB2FileDataID:       meta.DB2FileDataID,
		DBDDefinitionHash:   meta.DBDDefinitionHash,
		DecoderVersion:      meta.DecoderVersion,
		MaterializerVersion: meta.MaterializerVersion,
		ParquetPath:         path,
		RowCount:            42,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("seed materialized table: %v", err)
	}

	loader := &countingTableLoader{loaded: materializerLoadedTable()}
	mat := &DB2Materializer{
		CacheRoot:           cacheRoot,
		MetadataDB:          db,
		Resolver:            fakeContextResolver{ctx: materializerRuntimeContext()},
		Loader:              loader,
		MaterializerVersion: "materializer-test",
	}

	if err := mat.EnsureTable(context.Background(), RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}, "SpellName"); err != nil {
		t.Fatalf("EnsureTable: %v", err)
	}
	if loader.calls != 0 {
		t.Fatalf("LoadDB2Table calls = %d, want 0", loader.calls)
	}
}

func TestDB2MaterializerDoesNotRecordSuccessWhenParquetWriteFails(t *testing.T) {
	db := openMaterializerTestDB(t)
	fail := errors.New("write failed")
	mat := &DB2Materializer{
		CacheRoot:           t.TempDir(),
		MetadataDB:          db,
		Resolver:            fakeContextResolver{ctx: materializerRuntimeContext()},
		Loader:              fakeTableLoader{loaded: materializerLoadedTable()},
		MaterializerVersion: "materializer-test",
		WriteParquet: func(string, cacheparquet.Metadata, LoadedDB2Table) error {
			return fail
		},
	}

	err := mat.EnsureTable(context.Background(), RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}, "SpellName")
	if !errors.Is(err, fail) {
		t.Fatalf("EnsureTable error = %v, want %v", err, fail)
	}

	_, err = metadata.GetMaterializedTable(db, "cn", "wow", "build-key", "zhCN", "SpellName")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("materialized record error = %v, want sql.ErrNoRows", err)
	}
}

func openMaterializerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open metadata db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func materializerRuntimeContext() *appruntime.Context {
	return &appruntime.Context{
		Region:      "cn",
		Product:     "wow",
		BuildKey:    "build-key",
		BuildName:   "12.0.0.61234",
		Locale:      "zhCN",
		TablesReady: map[string]bool{},
	}
}

func materializerLoadedTable() LoadedDB2Table {
	return LoadedDB2Table{
		DB2FileDataID:     123,
		DBDDefinitionHash: "dbd-hash",
		DecoderVersion:    "decoder-v1",
		RowCount:          42,
		Schema: []cacheparquet.Field{
			{Name: "ID", Type: "uint32"},
			{Name: "Name_lang", Type: "string"},
		},
		Rows: []map[string]interface{}{
			{"ID": uint32(123), "Name_lang": "Fireball"},
		},
	}
}

type fakeContextResolver struct {
	ctx *appruntime.Context
	err error
}

func (f fakeContextResolver) ResolveContext(context.Context, RequestContext) (*appruntime.Context, error) {
	return f.ctx, f.err
}

type fakeTableLoader struct {
	loaded LoadedDB2Table
	err    error
}

func (f fakeTableLoader) LoadDB2Table(context.Context, *appruntime.Context, string) (LoadedDB2Table, error) {
	return f.loaded, f.err
}

type countingTableLoader struct {
	loaded LoadedDB2Table
	err    error
	calls  int
}

func (f *countingTableLoader) LoadDB2Table(context.Context, *appruntime.Context, string) (LoadedDB2Table, error) {
	f.calls++
	return f.loaded, f.err
}
