package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"

	cacheduckdb "wowdata/internal/cache/duckdb"
	storageduckdb "wowdata/internal/server/storage/duckdb"
	"wowdata/internal/server/storage/metadata"
)

type parquetQueryEngine interface {
	QueryParquet(context.Context, string, []interface{}) ([]map[string]interface{}, error)
}

type MetadataQueryService struct {
	metadataDBPath string
	db             *sql.DB
	trustedRoot    string
	queryBuilder   storageduckdb.QueryBuilder
	queryEngine    parquetQueryEngine
}

func NewMetadataQueryService(metadataDBPath string) *MetadataQueryService {
	trustedRoot := filepath.Dir(metadataDBPath)
	return NewMetadataQueryServiceWithRuntime(metadataDBPath, "", trustedRoot)
}

func NewMetadataQueryServiceWithRuntime(metadataDBPath string, duckDBPath string, trustedRoot string) *MetadataQueryService {
	if trustedRoot == "" {
		trustedRoot = "."
	}
	return &MetadataQueryService{
		metadataDBPath: metadataDBPath,
		trustedRoot:    trustedRoot,
		queryBuilder:   storageduckdb.NewQueryBuilder(trustedRoot),
		queryEngine:    cacheduckdb.NewEngine(duckDBPath),
	}
}

func NewMetadataQueryServiceWithDBAndRuntime(db *sql.DB, duckDBPath string, trustedRoot string) *MetadataQueryService {
	if trustedRoot == "" {
		trustedRoot = "."
	}
	return &MetadataQueryService{
		db:           db,
		trustedRoot:  trustedRoot,
		queryBuilder: storageduckdb.NewQueryBuilder(trustedRoot),
		queryEngine:  cacheduckdb.NewEngine(duckDBPath),
	}
}

func NewMetadataQueryServiceWithDB(db *sql.DB) *MetadataQueryService {
	return NewMetadataQueryServiceForTest(db, ".", nil)
}

func NewMetadataQueryServiceForTest(db *sql.DB, trustedRoot string, engine parquetQueryEngine) *MetadataQueryService {
	if trustedRoot == "" {
		trustedRoot = "."
	}
	return &MetadataQueryService{
		db:           db,
		trustedRoot:  trustedRoot,
		queryBuilder: storageduckdb.NewQueryBuilder(trustedRoot),
		queryEngine:  engine,
	}
}

func (s *MetadataQueryService) Tables(ctx context.Context, req TablesRequest) (TableCatalog, error) {
	db, closeDB, err := s.openDB()
	if err != nil {
		return TableCatalog{}, err
	}
	if closeDB {
		defer db.Close()
	}
	buildKey, err := s.resolveBuildKey(ctx, db, req.Context)
	if err != nil {
		return TableCatalog{}, err
	}
	tables, err := metadata.ListValidMaterializedTables(ctx, db, metadata.TableCatalogLookup{
		Region:   req.Context.Region,
		Product:  req.Context.Product,
		Locale:   req.Context.Locale,
		BuildKey: buildKey,
	})
	if err != nil {
		return TableCatalog{}, err
	}
	out := make([]TableInfo, 0, len(tables))
	for _, table := range tables {
		out = append(out, TableInfo{Name: table.Key.TableName})
	}
	return TableCatalog{Tables: out}, nil
}

func (s *MetadataQueryService) Schema(ctx context.Context, req SchemaRequest) (Schema, error) {
	table, err := s.materializedTable(ctx, req.Context, req.Table)
	if err != nil {
		return Schema{}, err
	}
	query, args, err := s.queryBuilder.BuildSchemaSQL(s.tableRef(table))
	if err != nil {
		return Schema{}, err
	}
	rows, err := s.query(ctx, query, args)
	if err != nil {
		return Schema{}, err
	}
	fields := make([]Field, 0, len(rows))
	for _, row := range rows {
		fields = append(fields, Field{
			Name: stringFromRow(row, "column_name", "name"),
			Type: stringFromRow(row, "column_type", "type"),
		})
	}
	return Schema{Table: table.Key.TableName, RowCount: table.RowCount, Fields: fields}, nil
}

func (s *MetadataQueryService) Rows(ctx context.Context, req QueryRowsRequest) ([]map[string]interface{}, error) {
	if req.Filter != "" {
		return nil, fmt.Errorf("filter is not supported by the metadata query service; use ids or foreign-key")
	}
	table, err := s.materializedTable(ctx, req.Context, req.Table)
	if err != nil {
		return nil, err
	}
	idField := req.IDField
	if idField == "" {
		idField = "ID"
	}
	where := map[string]interface{}{}
	if len(req.IDs) == 1 {
		where[idField] = req.IDs[0]
	} else if len(req.IDs) > 1 {
		where[idField] = append([]uint64(nil), req.IDs...)
	}
	query, args, err := s.queryBuilder.BuildRowsSQL(s.tableRef(table), storageduckdb.RowsQuery{
		Columns: req.Fields,
		Where:   where,
		Limit:   req.Limit,
		Offset:  req.Offset,
	})
	if err != nil {
		return nil, err
	}
	return s.query(ctx, query, args)
}

func (s *MetadataQueryService) Search(ctx context.Context, req SearchRequest) ([]map[string]interface{}, error) {
	table, err := s.materializedTable(ctx, req.Context, req.Table)
	if err != nil {
		return nil, err
	}
	query, args, err := s.queryBuilder.BuildSearchSQL(s.tableRef(table), storageduckdb.SearchQuery{
		SearchColumn: req.Field,
		Pattern:      "%" + req.Query + "%",
		Limit:        req.Limit,
	})
	if err != nil {
		return nil, err
	}
	return s.query(ctx, query, args)
}

func (s *MetadataQueryService) ForeignKey(ctx context.Context, req ForeignKeyRequest) ([]map[string]interface{}, error) {
	table, err := s.materializedTable(ctx, req.Context, req.Table)
	if err != nil {
		return nil, err
	}
	query, args, err := s.queryBuilder.BuildForeignKeySQL(s.tableRef(table), storageduckdb.ForeignKeyQuery{
		Field: req.Field,
		Value: req.Value,
		Limit: req.Limit,
	})
	if err != nil {
		return nil, err
	}
	return s.query(ctx, query, args)
}

func (s *MetadataQueryService) Stream(ctx context.Context, req StreamRequest) ([]map[string]interface{}, error) {
	table, err := s.materializedTable(ctx, req.Context, req.Table)
	if err != nil {
		return nil, err
	}
	query, args, err := s.queryBuilder.BuildStreamSQL(s.tableRef(table), storageduckdb.StreamQuery{
		Columns: req.Fields,
		Limit:   req.Limit,
		Offset:  req.Offset,
	})
	if err != nil {
		return nil, err
	}
	return s.query(ctx, query, args)
}

func (s *MetadataQueryService) openDB() (*sql.DB, bool, error) {
	if s.db != nil {
		return s.db, false, nil
	}
	db, err := metadata.Open(s.metadataDBPath)
	return db, true, err
}

func (s *MetadataQueryService) materializedTable(ctx context.Context, reqCtx RequestContext, tableName string) (metadata.MaterializedTable, error) {
	db, closeDB, err := s.openDB()
	if err != nil {
		return metadata.MaterializedTable{}, err
	}
	if closeDB {
		defer db.Close()
	}
	buildKey, err := s.resolveBuildKey(ctx, db, reqCtx)
	if err != nil {
		return metadata.MaterializedTable{}, err
	}
	table, err := materializedTableForBuild(ctx, db, metadata.TableKey{
		Region:    reqCtx.Region,
		Product:   reqCtx.Product,
		Locale:    reqCtx.Locale,
		BuildKey:  buildKey,
		TableName: tableName,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return metadata.MaterializedTable{}, fmt.Errorf("valid materialized table %s not found for %s/%s/%s build %s", tableName, reqCtx.Region, reqCtx.Product, reqCtx.Locale, buildKey)
	}
	if err != nil {
		return metadata.MaterializedTable{}, err
	}
	return table, nil
}

func (s *MetadataQueryService) resolveBuildKey(ctx context.Context, db *sql.DB, reqCtx RequestContext) (string, error) {
	active, err := metadata.ActiveBuild(ctx, db, reqCtx.Region, reqCtx.Product, reqCtx.Locale)
	if err != nil {
		return "", fmt.Errorf("active build is required for %s/%s/%s: %w", reqCtx.Region, reqCtx.Product, reqCtx.Locale, err)
	}
	if reqCtx.BuildKey != "" {
		if reqCtx.BuildKey != active.Key.BuildKey {
			return "", fmt.Errorf("requested build %s is not the active build for %s/%s/%s", reqCtx.BuildKey, reqCtx.Region, reqCtx.Product, reqCtx.Locale)
		}
		return reqCtx.BuildKey, nil
	}
	return active.Key.BuildKey, nil
}

func materializedTableForBuild(ctx context.Context, db *sql.DB, key metadata.TableKey) (metadata.MaterializedTable, error) {
	var table metadata.MaterializedTable
	err := db.QueryRowContext(ctx, `
SELECT region, product, locale, build_key, table_name,
  db2_file_data_id, dbd_hash, decoder_version, materializer_version,
  parquet_path, row_count, state, error
FROM server_materialized_tables
WHERE region = ? AND product = ? AND locale = ? AND build_key = ? AND table_name = ? AND state = ?
ORDER BY updated_seq DESC
LIMIT 1`,
		key.Region,
		key.Product,
		key.Locale,
		key.BuildKey,
		key.TableName,
		metadata.StateValid,
	).Scan(
		&table.Key.Region,
		&table.Key.Product,
		&table.Key.Locale,
		&table.Key.BuildKey,
		&table.Key.TableName,
		&table.DB2FileDataID,
		&table.DBDHash,
		&table.DecoderVersion,
		&table.MaterializerVersion,
		&table.ParquetPath,
		&table.RowCount,
		&table.State,
		&table.Error,
	)
	return table, err
}

func (s *MetadataQueryService) tableRef(table metadata.MaterializedTable) storageduckdb.TableRef {
	return storageduckdb.TableRef{
		TableName:   table.Key.TableName,
		ParquetPath: s.resolveParquetPath(table.ParquetPath),
	}
}

func (s *MetadataQueryService) resolveParquetPath(path string) string {
	if filepath.IsAbs(path) || s.trustedRoot == "" {
		return path
	}
	return filepath.Join(s.trustedRoot, path)
}

func (s *MetadataQueryService) query(ctx context.Context, sql string, args []interface{}) ([]map[string]interface{}, error) {
	if s.queryEngine == nil {
		return nil, newCapabilityUnavailableError("query engine")
	}
	rows, err := s.queryEngine.QueryParquet(ctx, sql, args)
	if errors.Is(err, cacheduckdb.ErrUnavailable) {
		return nil, newCapabilityUnavailableError("query engine")
	}
	return rows, err
}

func stringFromRow(row map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok && value != nil {
			switch v := value.(type) {
			case string:
				return v
			case []byte:
				return string(v)
			default:
				return fmt.Sprint(v)
			}
		}
	}
	return ""
}
