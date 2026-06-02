package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wowdata/internal/server/storage/metadata"
)

func TestMetadataQueryServiceTablesReadsRequestedBuildCatalog(t *testing.T) {
	ctx := context.Background()
	db := openMetadataQueryTestDB(t)
	build1 := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"}
	build2 := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2"}
	seedReadyBuild(t, ctx, db, build1)
	seedReadyBuild(t, ctx, db, build2)
	if err := metadata.ActivateBuild(ctx, db, build1); err != nil {
		t.Fatalf("activate build-1: %v", err)
	}
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

func TestMetadataQueryServiceRejectsExplicitNonActiveCandidateBuild(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openMetadataQueryTestDB(t)
	active := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"}
	candidate := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "candidate-build"}
	seedReadyBuild(t, ctx, db, active)
	if err := metadata.ActivateBuild(ctx, db, active); err != nil {
		t.Fatalf("activate build: %v", err)
	}
	if err := metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{Key: candidate, BuildName: "candidate-build", State: metadata.StatePreparing}); err != nil {
		t.Fatalf("upsert candidate build: %v", err)
	}
	seedMaterializedTableAtPath(t, ctx, db, candidate, "Item", filepath.Join(root, "db2", "candidate", "Item.parquet"), 1)
	engine := &fakeParquetQueryEngine{}

	_, err := NewMetadataQueryServiceForTest(db, root, engine).Stream(ctx, StreamRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "candidate-build"},
		Table:   "Item",
		Limit:   1,
	})
	if err == nil {
		t.Fatal("Stream explicit candidate build error = nil, want not active error")
	}
	if !strings.Contains(err.Error(), "active build") {
		t.Fatalf("Stream error = %q, want active build message", err.Error())
	}
	if len(engine.calls) != 0 {
		t.Fatalf("engine calls = %#v, want none for non-active candidate", engine.calls)
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

func TestMetadataQueryServiceSchemaUsesMaterializedParquetAndReturnsOrderedFields(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openMetadataQueryTestDB(t)
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"}
	seedActiveReadyBuild(t, ctx, db, key)
	parquetPath := seedMaterializedTableAtPath(t, ctx, db, key, "SpellName", filepath.Join(root, "db2", "SpellName.parquet"), 42)
	engine := &fakeParquetQueryEngine{
		results: [][]map[string]interface{}{{
			{"column_name": "ID", "column_type": "INTEGER"},
			{"column_name": "Name_lang", "column_type": "VARCHAR"},
		}},
	}

	schema, err := NewMetadataQueryServiceForTest(db, root, engine).Schema(ctx, SchemaRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"},
		Table:   "SpellName",
	})
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if schema.Table != "SpellName" || schema.RowCount != 42 {
		t.Fatalf("schema table/rowCount = %q/%d, want SpellName/42", schema.Table, schema.RowCount)
	}
	if len(schema.Fields) != 2 || schema.Fields[0] != (Field{Name: "ID", Type: "INTEGER"}) || schema.Fields[1] != (Field{Name: "Name_lang", Type: "VARCHAR"}) {
		t.Fatalf("fields = %#v, want ordered DuckDB describe fields", schema.Fields)
	}
	call := engine.calls[0]
	if !strings.Contains(call.query, `DESCRIBE SELECT * FROM read_parquet(?) AS "SpellName"`) {
		t.Fatalf("schema SQL = %q, want parameterized describe", call.query)
	}
	if len(call.args) != 1 || call.args[0] != parquetPath {
		t.Fatalf("schema args = %#v, want parquet path arg %q", call.args, parquetPath)
	}
}

func TestMetadataQueryServiceRowsBuildsBoundedParameterizedQuery(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openMetadataQueryTestDB(t)
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"}
	seedActiveReadyBuild(t, ctx, db, key)
	parquetPath := seedMaterializedTableAtPath(t, ctx, db, key, "ItemSparse", filepath.Join(root, "db2", "ItemSparse.parquet"), 2)
	engine := &fakeParquetQueryEngine{results: [][]map[string]interface{}{{{"ID": uint64(19019)}}}}

	rows, err := NewMetadataQueryServiceForTest(db, root, engine).Rows(ctx, QueryRowsRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"},
		Table:   "ItemSparse",
		IDs:     []uint64{19019},
		IDField: `ID`,
		Fields:  []string{"ID", "Display_lang"},
		Limit:   10,
		Offset:  5,
	})
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != 1 || rows[0]["ID"] != uint64(19019) {
		t.Fatalf("rows = %#v", rows)
	}
	call := engine.calls[0]
	for _, want := range []string{
		`SELECT "ID", "Display_lang" FROM read_parquet(?) AS "ItemSparse"`,
		`WHERE "ID" = ?`,
		`LIMIT ? OFFSET ?`,
	} {
		if !strings.Contains(call.query, want) {
			t.Fatalf("rows SQL = %q, missing %q", call.query, want)
		}
	}
	if strings.Contains(call.query, "19019") {
		t.Fatalf("rows SQL = %q, must not inline ID value", call.query)
	}
	wantArgs := []interface{}{parquetPath, uint64(19019), 10, 5}
	if !interfaceSlicesEqual(call.args, wantArgs) {
		t.Fatalf("rows args = %#v, want %#v", call.args, wantArgs)
	}
}

func TestMetadataQueryServiceRowsSupportsMultipleIDsWithoutInlining(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openMetadataQueryTestDB(t)
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"}
	seedActiveReadyBuild(t, ctx, db, key)
	parquetPath := seedMaterializedTableAtPath(t, ctx, db, key, "ItemSparse", filepath.Join(root, "db2", "ItemSparse.parquet"), 2)
	engine := &fakeParquetQueryEngine{results: [][]map[string]interface{}{{}}}

	if _, err := NewMetadataQueryServiceForTest(db, root, engine).Rows(ctx, QueryRowsRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"},
		Table:   "ItemSparse",
		IDs:     []uint64{19019, 19020},
		IDField: "ID",
		Limit:   2,
	}); err != nil {
		t.Fatalf("Rows: %v", err)
	}
	call := engine.calls[0]
	if !strings.Contains(call.query, `WHERE "ID" IN (?, ?)`) {
		t.Fatalf("rows SQL = %q, want parameterized IN list", call.query)
	}
	if strings.Contains(call.query, "19019") || strings.Contains(call.query, "19020") {
		t.Fatalf("rows SQL = %q, must not inline ID values", call.query)
	}
	if !interfaceSlicesEqual(call.args, []interface{}{parquetPath, uint64(19019), uint64(19020), 2}) {
		t.Fatalf("rows args = %#v", call.args)
	}
}

func TestMetadataQueryServiceRowsRejectsUnsafeFilter(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openMetadataQueryTestDB(t)
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"}
	seedActiveReadyBuild(t, ctx, db, key)
	seedMaterializedTableAtPath(t, ctx, db, key, "ItemSparse", filepath.Join(root, "db2", "ItemSparse.parquet"), 2)
	engine := &fakeParquetQueryEngine{}

	_, err := NewMetadataQueryServiceForTest(db, root, engine).Rows(ctx, QueryRowsRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"},
		Table:   "ItemSparse",
		Filter:  `ID = 1 OR 1=1`,
	})
	if err == nil {
		t.Fatal("Rows unsafe filter error = nil")
	}
	if len(engine.calls) != 0 {
		t.Fatalf("engine calls = %#v, want none for unsafe filter", engine.calls)
	}
}

func TestMetadataQueryServiceSearchForeignKeyAndStreamDispatchSafeQueries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openMetadataQueryTestDB(t)
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"}
	seedActiveReadyBuild(t, ctx, db, key)
	parquetPath := seedMaterializedTableAtPath(t, ctx, db, key, "SpellName", filepath.Join(root, "db2", "SpellName.parquet"), 2)
	engine := &fakeParquetQueryEngine{results: [][]map[string]interface{}{{}, {}, {}}}
	svc := NewMetadataQueryServiceForTest(db, root, engine)

	if _, err := svc.Search(ctx, SearchRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"},
		Table:   "SpellName",
		Field:   "Name_lang",
		Query:   `frost%' OR 1=1 --`,
		Limit:   3,
	}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := svc.ForeignKey(ctx, ForeignKeyRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"},
		Table:   "SpellName",
		Field:   "SpellID",
		Value:   `1 OR 1=1`,
		Limit:   4,
	}); err != nil {
		t.Fatalf("ForeignKey: %v", err)
	}
	if _, err := svc.Stream(ctx, StreamRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"},
		Table:   "SpellName",
		Limit:   5,
		Offset:  6,
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	search := engine.calls[0]
	if !strings.Contains(search.query, `WHERE "Name_lang" ILIKE ?`) || strings.Contains(search.query, "frost") {
		t.Fatalf("search SQL = %q, want parameterized ILIKE", search.query)
	}
	if !interfaceSlicesEqual(search.args, []interface{}{parquetPath, `%frost%' OR 1=1 --%`, 3}) {
		t.Fatalf("search args = %#v", search.args)
	}
	fk := engine.calls[1]
	if !strings.Contains(fk.query, `WHERE "SpellID" = ?`) || strings.Contains(fk.query, "OR 1=1") {
		t.Fatalf("foreign key SQL = %q, want parameterized equality", fk.query)
	}
	if !interfaceSlicesEqual(fk.args, []interface{}{parquetPath, `1 OR 1=1`, 4}) {
		t.Fatalf("foreign key args = %#v", fk.args)
	}
	stream := engine.calls[2]
	if !strings.Contains(stream.query, `SELECT * FROM read_parquet(?) AS "SpellName" LIMIT ? OFFSET ?`) {
		t.Fatalf("stream SQL = %q", stream.query)
	}
	if !interfaceSlicesEqual(stream.args, []interface{}{parquetPath, 5, 6}) {
		t.Fatalf("stream args = %#v", stream.args)
	}
}

func TestMetadataQueryServiceMissingTableReturnsClearError(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openMetadataQueryTestDB(t)
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "missing-build"}
	seedActiveReadyBuild(t, ctx, db, key)
	engine := &fakeParquetQueryEngine{}
	_, err := NewMetadataQueryServiceForTest(db, root, engine).Schema(ctx, SchemaRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "missing-build"},
		Table:   "Missing",
	})
	if err == nil {
		t.Fatal("Schema missing table error = nil")
	}
	if !strings.Contains(err.Error(), "materialized table Missing") || !strings.Contains(err.Error(), "missing-build") {
		t.Fatalf("Schema missing table error = %q", err.Error())
	}
	if len(engine.calls) != 0 {
		t.Fatalf("engine calls = %#v, want none for missing table", engine.calls)
	}
}

func TestMetadataQueryServiceQueryModesResolveProvidedAndActiveBuilds(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openMetadataQueryTestDB(t)
	active := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "active-build"}
	inactive := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "inactive-build"}
	seedReadyBuild(t, ctx, db, inactive)
	seedReadyBuild(t, ctx, db, active)
	if err := metadata.ActivateBuild(ctx, db, active); err != nil {
		t.Fatalf("activate build: %v", err)
	}
	activePath := seedMaterializedTableAtPath(t, ctx, db, active, "Item", filepath.Join(root, "db2", "active", "Item.parquet"), 1)
	inactivePath := seedMaterializedTableAtPath(t, ctx, db, inactive, "Item", filepath.Join(root, "db2", "inactive", "Item.parquet"), 1)
	engine := &fakeParquetQueryEngine{results: [][]map[string]interface{}{{}, {}}}
	svc := NewMetadataQueryServiceForTest(db, root, engine)

	if _, err := svc.Stream(ctx, StreamRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "inactive-build"},
		Table:   "Item",
		Limit:   1,
	}); err == nil {
		t.Fatal("Stream provided inactive buildKey error = nil, want not active")
	}
	if _, err := svc.Stream(ctx, StreamRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS"},
		Table:   "Item",
		Limit:   1,
	}); err != nil {
		t.Fatalf("Stream active build: %v", err)
	}
	if len(engine.calls) != 1 {
		t.Fatalf("engine calls = %#v, want only active query", engine.calls)
	}
	if engine.calls[0].args[0] != activePath {
		t.Fatalf("omitted build path = %#v, want active path %q; inactive path was %q", engine.calls[0].args[0], activePath, inactivePath)
	}
}

func TestMetadataQueryServiceQueryModeWithoutBuildKeyRequiresActiveBuild(t *testing.T) {
	_, err := NewMetadataQueryServiceForTest(openMetadataQueryTestDB(t), t.TempDir(), &fakeParquetQueryEngine{}).Stream(context.Background(), StreamRequest{
		Context: RequestContext{Region: "us", Product: "wow", Locale: "enUS"},
		Table:   "Item",
	})
	if err == nil {
		t.Fatal("Stream without buildKey error = nil, want active build error")
	}
	if !strings.Contains(err.Error(), "active build") {
		t.Fatalf("Stream error = %q, want active build message", err.Error())
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

func seedActiveReadyBuild(t *testing.T, ctx context.Context, db *sql.DB, key metadata.BuildKey) {
	t.Helper()
	seedReadyBuild(t, ctx, db, key)
	if err := metadata.ActivateBuild(ctx, db, key); err != nil {
		t.Fatalf("activate build %s: %v", key.BuildKey, err)
	}
}

func seedMaterializedTable(t *testing.T, ctx context.Context, db *sql.DB, key metadata.BuildKey, table string) {
	t.Helper()
	seedMaterializedTableAtPath(t, ctx, db, key, table, "cache/db2/"+table+".parquet", 1)
}

func seedMaterializedTableAtPath(t *testing.T, ctx context.Context, db *sql.DB, key metadata.BuildKey, table string, parquetPath string, rowCount int) string {
	t.Helper()
	if err := ensureParentDirAndFile(parquetPath); err != nil {
		t.Fatalf("create parquet placeholder %s: %v", parquetPath, err)
	}
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key:                 metadata.TableKey{Region: key.Region, Product: key.Product, Locale: key.Locale, BuildKey: key.BuildKey, TableName: table},
		DB2FileDataID:       1,
		DBDHash:             "dbd-a",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         parquetPath,
		RowCount:            rowCount,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert table %s/%s: %v", key.BuildKey, table, err)
	}
	return filepath.Clean(parquetPath)
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

type fakeParquetQueryEngine struct {
	calls   []fakeParquetQueryCall
	results [][]map[string]interface{}
}

type fakeParquetQueryCall struct {
	query string
	args  []interface{}
}

func (f *fakeParquetQueryEngine) QueryParquet(_ context.Context, query string, args []interface{}) ([]map[string]interface{}, error) {
	f.calls = append(f.calls, fakeParquetQueryCall{query: query, args: append([]interface{}(nil), args...)})
	if len(f.results) == 0 {
		return nil, nil
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result, nil
}

func ensureParentDirAndFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, nil, 0644)
}

func interfaceSlicesEqual(a, b []interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
