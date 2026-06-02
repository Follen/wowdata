package service

import (
	"context"
	"database/sql"
	"fmt"

	"wowdata/internal/server/storage/metadata"
)

type MetadataQueryService struct {
	metadataDBPath string
	db             *sql.DB
}

func NewMetadataQueryService(metadataDBPath string) *MetadataQueryService {
	return &MetadataQueryService{metadataDBPath: metadataDBPath}
}

func NewMetadataQueryServiceWithDB(db *sql.DB) *MetadataQueryService {
	return &MetadataQueryService{db: db}
}

func (s *MetadataQueryService) Tables(ctx context.Context, req TablesRequest) (TableCatalog, error) {
	db, closeDB, err := s.openDB()
	if err != nil {
		return TableCatalog{}, err
	}
	if closeDB {
		defer db.Close()
	}
	buildKey := req.Context.BuildKey
	if buildKey == "" {
		active, err := metadata.ActiveBuild(ctx, db, req.Context.Region, req.Context.Product, req.Context.Locale)
		if err != nil {
			return TableCatalog{}, fmt.Errorf("active build is required for %s/%s/%s: %w", req.Context.Region, req.Context.Product, req.Context.Locale, err)
		}
		buildKey = active.Key.BuildKey
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

func (s *MetadataQueryService) Schema(context.Context, SchemaRequest) (Schema, error) {
	return Schema{}, newCapabilityUnavailableError("query engine")
}

func (s *MetadataQueryService) Rows(context.Context, QueryRowsRequest) ([]map[string]interface{}, error) {
	return nil, newCapabilityUnavailableError("query engine")
}

func (s *MetadataQueryService) Search(context.Context, SearchRequest) ([]map[string]interface{}, error) {
	return nil, newCapabilityUnavailableError("query engine")
}

func (s *MetadataQueryService) ForeignKey(context.Context, ForeignKeyRequest) ([]map[string]interface{}, error) {
	return nil, newCapabilityUnavailableError("query engine")
}

func (s *MetadataQueryService) Stream(context.Context, StreamRequest) ([]map[string]interface{}, error) {
	return nil, newCapabilityUnavailableError("query engine")
}

func (s *MetadataQueryService) openDB() (*sql.DB, bool, error) {
	if s.db != nil {
		return s.db, false, nil
	}
	db, err := metadata.Open(s.metadataDBPath)
	return db, true, err
}
