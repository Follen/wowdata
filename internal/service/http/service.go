package http

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"wowdata/internal/cache/duckdb"
	"wowdata/internal/cache/metadata"
	"wowdata/internal/config"
)

type RequestContext struct {
	Region  string
	Product string
	Locale  string
}

type ToolPolicy struct {
	ExposeWarmup     bool
	ExposeAdminTools bool
	ExposeDebugTools bool
}

type Service struct {
	cfg          config.HTTPConfig
	materializer Materializer
	queryEngine  parquetQueryEngine
	metadataDB   *sql.DB
	flights      *Singleflight

	mu              sync.Mutex
	ensuredTables   map[string]struct{}
	materializeFunc func(context.Context, RequestContext, string) error
	metadataErr     error
}

func NewService(cfg config.HTTPConfig, materializer Materializer) *Service {
	return &Service{
		cfg:             cfg,
		materializer:    materializer,
		flights:         NewSingleflight(),
		ensuredTables:   make(map[string]struct{}),
		materializeFunc: nil,
	}
}

type DB2Query struct {
	RequestContext RequestContext
	Table          string
	IDs            []uint32
	IDField        string
	Fields         []string
	Filter         string
	Limit          int
}

type parquetQueryEngine interface {
	QueryParquet(ctx context.Context, query string, args []interface{}) ([]map[string]interface{}, error)
}

func (s *Service) ResolveRequestContext(rc RequestContext) RequestContext {
	if rc.Region == "" {
		rc.Region = s.cfg.Defaults.Region
	}
	if rc.Product == "" {
		rc.Product = s.cfg.Defaults.Product
	}
	if rc.Locale == "" {
		rc.Locale = s.cfg.Defaults.Locale
	}
	return rc
}

func (s *Service) ToolPolicy() ToolPolicy {
	return ToolPolicy{
		ExposeWarmup:     false,
		ExposeAdminTools: s.cfg.Tools.ExposeAdminTools,
		ExposeDebugTools: s.cfg.Tools.ExposeDebugTools,
	}
}

func (s *Service) Builds() BuildCatalog {
	pinned := make([]PinnedContext, 0, len(s.cfg.Contexts.Pinned))
	for _, ctx := range s.cfg.Contexts.Pinned {
		pinned = append(pinned, PinnedContext{
			Region:  ctx.Region,
			Product: ctx.Product,
			Locale:  ctx.Locale,
			Label:   ctx.Label,
		})
	}
	return BuildCatalog{
		Default: ContextDefaults{
			Region:  s.cfg.Defaults.Region,
			Product: s.cfg.Defaults.Product,
			Locale:  s.cfg.Defaults.Locale,
		},
		Pinned: pinned,
	}
}

func (s *Service) EnsureContext(context.Context, RequestContext) error {
	return nil
}

func (s *Service) RequireCapability(ctx context.Context, rc RequestContext, capability string) error {
	return NewCapabilityError(capabilityErrorCode(capability), capability)
}

func (s *Service) Status() Status {
	return Status{
		OK: true,
		Cache: CacheStatus{
			Root: s.cfg.Cache.Root,
		},
		Memory: MemoryStatus{
			MaxContexts: s.cfg.Contexts.MaxContexts,
		},
	}
}

func (s *Service) EnsureTable(ctx context.Context, rc RequestContext, table string) error {
	table = strings.TrimSpace(table)
	if table == "" {
		return fmt.Errorf("table is required")
	}
	rc = s.ResolveRequestContext(rc)
	key := tableKey(rc, table)

	s.mu.Lock()
	if _, ok := s.ensuredTables[key]; ok {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	_, err := s.flights.Do(key, func() (interface{}, error) {
		s.mu.Lock()
		if _, ok := s.ensuredTables[key]; ok {
			s.mu.Unlock()
			return nil, nil
		}
		s.mu.Unlock()

		if err := s.materialize(ctx, rc, table); err != nil {
			if errors.Is(err, duckdb.ErrUnavailable) {
				return nil, NewCapabilityError("query_engine_unavailable", "query engine")
			}
			return nil, err
		}

		s.mu.Lock()
		s.ensuredTables[key] = struct{}{}
		s.mu.Unlock()
		return nil, nil
	})
	return err
}

func (s *Service) QueryDB2(ctx context.Context, query DB2Query) ([]map[string]interface{}, error) {
	table := strings.TrimSpace(query.Table)
	if table == "" {
		return nil, fmt.Errorf("table is required")
	}
	if err := s.EnsureTable(ctx, query.RequestContext, table); err != nil {
		return nil, err
	}
	engine := s.queryEngine
	if engine == nil {
		engine = duckdb.NewEngine(s.cfg.Cache.DuckDBPath)
	}
	sqlQuery, args, err := s.buildDB2SQL(query)
	if err != nil {
		return nil, err
	}
	rows, err := engine.QueryParquet(ctx, sqlQuery, args)
	if errors.Is(err, duckdb.ErrUnavailable) {
		return nil, NewCapabilityError("query_engine_unavailable", "query engine")
	}
	return rows, err
}

func (s *Service) SetMaterializeFuncForTest(fn func(context.Context, RequestContext, string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.materializeFunc = fn
}

func (s *Service) SetQueryEngineForTest(engine parquetQueryEngine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queryEngine = engine
}

func (s *Service) SetMetadataDBForTest(db *sql.DB) {
	s.SetMetadataDB(db)
}

func (s *Service) SetMetadataDB(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadataDB = db
}

func (s *Service) HasMaterializerForTest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.materializer != nil
}

func (s *Service) HasMetadataDBForTest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metadataDB != nil
}

func (s *Service) buildDB2SQL(query DB2Query) (string, []interface{}, error) {
	rc := s.ResolveRequestContext(query.RequestContext)
	path, err := s.materializedParquetPath(rc, strings.TrimSpace(query.Table))
	if err != nil {
		return "", nil, err
	}
	if strings.Contains(path, "'") {
		return "", nil, fmt.Errorf("unsafe parquet path")
	}
	fields, err := selectList(query.Fields)
	if err != nil {
		return "", nil, err
	}
	var builder strings.Builder
	builder.WriteString("SELECT ")
	builder.WriteString(fields)
	builder.WriteString(" FROM read_parquet('")
	builder.WriteString(path)
	builder.WriteString("')")
	args := make([]interface{}, 0, len(query.IDs))
	clauses := make([]string, 0, 2)
	if len(query.IDs) > 0 {
		idField := query.IDField
		if idField == "" {
			idField = "ID"
		}
		if !duckdb.IsSafeIdentifier(idField) {
			return "", nil, fmt.Errorf("unsafe field identifier %q", idField)
		}
		placeholders := make([]string, len(query.IDs))
		for i, id := range query.IDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		clauses = append(clauses, fmt.Sprintf(`"%s" IN (%s)`, idField, strings.Join(placeholders, ", ")))
	}
	if strings.TrimSpace(query.Filter) != "" {
		clause, value, err := safeFilterClause(query.Filter)
		if err != nil {
			return "", nil, CapabilityError{Code: "invalid_filter", Message: err.Error()}
		}
		clauses = append(clauses, clause)
		args = append(args, value)
	}
	if len(clauses) > 0 {
		builder.WriteString(" WHERE ")
		builder.WriteString(strings.Join(clauses, " AND "))
	}
	if query.Limit > 0 {
		builder.WriteString(" LIMIT ")
		builder.WriteString(strconv.Itoa(query.Limit))
	}
	return builder.String(), args, nil
}

func (s *Service) materializedParquetPath(rc RequestContext, table string) (string, error) {
	db, err := s.openMetadataDB()
	if err != nil {
		return "", err
	}
	record, err := metadata.LatestMaterializedTable(db, rc.Region, rc.Product, rc.Locale, table)
	if errors.Is(err, sql.ErrNoRows) {
		return "", NewCapabilityError("query_engine_unavailable", "query engine")
	}
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(record.ParquetPath, `\`, `/`), nil
}

func (s *Service) openMetadataDB() (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.metadataDB != nil {
		return s.metadataDB, nil
	}
	if s.metadataErr != nil {
		return nil, s.metadataErr
	}
	db, err := metadata.Open(s.cfg.Cache.MetadataDB, "migrations/sqlite")
	if err != nil {
		s.metadataErr = err
		return nil, err
	}
	s.metadataDB = db
	return db, nil
}

func safeFilterClause(filter string) (string, interface{}, error) {
	parts := strings.SplitN(strings.TrimSpace(filter), "=", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid filter %q, expected field=value", filter)
	}
	field := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if !duckdb.IsSafeIdentifier(field) {
		return "", nil, fmt.Errorf("unsafe filter field %q", field)
	}
	if strings.ContainsAny(value, "'\";()") || strings.Contains(strings.ToLower(value), " or ") || strings.Contains(strings.ToLower(value), " and ") {
		return "", nil, fmt.Errorf("unsafe filter value")
	}
	if value == "" {
		return "", nil, fmt.Errorf("filter value is required")
	}
	return fmt.Sprintf(`"%s" = ?`, field), value, nil
}

func selectList(fields []string) (string, error) {
	if len(fields) == 0 {
		return "*", nil
	}
	quoted := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if !duckdb.IsSafeIdentifier(field) {
			return "", fmt.Errorf("unsafe field identifier %q", field)
		}
		quoted = append(quoted, `"`+field+`"`)
	}
	if len(quoted) == 0 {
		return "*", nil
	}
	return strings.Join(quoted, ", "), nil
}

func (s *Service) materialize(ctx context.Context, rc RequestContext, table string) error {
	s.mu.Lock()
	fn := s.materializeFunc
	s.mu.Unlock()
	if fn != nil {
		return fn(ctx, rc, table)
	}
	if s.materializer != nil {
		return s.materializer.EnsureTable(ctx, rc, table)
	}
	return NewCapabilityError("query_engine_unavailable", "query engine")
}

func tableKey(rc RequestContext, table string) string {
	return rc.Region + "/" + rc.Product + "/" + rc.Locale + "/" + table
}

func capabilityErrorCode(capability string) string {
	if strings.Contains(capability, "export") {
		return "export_engine_unavailable"
	}
	return "query_engine_unavailable"
}
