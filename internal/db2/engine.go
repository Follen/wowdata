package db2

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PlanMode is intentionally small: it is the internal physical boundary used
// by CLI services, not a public query language.
type PlanMode uint8

const (
	PlanRows PlanMode = iota
	PlanSearch
	PlanForeignKey
	PlanSchema
	PlanStream
)

type QueryPlan struct {
	Table         string
	Mode          PlanMode
	IDs           []uint32
	Fields        []string
	Filter        func(map[string]interface{}) bool
	SearchField   string
	SearchQuery   string
	CaseSensitive bool
	ForeignField  string
	ForeignValue  uint32
	Limit         int
	Yield         func(map[string]interface{}) error
}

type QueryStats struct {
	Table         string        `json:"table"`
	Mode          string        `json:"mode"`
	Physical      string        `json:"physical"`
	InputRows     int           `json:"inputRows"`
	OutputRows    int           `json:"outputRows"`
	DecodedRows   int           `json:"decodedRows"`
	DecodedFields int           `json:"decodedFields"`
	ScanRows      int           `json:"scanRows"`
	BatchCount    int           `json:"batchCount"`
	Duration      time.Duration `json:"duration"`
	Fallback      string        `json:"fallback,omitempty"`
}

type Engine struct {
	catalog *Catalog
}

type Catalog struct {
	mu     sync.RWMutex
	tables map[string]tableEntry
}

type tableEntry struct {
	schema []SchemaField
	reader RowReader
}

type Snapshot struct {
	catalog *Catalog
	tables  map[string]tableEntry
	closed  bool
	mu      sync.RWMutex
}

func NewEngine() *Engine {
	return &Engine{catalog: &Catalog{tables: make(map[string]tableEntry)}}
}

func (e *Engine) Register(table string, schema []SchemaField, reader RowReader) error {
	if e == nil || e.catalog == nil {
		return fmt.Errorf("db2 engine is nil")
	}
	if table == "" || reader == nil {
		return fmt.Errorf("table and reader are required")
	}
	copySchema := append([]SchemaField(nil), schema...)
	e.catalog.mu.Lock()
	e.catalog.tables[table] = tableEntry{schema: copySchema, reader: reader}
	e.catalog.mu.Unlock()
	return nil
}

func (e *Engine) Snapshot() *Snapshot {
	if e == nil || e.catalog == nil {
		return &Snapshot{tables: map[string]tableEntry{}}
	}
	e.catalog.mu.RLock()
	tables := make(map[string]tableEntry, len(e.catalog.tables))
	for name, table := range e.catalog.tables {
		table.schema = append([]SchemaField(nil), table.schema...)
		tables[name] = table
	}
	e.catalog.mu.RUnlock()
	return &Snapshot{catalog: e.catalog, tables: tables}
}

func (s *Snapshot) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.tables = nil
	s.mu.Unlock()
}

func (s *Snapshot) Execute(ctx context.Context, plan QueryPlan) (QueryResult, QueryStats, error) {
	started := time.Now()
	stats := QueryStats{Table: plan.Table, Mode: modeName(plan.Mode), Physical: "fallback"}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return QueryResult{}, stats, err
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return QueryResult{}, stats, fmt.Errorf("db2 snapshot is closed")
	}
	table, ok := s.tables[plan.Table]
	s.mu.RUnlock()
	if !ok || table.reader == nil {
		return QueryResult{}, stats, fmt.Errorf("table not loaded: %s", plan.Table)
	}
	result := QueryResult{Schema: append([]SchemaField(nil), table.schema...)}
	if sized, ok := table.reader.(SizedReader); ok {
		stats.InputRows = sized.Size()
	}
	switch plan.Mode {
	case PlanSchema:
		stats.Physical = "catalog"
	case PlanRows:
		stats.Physical = "point-or-scan"
		var err error
		result.Rows, err = getRowsContext(ctx, table.reader, plan.IDs, plan.Fields, plan.Filter, plan.Limit)
		if err != nil {
			return QueryResult{}, stats, err
		}
		if len(plan.IDs) > 0 {
			stats.DecodedRows = len(result.Rows)
			stats.BatchCount = 1
		} else {
			stats.ScanRows = stats.InputRows
			stats.DecodedRows = len(result.Rows)
		}
	case PlanSearch:
		stats.Physical = "projected-scan"
		var err error
		result.Rows, err = searchRowsContext(ctx, table.reader, plan.SearchField, plan.SearchQuery, plan.CaseSensitive, plan.Limit)
		if err != nil {
			return QueryResult{}, stats, err
		}
		stats.ScanRows = stats.InputRows
		stats.DecodedRows = len(result.Rows)
	case PlanForeignKey:
		stats.Physical = "relationship-or-scan"
		var relationship bool
		var err error
		result.Rows, relationship, err = getForeignRowsContext(ctx, table.reader, plan.Table, plan.ForeignField, plan.ForeignValue)
		if err != nil {
			return QueryResult{}, stats, err
		}
		if relationship {
			stats.Physical = "relationship"
		} else {
			stats.ScanRows = stats.InputRows
			stats.Fallback = "relationship index unavailable"
		}
		if plan.Limit > 0 && len(result.Rows) > plan.Limit {
			result.Rows = result.Rows[:plan.Limit]
		}
		stats.DecodedRows = len(result.Rows)
	case PlanStream:
		stats.Physical = "stream-scan"
		if plan.Yield == nil {
			return QueryResult{}, stats, fmt.Errorf("DB2 stream plan requires a yield callback")
		}
		streamedRows, err := executeStream(ctx, table.reader, plan)
		if err != nil {
			return QueryResult{}, stats, err
		}
		stats.DecodedRows = streamedRows
		stats.OutputRows = streamedRows
		stats.ScanRows = stats.InputRows
	default:
		return QueryResult{}, stats, fmt.Errorf("unsupported DB2 plan mode %d", plan.Mode)
	}
	if plan.Mode != PlanStream {
		stats.OutputRows = len(result.Rows)
	}
	if len(plan.Fields) > 0 {
		stats.DecodedFields = len(plan.Fields)
	} else {
		stats.DecodedFields = len(table.schema)
	}
	stats.Duration = time.Since(started)
	return result, stats, nil
}

// Keep the stream callback's captured counter out of Execute so point and
// relationship plans do not inherit a heap escape from an unselected branch.
func executeStream(ctx context.Context, reader RowReader, plan QueryPlan) (int, error) {
	streamedRows := 0
	err := streamRowsContext(ctx, reader, plan.Fields, plan.Filter, plan.Limit, func(row map[string]interface{}) error {
		streamedRows++
		return plan.Yield(row)
	})
	return streamedRows, err
}

func hasBatchRelationship(reader RowReader) bool {
	_, ok := reader.(BatchRelationshipRowReader)
	return ok
}

func modeName(mode PlanMode) string {
	switch mode {
	case PlanRows:
		return "rows"
	case PlanSearch:
		return "search"
	case PlanForeignKey:
		return "foreign-key"
	case PlanSchema:
		return "schema"
	case PlanStream:
		return "stream"
	default:
		return "unknown"
	}
}
