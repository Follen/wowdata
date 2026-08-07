package runtime

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"wowdata/internal/db2"
	"wowdata/internal/sqlquery"
)

type memoryDB2Table struct {
	schema []SchemaField
	reader db2.RowReader
}

func (s *MemoryDB2Store) Describe(ctx context.Context, table string) (sqlquery.TableInfo, error) {
	t, err := s.table(table)
	if err != nil {
		return sqlquery.TableInfo{}, err
	}
	info := sqlquery.TableInfo{Name: table}
	if sized, ok := t.reader.(db2.SizedReader); ok {
		info.RowCount = sized.Size()
	}
	schema := t.schema
	if len(schema) == 0 {
		// Compatibility-only path for legacy in-memory readers and tests. Real
		// DB2 tables always arrive with a Build-specific DBD schema.
		rows := t.reader.GetAllRows()
		info.RowCount = len(rows)
		for _, row := range rows {
			for name, value := range row {
				schema = append(schema, SchemaField{Name: name, Type: fmt.Sprintf("%T", value)})
			}
			break
		}
		sort.Slice(schema, func(i, j int) bool { return schema[i].Name < schema[j].Name })
	}
	for _, field := range schema {
		typ := strings.ToLower(field.Type)
		info.Columns = append(info.Columns, sqlquery.Column{
			Name: field.Name, Type: field.Type,
			ID:           strings.EqualFold(field.Name, "ID") || strings.Contains(typ, "noninlineid"),
			Relationship: strings.Contains(typ, "relation"),
		})
	}
	return info, ctx.Err()
}

func (s *MemoryDB2Store) Lookup(ctx context.Context, table string, ids []uint32, fields []string) ([]sqlquery.Row, sqlquery.AccessStats, error) {
	if _, err := s.table(table); err != nil {
		return nil, sqlquery.AccessStats{}, err
	}
	snapshot := s.engine.Snapshot()
	defer snapshot.Close()
	result, stats, err := snapshot.Execute(ctx, db2.QueryPlan{Table: table, Mode: db2.PlanRows, IDs: ids, Fields: fields})
	return sqlRows(result.Rows), sqlAccess(stats), err
}

func (s *MemoryDB2Store) Relationship(ctx context.Context, table, field string, values []uint32, fields []string) ([]sqlquery.Row, sqlquery.AccessStats, error) {
	if _, err := s.table(table); err != nil {
		return nil, sqlquery.AccessStats{}, err
	}
	snapshot := s.engine.Snapshot()
	defer snapshot.Close()
	var rows []map[string]interface{}
	combined := sqlquery.AccessStats{Physical: "WDCRelationshipLookup", BatchCount: 1}
	for _, value := range values {
		result, stats, err := snapshot.Execute(ctx, db2.QueryPlan{Table: table, Mode: db2.PlanForeignKey, ForeignField: field, ForeignValue: value, Fields: fields})
		if err != nil {
			return nil, combined, err
		}
		rows = append(rows, result.Rows...)
		a := sqlAccess(stats)
		combined.InputRows = maxInt(combined.InputRows, a.InputRows)
		combined.VisitedRows += a.VisitedRows
		combined.ScannedRows += a.ScannedRows
		combined.DecodedRows += a.DecodedRows
		combined.DecodedFields = maxInt(combined.DecodedFields, a.DecodedFields)
		combined.OutputRows += a.OutputRows
		if a.Fallback != "" {
			combined.Fallback = a.Fallback
		}
	}
	if combined.Fallback != "" {
		combined.Physical = "WDCSectionScan"
	}
	return sqlRows(rows), combined, ctx.Err()
}

func (s *MemoryDB2Store) Scan(ctx context.Context, table string, fields []string, limit int) ([]sqlquery.Row, sqlquery.AccessStats, error) {
	if _, err := s.table(table); err != nil {
		return nil, sqlquery.AccessStats{}, err
	}
	snapshot := s.engine.Snapshot()
	defer snapshot.Close()
	result, stats, err := snapshot.Execute(ctx, db2.QueryPlan{Table: table, Mode: db2.PlanRows, Fields: fields, Limit: limit})
	a := sqlAccess(stats)
	a.Physical = "WDCColumnBatchScan"
	return sqlRows(result.Rows), a, err
}

func (s *MemoryDB2Store) ScanStream(ctx context.Context, table string, fields []string, limit int, yield func(sqlquery.Row) error) (sqlquery.AccessStats, error) {
	if _, err := s.table(table); err != nil {
		return sqlquery.AccessStats{}, err
	}
	snapshot := s.engine.Snapshot()
	defer snapshot.Close()
	_, stats, err := snapshot.Execute(ctx, db2.QueryPlan{Table: table, Mode: db2.PlanStream, Fields: fields, Limit: limit, Yield: func(row map[string]interface{}) error {
		return yield(sqlquery.Row(row))
	}})
	a := sqlAccess(stats)
	a.Physical = "WDCStreamingColumnBatchScan"
	return a, err
}

func sqlRows(rows []map[string]interface{}) []sqlquery.Row {
	out := make([]sqlquery.Row, len(rows))
	for i, row := range rows {
		out[i] = sqlquery.Row(row)
	}
	return out
}

func sqlAccess(stats db2.QueryStats) sqlquery.AccessStats {
	physical := stats.Physical
	switch stats.Physical {
	case "point-or-scan":
		physical = "WDCRecordIDLookup"
	case "relationship":
		physical = "WDCRelationshipLookup"
	case "projected-scan", "stream-scan":
		physical = "WDCColumnBatchScan"
	}
	return sqlquery.AccessStats{Physical: physical, InputRows: stats.InputRows, VisitedRows: stats.ScanRows + stats.DecodedRows, ScannedRows: stats.ScanRows, DecodedRows: stats.DecodedRows, DecodedFields: stats.DecodedFields, OutputRows: stats.OutputRows, BatchCount: stats.BatchCount, Fallback: stats.Fallback}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
	query, err := typedSelect(table, ids, fields, filter, limit)
	if err != nil {
		return nil, err
	}
	return s.executeTyped(query)
}

func (s *MemoryDB2Store) Search(table string, field string, query string, limit int) ([]map[string]interface{}, error) {
	if strings.TrimSpace(field) == "" {
		return nil, fmt.Errorf("search field is required")
	}
	q, err := typedSelect(table, nil, nil, "", limit)
	if err != nil {
		return nil, err
	}
	q.Where = &sqlquery.BinaryExpr{Op: "LIKE", Left: &sqlquery.CallExpr{Name: "LOWER", Args: []sqlquery.Expr{&sqlquery.Identifier{Name: field}}}, Right: &sqlquery.Literal{Value: "%" + strings.ToLower(query) + "%"}}
	return s.executeTyped(q)
}

func (s *MemoryDB2Store) ForeignKey(table string, field string, value uint32, limit int) ([]map[string]interface{}, error) {
	q, err := typedSelect(table, nil, nil, "", limit)
	if err != nil {
		return nil, err
	}
	q.Where = &sqlquery.BinaryExpr{Op: "=", Left: &sqlquery.Identifier{Name: field}, Right: &sqlquery.Literal{Value: value}}
	return s.executeTyped(q)
}

func (s *MemoryDB2Store) Stream(table string, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	return s.Rows(table, nil, fields, filter, limit)
}

func (s *MemoryDB2Store) executeTyped(query *sqlquery.Select) ([]map[string]interface{}, error) {
	result, err := (&sqlquery.Engine{Source: s}).Execute(context.Background(), &sqlquery.Statement{Query: query}, nil)
	if err != nil {
		return nil, err
	}
	return result.Rows, nil
}

func typedSelect(table string, ids []uint32, fields []string, filter string, limit int) (*sqlquery.Select, error) {
	q := &sqlquery.Select{From: sqlquery.TableRef{Name: table}}
	if len(fields) == 0 {
		q.Items = []sqlquery.SelectItem{{Expr: &sqlquery.Star{}}}
	} else {
		for _, field := range fields {
			q.Items = append(q.Items, sqlquery.SelectItem{Expr: &sqlquery.Identifier{Name: field}})
		}
	}
	var predicates []sqlquery.Expr
	if len(ids) == 1 {
		predicates = append(predicates, &sqlquery.BinaryExpr{Op: "=", Left: &sqlquery.Identifier{Name: "ID"}, Right: &sqlquery.Literal{Value: ids[0]}})
	} else if len(ids) > 1 {
		in := &sqlquery.InExpr{X: &sqlquery.Identifier{Name: "ID"}}
		for _, id := range ids {
			in.List = append(in.List, &sqlquery.Literal{Value: id})
		}
		predicates = append(predicates, in)
	}
	filter = strings.TrimSpace(filter)
	if filter != "" {
		parts := strings.SplitN(filter, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("invalid filter %q, expected field=value", filter)
		}
		predicates = append(predicates, &sqlquery.BinaryExpr{Op: "=", Left: &sqlquery.Identifier{Name: strings.TrimSpace(parts[0])}, Right: &sqlquery.Literal{Value: inferFilterValue(strings.TrimSpace(parts[1]))}})
	}
	for _, p := range predicates {
		if q.Where == nil {
			q.Where = p
		} else {
			q.Where = &sqlquery.BinaryExpr{Op: "AND", Left: q.Where, Right: p}
		}
	}
	if limit > 0 {
		q.Limit = &limit
	}
	return q, nil
}

func inferFilterValue(value string) any {
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n
	}
	if n, err := strconv.ParseUint(value, 10, 64); err == nil {
		return n
	}
	if b, err := strconv.ParseBool(value); err == nil {
		return b
	}
	return value
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
