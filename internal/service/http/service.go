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
	appruntime "wowdata/internal/local/runtime"
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
	resolver     ContextResolver
	contextPool  *ContextPool
	queryEngine  parquetQueryEngine
	metadataDB   *sql.DB
	flights      *Singleflight

	mu              sync.Mutex
	ensuredTables   map[string]struct{}
	materializeFunc func(context.Context, RequestContext, string) error
	metadataErr     error
}

func NewService(cfg config.HTTPConfig, materializer Materializer) *Service {
	svc := &Service{
		cfg:             cfg,
		materializer:    materializer,
		contextPool:     NewContextPool(cfg.Contexts.MaxContexts),
		flights:         NewSingleflight(),
		ensuredTables:   make(map[string]struct{}),
		materializeFunc: nil,
	}
	if db2Materializer, ok := materializer.(*DB2Materializer); ok {
		svc.resolver = db2Materializer.Resolver
	}
	for _, pinned := range cfg.Contexts.Pinned {
		svc.contextPool.Pin(contextKey(svc.ResolveRequestContext(RequestContext{
			Region:  pinned.Region,
			Product: pinned.Product,
			Locale:  pinned.Locale,
		})))
	}
	return svc
}

type DB2Query struct {
	RequestContext RequestContext
	Table          string
	IDs            []uint32
	IDField        string
	SearchField    string
	SearchQuery    string
	Fields         []string
	Filter         string
	Limit          int
}

type DB2Schema struct {
	Table    string            `json:"table"`
	RowCount int               `json:"rowCount"`
	Fields   map[string]string `json:"fields"`
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

func (s *Service) EnsureContext(ctx context.Context, rc RequestContext) error {
	_, err := s.ResolveContext(ctx, rc)
	return err
}

func (s *Service) ResolveContext(ctx context.Context, rc RequestContext) (*appruntime.Context, error) {
	rc = s.ResolveRequestContext(rc)
	key := contextKey(rc)
	if runtimeCtx, ok := s.contextPool.Get(key); ok {
		return runtimeCtx, nil
	}
	value, err := s.flights.Do("context:"+key, func() (interface{}, error) {
		if runtimeCtx, ok := s.contextPool.Get(key); ok {
			return runtimeCtx, nil
		}
		resolver := s.resolver
		if resolver == nil {
			if db2Materializer, ok := s.materializer.(*DB2Materializer); ok {
				resolver = db2Materializer.Resolver
			}
		}
		if resolver == nil {
			return nil, NewCapabilityError("context_unavailable", "runtime context")
		}
		runtimeCtx, err := resolver.ResolveContext(ctx, rc)
		if err != nil {
			return nil, err
		}
		if runtimeCtx == nil {
			return nil, fmt.Errorf("resolved context is nil")
		}
		s.contextPool.Put(key, runtimeCtx)
		return runtimeCtx, nil
	})
	if err != nil {
		return nil, err
	}
	runtimeCtx, ok := value.(*appruntime.Context)
	if !ok || runtimeCtx == nil {
		return nil, fmt.Errorf("resolved context is nil")
	}
	return runtimeCtx, nil
}

func (s *Service) RequireCapability(ctx context.Context, rc RequestContext, capability string) error {
	return NewCapabilityError(capabilityErrorCode(capability), capability)
}

func (s *Service) PrewarmConfiguredContexts(ctx context.Context) error {
	if !s.cfg.Prepare.PrewarmOnStart {
		return nil
	}
	tables := s.cfg.Prepare.DefaultTables
	if len(tables) == 0 {
		return nil
	}
	contexts := s.cfg.Contexts.Pinned
	if len(contexts) == 0 {
		contexts = []config.HTTPPinnedContext{{
			Region:  s.cfg.Defaults.Region,
			Product: s.cfg.Defaults.Product,
			Locale:  s.cfg.Defaults.Locale,
			Label:   "default",
		}}
	}
	for _, pinned := range contexts {
		rc := s.ResolveRequestContext(RequestContext{Region: pinned.Region, Product: pinned.Product, Locale: pinned.Locale})
		for _, table := range tables {
			if err := s.EnsureTable(ctx, rc, table); err != nil {
				return fmt.Errorf("prewarm %s/%s/%s %s: %w", rc.Region, rc.Product, rc.Locale, table, err)
			}
		}
	}
	return nil
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

func (s *Service) SchemaDB2(ctx context.Context, rc RequestContext, table string) (DB2Schema, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		return DB2Schema{}, fmt.Errorf("table is required")
	}
	rc = s.ResolveRequestContext(rc)
	if err := s.EnsureTable(ctx, rc, table); err != nil {
		return DB2Schema{}, err
	}
	record, err := s.materializedTableRecord(rc, table)
	if err != nil {
		return DB2Schema{}, err
	}
	path := strings.ReplaceAll(record.ParquetPath, `\`, `/`)
	if strings.Contains(path, "'") {
		return DB2Schema{}, fmt.Errorf("unsafe parquet path")
	}
	engine := s.queryEngine
	if engine == nil {
		engine = duckdb.NewEngine(s.cfg.Cache.DuckDBPath)
	}
	rows, err := engine.QueryParquet(ctx, "DESCRIBE SELECT * FROM read_parquet('"+path+"')", nil)
	if errors.Is(err, duckdb.ErrUnavailable) {
		return DB2Schema{}, NewCapabilityError("query_engine_unavailable", "query engine")
	}
	if err != nil {
		return DB2Schema{}, err
	}
	fields := make(map[string]string, len(rows))
	for _, row := range rows {
		name := describeValue(row, "column_name", "Column Name")
		fieldType := describeValue(row, "column_type", "Column Type")
		if name != "" {
			fields[name] = fieldType
		}
	}
	return DB2Schema{Table: table, RowCount: record.RowCount, Fields: fields}, nil
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

func (s *Service) SetContextResolverForTest(resolver ContextResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolver = resolver
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
	if strings.TrimSpace(query.SearchQuery) != "" {
		if !duckdb.IsSafeIdentifier(query.SearchField) {
			return "", nil, fmt.Errorf("unsafe search field %q", query.SearchField)
		}
		clauses = append(clauses, fmt.Sprintf(`lower(CAST("%s" AS VARCHAR)) LIKE ?`, query.SearchField))
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(query.SearchQuery))+"%")
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
	record, err := s.materializedTableRecord(rc, table)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(record.ParquetPath, `\`, `/`), nil
}

func (s *Service) materializedTableRecord(rc RequestContext, table string) (metadata.MaterializedTable, error) {
	db, err := s.openMetadataDB()
	if err != nil {
		return metadata.MaterializedTable{}, err
	}
	record, err := metadata.LatestMaterializedTable(db, rc.Region, rc.Product, rc.Locale, table)
	if errors.Is(err, sql.ErrNoRows) {
		return metadata.MaterializedTable{}, NewCapabilityError("query_engine_unavailable", "query engine")
	}
	if err != nil {
		return metadata.MaterializedTable{}, err
	}
	return record, nil
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

func describeValue(row map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok && value != nil {
			text := fmt.Sprint(value)
			if text != "<nil>" {
				return text
			}
		}
	}
	return ""
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

func contextKey(rc RequestContext) string {
	return appruntime.RemoteContextKey{
		Region:    rc.Region,
		Product:   rc.Product,
		Locale:    rc.Locale,
		CacheRoot: "",
	}.String()
}

func capabilityErrorCode(capability string) string {
	if strings.Contains(capability, "export") {
		return "export_engine_unavailable"
	}
	return "query_engine_unavailable"
}
