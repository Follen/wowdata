package db2

import (
	"fmt"
	"strings"

	appruntime "wowdata/internal/local/runtime"
)

type Query struct {
	Table  string
	IDs    []uint32
	Fields []string
	Filter string
	Limit  int
}

type Service struct {
	store appruntime.DB2Store
}

func NewService(store appruntime.DB2Store) *Service {
	return &Service{store: store}
}

func (s *Service) Rows(query Query) ([]map[string]interface{}, error) {
	table := strings.TrimSpace(query.Table)
	if table == "" {
		return nil, fmt.Errorf("table is required")
	}
	if s.store == nil {
		return nil, fmt.Errorf("db2 store is required")
	}
	return s.store.Rows(table, query.IDs, query.Fields, query.Filter, query.Limit)
}

func (s *Service) Schema(table string) ([]appruntime.SchemaField, int, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		return nil, 0, fmt.Errorf("table is required")
	}
	if s.store == nil {
		return nil, 0, fmt.Errorf("db2 store is required")
	}
	return s.store.Schema(table)
}
