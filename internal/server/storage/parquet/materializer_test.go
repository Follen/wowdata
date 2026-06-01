package parquet

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cacheparquet "wowdata/internal/cache/parquet"
	"wowdata/internal/server/storage/metadata"
)

func TestMaterializeSkipsDecodeWhenMetadataAndParquetAreValid(t *testing.T) {
	ctx := context.Background()
	db := openMetadataDB(t)
	root := t.TempDir()
	spec := testSpec(root)

	if err := cacheparquet.WriteMetadataFile(spec.ParquetPath, wantParquetMetadata(spec)); err != nil {
		t.Fatalf("write existing parquet metadata: %v", err)
	}
	if err := metadata.UpsertMaterializedTable(ctx, db, wantMaterializedTable(spec, metadata.StateValid, 3)); err != nil {
		t.Fatalf("upsert valid materialized table: %v", err)
	}

	decoder := &fakeDecoder{err: errors.New("decode should not be called")}
	writer := &observingWriter{}
	result, err := NewMaterializer(db, decoder, writer).Materialize(ctx, spec)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if !result.Reused {
		t.Fatalf("result reused = false, want true")
	}
	if decoder.calls != 0 {
		t.Fatalf("decode calls = %d, want 0", decoder.calls)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0", writer.calls)
	}
}

func TestMaterializeInvalidFooterMarksExistingMetadataStale(t *testing.T) {
	ctx := context.Background()
	db := openMetadataDB(t)
	root := t.TempDir()
	spec := testSpec(root)

	wrong := wantParquetMetadata(spec)
	wrong.DBDDefinitionHash = "old-dbd"
	if err := cacheparquet.WriteMetadataFile(spec.ParquetPath, wrong); err != nil {
		t.Fatalf("write wrong parquet metadata: %v", err)
	}
	if err := metadata.UpsertMaterializedTable(ctx, db, wantMaterializedTable(spec, metadata.StateValid, 3)); err != nil {
		t.Fatalf("upsert stale materialized table: %v", err)
	}

	decoder := &fakeDecoder{
		rows: []map[string]interface{}{{"ID": int32(1), "Name": "Renewed"}},
		duringDecode: func() error {
			if got := materializedState(t, db, spec.Key); got != metadata.StateStale {
				return errors.New("state during decode = " + got + ", want stale")
			}
			return nil
		},
	}
	writer := &observingWriter{write: cacheparquet.WriteRowsFile}
	if _, err := NewMaterializer(db, decoder, writer).Materialize(ctx, spec); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	got := materializedState(t, db, spec.Key)
	if got != metadata.StateValid {
		t.Fatalf("final table state = %q, want valid after rematerialize", got)
	}
	if decoder.calls != 1 {
		t.Fatalf("decode calls = %d, want 1", decoder.calls)
	}
	if !strings.Contains(decoder.lastStaleReason, "stale") {
		t.Fatalf("stale reason = %q, want footer stale reason passed to decoder", decoder.lastStaleReason)
	}
}

func TestMaterializeWriterUsesTempFileAndRename(t *testing.T) {
	ctx := context.Background()
	db := openMetadataDB(t)
	root := t.TempDir()
	spec := testSpec(root)
	decoder := &fakeDecoder{rows: []map[string]interface{}{{"ID": int32(1), "Name": "Atomic"}}}
	writer := &observingWriter{write: cacheparquet.WriteRowsFile}

	if _, err := NewMaterializer(db, decoder, writer).Materialize(ctx, spec); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want 1", writer.calls)
	}
	if !writer.sawTemp {
		t.Fatalf("writer did not observe temp file during write")
	}
	if _, err := os.Stat(spec.ParquetPath); err != nil {
		t.Fatalf("final parquet path missing after rename: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(spec.ParquetPath), "."+filepath.Base(spec.ParquetPath)+".tmp-*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left after rename: %#v", matches)
	}
}

func TestMaterializeReleasesDecodedRowsAfterWrite(t *testing.T) {
	ctx := context.Background()
	db := openMetadataDB(t)
	root := t.TempDir()
	spec := testSpec(root)
	rows := &rowBuffer{values: []map[string]interface{}{{"ID": int32(1), "Name": "Released"}}}
	decoder := &fakeDecoder{buffer: rows}
	writer := &observingWriter{write: cacheparquet.WriteRowsFile}

	if _, err := NewMaterializer(db, decoder, writer).Materialize(ctx, spec); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if !rows.released {
		t.Fatalf("decoded rows were not released")
	}
	if rows.values != nil {
		t.Fatalf("row values = %#v, want nil after release", rows.values)
	}
}

type fakeDecoder struct {
	rows            []map[string]interface{}
	buffer          *rowBuffer
	err             error
	calls           int
	lastStaleReason string
	duringDecode    func() error
}

func (d *fakeDecoder) DecodeTable(_ context.Context, _ TableSpec, staleReason string) (DecodedTable, error) {
	d.calls++
	d.lastStaleReason = staleReason
	if d.duringDecode != nil {
		if err := d.duringDecode(); err != nil {
			return DecodedTable{}, err
		}
	}
	if d.err != nil {
		return DecodedTable{}, d.err
	}
	if d.buffer != nil {
		return DecodedTable{Rows: d.buffer.values, Release: d.buffer.Release}, nil
	}
	return DecodedTable{Rows: d.rows}, nil
}

type rowBuffer struct {
	values   []map[string]interface{}
	released bool
}

func (b *rowBuffer) Release() {
	b.released = true
	b.values = nil
}

type observingWriter struct {
	write   func(string, cacheparquet.Metadata, []cacheparquet.Field, []map[string]interface{}) error
	calls   int
	sawTemp bool
}

func (w *observingWriter) WriteRows(path string, meta cacheparquet.Metadata, schema []cacheparquet.Field, rows []map[string]interface{}) error {
	w.calls++
	if w.write == nil {
		return nil
	}
	return w.write(path, meta, schema, rows)
}

func (w *observingWriter) ObserveTemp(path string) {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if len(matches) > 0 {
		w.sawTemp = true
	}
}

func testSpec(root string) TableSpec {
	return TableSpec{
		Key: metadata.TableKey{
			Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", TableName: "Spell",
		},
		DB2FileDataID:       123,
		DBDHash:             "dbd-a",
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         filepath.Join(root, "db2", "us", "wow", "build-1", "enUS", "Spell.parquet"),
		Schema: []cacheparquet.Field{
			{Name: "ID", Type: "int32"},
			{Name: "Name", Type: "string"},
		},
	}
}

func wantParquetMetadata(spec TableSpec) cacheparquet.Metadata {
	return cacheparquet.Metadata{
		Region:              spec.Key.Region,
		Product:             spec.Key.Product,
		BuildKey:            spec.Key.BuildKey,
		Locale:              spec.Key.Locale,
		Table:               spec.Key.TableName,
		DB2FileDataID:       spec.DB2FileDataID,
		DBDDefinitionHash:   spec.DBDHash,
		DecoderVersion:      spec.DecoderVersion,
		MaterializerVersion: spec.MaterializerVersion,
	}
}

func wantMaterializedTable(spec TableSpec, state string, rows int) metadata.MaterializedTable {
	return metadata.MaterializedTable{
		Key:                 spec.Key,
		DB2FileDataID:       spec.DB2FileDataID,
		DBDHash:             spec.DBDHash,
		DecoderVersion:      spec.DecoderVersion,
		MaterializerVersion: spec.MaterializerVersion,
		ParquetPath:         spec.ParquetPath,
		RowCount:            rows,
		State:               state,
	}
}

func materializedState(t *testing.T, db *sql.DB, key metadata.TableKey) string {
	t.Helper()
	var state string
	err := db.QueryRow(`
SELECT state FROM server_materialized_tables
WHERE region = ? AND product = ? AND locale = ? AND build_key = ? AND table_name = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey, key.TableName,
	).Scan(&state)
	if err != nil {
		t.Fatalf("query materialized state: %v", err)
	}
	return state
}

func openMetadataDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
