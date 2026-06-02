package http

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"wowdata/internal/cache/metadata"
	cacheparquet "wowdata/internal/cache/parquet"
	appruntime "wowdata/internal/local/runtime"
)

type Materializer interface {
	EnsureTable(ctx context.Context, rc RequestContext, table string) error
}

type ContextResolver interface {
	ResolveContext(ctx context.Context, rc RequestContext) (*appruntime.Context, error)
}

type TableLoader interface {
	LoadDB2Table(ctx context.Context, runtimeCtx *appruntime.Context, table string) (LoadedDB2Table, error)
}

type LoadedDB2Table struct {
	DB2FileDataID     int
	DBDDefinitionHash string
	DecoderVersion    string
	RowCount          int
	Schema            []cacheparquet.Field
	Rows              []map[string]interface{}
}

type DB2Materializer struct {
	CacheRoot             string
	MetadataDB            *sql.DB
	Resolver              ContextResolver
	Loader                TableLoader
	MaterializerVersion   string
	MigrationsDescription string
	WriteParquet          func(path string, meta cacheparquet.Metadata, loaded LoadedDB2Table) error
	ValidateParquet       func(path string, want cacheparquet.Metadata) (cacheparquet.Metadata, error)
	BeforeLoad            func(ctx context.Context, rc RequestContext, runtimeCtx *appruntime.Context) error
}

func (m *DB2Materializer) EnsureTable(ctx context.Context, rc RequestContext, table string) error {
	table = strings.TrimSpace(table)
	if table == "" {
		return fmt.Errorf("table is required")
	}
	if m == nil {
		return NewCapabilityError("query_engine_unavailable", "DB2 materializer")
	}
	if m.Resolver == nil {
		return fmt.Errorf("context resolver is required")
	}
	if m.Loader == nil {
		return fmt.Errorf("DB2 table loader is required")
	}
	if m.MetadataDB == nil {
		return fmt.Errorf("metadata database is required")
	}
	runtimeCtx, err := m.Resolver.ResolveContext(ctx, rc)
	if err != nil {
		return err
	}
	if runtimeCtx == nil {
		return fmt.Errorf("resolved context is nil")
	}
	if m.BeforeLoad != nil {
		if err := m.BeforeLoad(ctx, rc, runtimeCtx); err != nil {
			return err
		}
	}
	if ok, err := m.reuseExisting(runtimeCtx, table); err != nil {
		return err
	} else if ok {
		return nil
	}
	loaded, err := m.Loader.LoadDB2Table(ctx, runtimeCtx, table)
	if err != nil {
		return err
	}

	meta := cacheparquet.Metadata{
		Region:              runtimeCtx.Region,
		Product:             runtimeCtx.Product,
		BuildKey:            runtimeCtx.BuildKey,
		Locale:              runtimeCtx.Locale,
		Table:               table,
		DB2FileDataID:       loaded.DB2FileDataID,
		DBDDefinitionHash:   loaded.DBDDefinitionHash,
		DecoderVersion:      loaded.DecoderVersion,
		MaterializerVersion: m.materializerVersion(),
	}
	path := cacheparquet.PathFor(m.CacheRoot, meta)
	if err := m.writeParquet(path, meta, loaded); err != nil {
		return err
	}
	if _, err := m.validateParquet(path, meta); err != nil {
		return err
	}
	if err := metadata.UpsertMaterializedTable(m.MetadataDB, metadata.MaterializedTable{
		Region:              meta.Region,
		Product:             meta.Product,
		BuildKey:            meta.BuildKey,
		BuildName:           runtimeCtx.BuildName,
		Locale:              meta.Locale,
		TableName:           meta.Table,
		DB2FileDataID:       meta.DB2FileDataID,
		DBDDefinitionHash:   meta.DBDDefinitionHash,
		DecoderVersion:      meta.DecoderVersion,
		MaterializerVersion: meta.MaterializerVersion,
		ParquetPath:         path,
		RowCount:            loaded.RowCount,
		State:               metadata.StateValid,
	}); err != nil {
		return err
	}
	if runtimeCtx.TablesReady != nil {
		runtimeCtx.TablesReady[table] = true
	}
	return nil
}

func (m *DB2Materializer) reuseExisting(runtimeCtx *appruntime.Context, table string) (bool, error) {
	record, err := metadata.GetMaterializedTable(m.MetadataDB, runtimeCtx.Region, runtimeCtx.Product, runtimeCtx.BuildKey, runtimeCtx.Locale, table)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if record.State != metadata.StateValid || record.MaterializerVersion != m.materializerVersion() {
		return false, nil
	}
	want := cacheparquet.Metadata{
		Region:              record.Region,
		Product:             record.Product,
		BuildKey:            record.BuildKey,
		Locale:              record.Locale,
		Table:               record.TableName,
		DB2FileDataID:       record.DB2FileDataID,
		DBDDefinitionHash:   record.DBDDefinitionHash,
		DecoderVersion:      record.DecoderVersion,
		MaterializerVersion: record.MaterializerVersion,
	}
	if _, err := m.validateParquet(record.ParquetPath, want); err != nil {
		_ = metadata.MarkMaterializedTableStale(m.MetadataDB, record.Region, record.Product, record.BuildKey, record.Locale, record.TableName)
		return false, nil
	}
	if runtimeCtx.TablesReady != nil {
		runtimeCtx.TablesReady[table] = true
	}
	return true, nil
}

func (m *DB2Materializer) materializerVersion() string {
	if m.MaterializerVersion != "" {
		return m.MaterializerVersion
	}
	return "http-materializer-v1"
}

func (m *DB2Materializer) writeParquet(path string, meta cacheparquet.Metadata, loaded LoadedDB2Table) error {
	if m.WriteParquet != nil {
		return m.WriteParquet(path, meta, loaded)
	}
	return cacheparquet.WriteRowsFile(path, meta, loaded.Schema, loaded.Rows)
}

func (m *DB2Materializer) validateParquet(path string, meta cacheparquet.Metadata) (cacheparquet.Metadata, error) {
	if m.ValidateParquet != nil {
		return m.ValidateParquet(path, meta)
	}
	return cacheparquet.ValidateExisting(path, meta)
}
