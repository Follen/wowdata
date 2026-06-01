package parquet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	cacheparquet "wowdata/internal/cache/parquet"
	"wowdata/internal/server/storage/metadata"
)

type TableSpec struct {
	Key                 metadata.TableKey
	DB2FileDataID       int
	DBDHash             string
	DecoderVersion      string
	MaterializerVersion string
	ParquetPath         string
	Schema              []cacheparquet.Field
}

type DecodedTable struct {
	Rows    []map[string]interface{}
	Release func()
}

type TableDecoder interface {
	DecodeTable(context.Context, TableSpec, string) (DecodedTable, error)
}

type RowWriter interface {
	WriteRows(string, cacheparquet.Metadata, []cacheparquet.Field, []map[string]interface{}) error
}

type tempObserver interface {
	ObserveTemp(string)
}

type Materializer struct {
	db               *sql.DB
	decoder          TableDecoder
	writer           RowWriter
	validateExisting func(string, cacheparquet.Metadata) (cacheparquet.Metadata, error)
}

type Result struct {
	Reused      bool
	ParquetPath string
	RowCount    int
}

func NewMaterializer(db *sql.DB, decoder TableDecoder, writer RowWriter) Materializer {
	return Materializer{db: db, decoder: decoder, writer: writer, validateExisting: cacheparquet.ValidateExisting}
}

func (m Materializer) Materialize(ctx context.Context, spec TableSpec) (Result, error) {
	if m.db == nil {
		return Result{}, errors.New("metadata db is required")
	}
	if m.decoder == nil {
		return Result{}, errors.New("table decoder is required")
	}
	if m.writer == nil {
		return Result{}, errors.New("row writer is required")
	}
	if m.validateExisting == nil {
		m.validateExisting = cacheparquet.ValidateExisting
	}

	want := parquetMetadata(spec)
	table := materializedTable(spec, metadata.StatePreparing, 0)

	staleReason := ""
	if existing, err := metadata.LatestValidMaterializedTable(ctx, m.db, metadata.TableLookup{
		Region: spec.Key.Region, Product: spec.Key.Product, Locale: spec.Key.Locale, TableName: spec.Key.TableName,
	}); err == nil && existing.Key == spec.Key && existing.ParquetPath == spec.ParquetPath {
		if _, err := m.validateExisting(existing.ParquetPath, want); err == nil {
			return Result{Reused: true, ParquetPath: existing.ParquetPath, RowCount: existing.RowCount}, nil
		} else {
			staleReason = fmt.Sprintf("stale parquet metadata: %v", err)
			if markErr := metadata.MarkMaterializedTableState(ctx, m.db, spec.Key, metadata.StateStale, staleReason); markErr != nil {
				return Result{}, markErr
			}
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}

	if staleReason == "" {
		table.State = metadata.StatePreparing
		if err := metadata.UpsertMaterializedTable(ctx, m.db, table); err != nil {
			return Result{}, err
		}
	}

	decoded, err := m.decoder.DecodeTable(ctx, spec, staleReason)
	if err != nil {
		_ = metadata.MarkMaterializedTableState(ctx, m.db, spec.Key, metadata.StateFailed, err.Error())
		return Result{}, err
	}

	if err := m.writeAtomically(spec.ParquetPath, want, spec.Schema, decoded.Rows); err != nil {
		_ = metadata.MarkMaterializedTableState(ctx, m.db, spec.Key, metadata.StateFailed, err.Error())
		return Result{}, err
	}
	rowCount := len(decoded.Rows)
	if decoded.Release != nil {
		decoded.Release()
		decoded.Rows = nil
	}
	if _, err := m.validateExisting(spec.ParquetPath, want); err != nil {
		_ = metadata.MarkMaterializedTableState(ctx, m.db, spec.Key, metadata.StateFailed, err.Error())
		return Result{}, err
	}

	table.State = metadata.StateValid
	table.RowCount = rowCount
	if err := metadata.UpsertMaterializedTable(ctx, m.db, table); err != nil {
		return Result{}, err
	}
	return Result{ParquetPath: spec.ParquetPath, RowCount: table.RowCount}, nil
}

func (m Materializer) writeAtomically(path string, meta cacheparquet.Metadata, schema []cacheparquet.Field, rows []map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if observer, ok := m.writer.(tempObserver); ok {
		observer.ObserveTemp(tmpPath)
	}
	if err := m.writer.WriteRows(tmpPath, meta, schema, rows); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

type parquetRowWriter struct{}

func (parquetRowWriter) WriteRows(path string, meta cacheparquet.Metadata, schema []cacheparquet.Field, rows []map[string]interface{}) error {
	return cacheparquet.WriteRowsFile(path, meta, schema, rows)
}

func parquetMetadata(spec TableSpec) cacheparquet.Metadata {
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

func materializedTable(spec TableSpec, state string, rowCount int) metadata.MaterializedTable {
	return metadata.MaterializedTable{
		Key:                 spec.Key,
		DB2FileDataID:       spec.DB2FileDataID,
		DBDHash:             spec.DBDHash,
		DecoderVersion:      spec.DecoderVersion,
		MaterializerVersion: spec.MaterializerVersion,
		ParquetPath:         spec.ParquetPath,
		RowCount:            rowCount,
		State:               state,
	}
}
