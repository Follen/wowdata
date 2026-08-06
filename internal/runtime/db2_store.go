package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"wowdata/internal/db2"
)

type memoryDB2Table struct {
	schema []SchemaField
	reader db2.RowReader
}

type MemoryDB2Store struct {
	mu     sync.RWMutex
	tables map[string]memoryDB2Table
	engine *db2.Engine
	ready  bool
}

func NewMemoryDB2Store() *MemoryDB2Store {
	return &MemoryDB2Store{tables: make(map[string]memoryDB2Table), engine: db2.NewEngine()}
}

func (s *MemoryDB2Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tables = make(map[string]memoryDB2Table)
	s.engine = db2.NewEngine()
	s.ready = false
}

func (s *MemoryDB2Store) AddTable(name string, schema []SchemaField, reader db2.RowReader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tables[name] = memoryDB2Table{schema: schema, reader: reader}
	if s.engine == nil {
		s.engine = db2.NewEngine()
	}
	_ = s.engine.Register(name, nil, reader)
	s.ready = true
}

func (s *MemoryDB2Store) Ready() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready
}

func (s *MemoryDB2Store) Schema(table string) ([]SchemaField, int, error) {
	t, err := s.table(table)
	if err != nil {
		return nil, 0, err
	}
	count := 0
	if sized, ok := t.reader.(db2.SizedReader); ok {
		count = sized.Size()
	} else {
		count = len(t.reader.GetAllRows())
	}
	return t.schema, count, nil
}

func (s *MemoryDB2Store) Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	_, err := s.table(table)
	if err != nil {
		return nil, err
	}
	filterFn, err := parseFilter(filter)
	if err != nil {
		return nil, err
	}
	snapshot := s.engine.Snapshot()
	defer snapshot.Close()
	result, _, err := snapshot.Execute(context.Background(), db2.QueryPlan{Table: table, Mode: db2.PlanRows, IDs: ids, Fields: fields, Filter: filterFn, Limit: limit})
	if err != nil {
		return nil, err
	}
	return result.Rows, nil
}

func (s *MemoryDB2Store) Search(table string, field string, query string, limit int) ([]map[string]interface{}, error) {
	_, err := s.table(table)
	if err != nil {
		return nil, err
	}
	snapshot := s.engine.Snapshot()
	defer snapshot.Close()
	result, _, err := snapshot.Execute(context.Background(), db2.QueryPlan{Table: table, Mode: db2.PlanSearch, SearchField: field, SearchQuery: query, Limit: limit})
	if err != nil {
		return nil, err
	}
	return result.Rows, nil
}

func (s *MemoryDB2Store) ForeignKey(table string, field string, value uint32, limit int) ([]map[string]interface{}, error) {
	_, err := s.table(table)
	if err != nil {
		return nil, err
	}
	snapshot := s.engine.Snapshot()
	defer snapshot.Close()
	result, _, err := snapshot.Execute(context.Background(), db2.QueryPlan{Table: table, Mode: db2.PlanForeignKey, ForeignField: field, ForeignValue: value, Limit: limit})
	if err != nil {
		return nil, err
	}
	rows := result.Rows
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (s *MemoryDB2Store) Stream(table string, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	return s.Rows(table, nil, fields, filter, limit)
}

func (s *MemoryDB2Store) table(name string) (memoryDB2Table, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tables[name]
	if !ok || t.reader == nil {
		return memoryDB2Table{}, fmt.Errorf("table not loaded: %s", name)
	}
	return t, nil
}

func parseFilter(filter string) (func(map[string]interface{}) bool, error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return nil, nil
	}
	parts := strings.SplitN(filter, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		return nil, fmt.Errorf("invalid filter %q, expected field=value", filter)
	}
	field := strings.TrimSpace(parts[0])
	want := strings.TrimSpace(parts[1])
	wantInt, wantIntErr := strconv.ParseInt(want, 10, 64)
	wantUint, wantUintErr := strconv.ParseUint(want, 10, 64)
	wantBool, wantBoolErr := strconv.ParseBool(want)

	return func(row map[string]interface{}) bool {
		got, ok := row[field]
		if !ok {
			return false
		}
		if fmt.Sprint(got) == want {
			return true
		}
		switch v := got.(type) {
		case int:
			return wantIntErr == nil && int64(v) == wantInt
		case int8:
			return wantIntErr == nil && int64(v) == wantInt
		case int16:
			return wantIntErr == nil && int64(v) == wantInt
		case int32:
			return wantIntErr == nil && int64(v) == wantInt
		case int64:
			return wantIntErr == nil && v == wantInt
		case uint:
			return wantUintErr == nil && uint64(v) == wantUint
		case uint8:
			return wantUintErr == nil && uint64(v) == wantUint
		case uint16:
			return wantUintErr == nil && uint64(v) == wantUint
		case uint32:
			return wantUintErr == nil && uint64(v) == wantUint
		case uint64:
			return wantUintErr == nil && v == wantUint
		case bool:
			return wantBoolErr == nil && v == wantBool
		default:
			return false
		}
	}, nil
}
