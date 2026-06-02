package http

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"wowdata/internal/cache/duckdb"
	"wowdata/internal/cache/metadata"
	"wowdata/internal/config"
	appruntime "wowdata/internal/local/runtime"
)

func TestServiceDefaultsToCNRetail(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)

	rc := svc.ResolveRequestContext(RequestContext{})

	if rc.Region != "cn" || rc.Product != "wow" || rc.Locale != "zhCN" {
		t.Fatalf("context = %#v", rc)
	}
}

func TestServiceRequestContextOverridesDefaults(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)

	rc := svc.ResolveRequestContext(RequestContext{
		Region:  "us",
		Product: "wow_classic",
		Locale:  "enUS",
	})

	if rc.Region != "us" || rc.Product != "wow_classic" || rc.Locale != "enUS" {
		t.Fatalf("context = %#v", rc)
	}
}

func TestServiceToolPolicyGatesWarmupAndAdminTools(t *testing.T) {
	cfg := config.DefaultHTTPConfig()
	cfg.Tools.ExposeAdminTools = false
	svc := NewService(cfg, nil)
	policy := svc.ToolPolicy()
	if policy.ExposeWarmup {
		t.Fatal("HTTP ordinary tools must not expose wow_warmup")
	}
	if policy.ExposeAdminTools {
		t.Fatal("admin tools should be hidden when disabled")
	}

	cfg.Tools.ExposeAdminTools = true
	svc = NewService(cfg, nil)
	policy = svc.ToolPolicy()
	if !policy.ExposeAdminTools {
		t.Fatal("admin tools should be exposed when enabled")
	}
}

func TestServiceBuildsReportsDefaultsAndPinnedContexts(t *testing.T) {
	cfg := config.DefaultHTTPConfig()
	cfg.Defaults.Region = "eu"
	cfg.Defaults.Product = "wowt"
	cfg.Defaults.Locale = "enUS"
	cfg.Contexts.Pinned = []config.HTTPPinnedContext{
		{Region: "cn", Product: "wow", Locale: "zhCN", Label: "CN Retail"},
	}
	svc := NewService(cfg, nil)

	builds := svc.Builds()

	if builds.Default.Region != "eu" || builds.Default.Product != "wowt" || builds.Default.Locale != "enUS" {
		t.Fatalf("default context = %#v", builds.Default)
	}
	if len(builds.Pinned) != 1 || builds.Pinned[0].Label != "CN Retail" {
		t.Fatalf("pinned contexts = %#v", builds.Pinned)
	}
}

func TestServiceEnsureTableCachesSuccessfulMaterialization(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)
	var calls int
	svc.SetMaterializeFuncForTest(func(context.Context, RequestContext, string) error {
		calls++
		return nil
	})

	rc := RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}
	if err := svc.EnsureTable(context.Background(), rc, "SpellName"); err != nil {
		t.Fatalf("EnsureTable first: %v", err)
	}
	if err := svc.EnsureTable(context.Background(), rc, "SpellName"); err != nil {
		t.Fatalf("EnsureTable second: %v", err)
	}

	if calls != 1 {
		t.Fatalf("materialize calls = %d, want 1", calls)
	}
}

func TestServiceEnsureTableRejectsEmptyTable(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)

	if err := svc.EnsureTable(context.Background(), RequestContext{}, ""); err == nil {
		t.Fatal("EnsureTable empty table error = nil")
	}
}

func TestServiceEnsureTableRejectsMissingMaterializerWithoutCaching(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)
	rc := RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}

	err := svc.EnsureTable(context.Background(), rc, "SpellName")
	if err == nil {
		t.Fatal("EnsureTable missing materializer error = nil")
	}
	var capabilityErr CapabilityError
	if !errors.As(err, &capabilityErr) {
		t.Fatalf("EnsureTable error = %T %[1]v, want CapabilityError", err)
	}
	if capabilityErr.Code != "query_engine_unavailable" {
		t.Fatalf("EnsureTable error code = %q, want query_engine_unavailable", capabilityErr.Code)
	}

	err = svc.EnsureTable(context.Background(), rc, "SpellName")
	if err == nil {
		t.Fatal("EnsureTable second missing materializer error = nil")
	}
	if !errors.As(err, &capabilityErr) {
		t.Fatalf("EnsureTable second error = %T %[1]v, want CapabilityError", err)
	}
	if capabilityErr.Code != "query_engine_unavailable" {
		t.Fatalf("EnsureTable second error code = %q, want query_engine_unavailable", capabilityErr.Code)
	}
}

func TestServiceEnsureTableRetriesAfterMaterializationError(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)
	var calls int
	fail := errors.New("materialize failed")
	svc.SetMaterializeFuncForTest(func(context.Context, RequestContext, string) error {
		calls++
		if calls == 1 {
			return fail
		}
		return nil
	})

	rc := RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}
	if err := svc.EnsureTable(context.Background(), rc, "SpellName"); !errors.Is(err, fail) {
		t.Fatalf("EnsureTable first error = %v, want %v", err, fail)
	}
	if err := svc.EnsureTable(context.Background(), rc, "SpellName"); err != nil {
		t.Fatalf("EnsureTable second: %v", err)
	}

	if calls != 2 {
		t.Fatalf("materialize calls = %d, want 2", calls)
	}
}

func TestServiceEnsureTableMapsQueryEngineUnavailable(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)
	var calls int
	svc.SetMaterializeFuncForTest(func(context.Context, RequestContext, string) error {
		calls++
		return duckdb.ErrUnavailable
	})

	rc := RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}
	for i := 0; i < 2; i++ {
		err := svc.EnsureTable(context.Background(), rc, "SpellName")
		var capabilityErr CapabilityError
		if !errors.As(err, &capabilityErr) {
			t.Fatalf("EnsureTable error = %T %[1]v, want CapabilityError", err)
		}
		if capabilityErr.Code != "query_engine_unavailable" {
			t.Fatalf("EnsureTable error code = %q, want query_engine_unavailable", capabilityErr.Code)
		}
	}
	if calls != 2 {
		t.Fatalf("materialize calls = %d, want 2 so unavailable failures are not cached", calls)
	}
}

func TestServiceEnsureContextUsesPoolAndSingleflight(t *testing.T) {
	cfg := config.DefaultHTTPConfig()
	cfg.Contexts.MaxContexts = 2
	resolver := &fakeRuntimeContextResolver{
		ctx: &appruntime.Context{
			Source:      "remote",
			Region:      "cn",
			Product:     "wow",
			Locale:      "zhCN",
			BuildKey:    "build-key",
			CASCReady:   true,
			DBDReady:    true,
			TablesReady: map[string]bool{},
		},
	}
	svc := NewService(cfg, nil)
	svc.SetContextResolverForTest(resolver)

	rc := RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}
	if err := svc.EnsureContext(context.Background(), rc); err != nil {
		t.Fatalf("EnsureContext first: %v", err)
	}
	if err := svc.EnsureContext(context.Background(), rc); err != nil {
		t.Fatalf("EnsureContext second: %v", err)
	}

	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls)
	}
}

func TestServicePrewarmConfiguredContextsEnsuresDefaultTablesForPinnedContexts(t *testing.T) {
	cfg := config.DefaultHTTPConfig()
	cfg.Contexts.Pinned = []config.HTTPPinnedContext{
		{Region: "cn", Product: "wow", Locale: "zhCN", Label: "CN Retail"},
		{Region: "cn", Product: "wowt", Locale: "zhCN", Label: "CN PTR"},
	}
	cfg.Prepare.DefaultTables = []string{"SpellName", "ItemSparse"}
	svc := NewService(cfg, nil)
	var ensured []string
	svc.SetMaterializeFuncForTest(func(ctx context.Context, rc RequestContext, table string) error {
		ensured = append(ensured, rc.Region+"/"+rc.Product+"/"+rc.Locale+"/"+table)
		return nil
	})

	if err := svc.PrewarmConfiguredContexts(context.Background()); err != nil {
		t.Fatalf("PrewarmConfiguredContexts: %v", err)
	}

	want := []string{
		"cn/wow/zhCN/SpellName",
		"cn/wow/zhCN/ItemSparse",
		"cn/wowt/zhCN/SpellName",
		"cn/wowt/zhCN/ItemSparse",
	}
	if strings.Join(ensured, "|") != strings.Join(want, "|") {
		t.Fatalf("ensured = %#v, want %#v", ensured, want)
	}
}

func TestServiceQueryDB2MapsUnavailableEngine(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)

	_, err := svc.QueryDB2(context.Background(), DB2Query{
		RequestContext: RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"},
		Table:          "SpellName",
		IDField:        "ID",
		IDs:            []uint32{123},
		Fields:         []string{"ID", "Name_lang"},
		Limit:          1,
	})

	var capabilityErr CapabilityError
	if !errors.As(err, &capabilityErr) {
		t.Fatalf("QueryDB2 error = %T %[1]v, want CapabilityError", err)
	}
	if capabilityErr.Code != "query_engine_unavailable" {
		t.Fatalf("QueryDB2 error code = %q, want query_engine_unavailable", capabilityErr.Code)
	}
}

func TestServiceQueryDB2UsesMaterializedParquetRecord(t *testing.T) {
	db, err := metadata.Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open metadata db: %v", err)
	}
	defer db.Close()
	parquetPath := filepath.Join(t.TempDir(), "SpellName.parquet")
	if err := metadata.UpsertMaterializedTable(db, metadata.MaterializedTable{
		Region:              "cn",
		Product:             "wow",
		BuildKey:            "build-key",
		BuildName:           "12.0.0.61234",
		Locale:              "zhCN",
		TableName:           "SpellName",
		DB2FileDataID:       123,
		DBDDefinitionHash:   "dbd-hash",
		DecoderVersion:      "decoder-v1",
		MaterializerVersion: "materializer-v1",
		ParquetPath:         parquetPath,
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert materialized table: %v", err)
	}
	engine := &fakeParquetQueryEngine{rows: []map[string]interface{}{{"ID": uint32(123)}}}
	svc := NewService(config.DefaultHTTPConfig(), nil)
	svc.SetMetadataDBForTest(db)
	svc.SetQueryEngineForTest(engine)
	svc.SetMaterializeFuncForTest(func(context.Context, RequestContext, string) error { return nil })

	rows, err := svc.QueryDB2(context.Background(), DB2Query{
		RequestContext: RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"},
		Table:          "SpellName",
		IDField:        "ID",
		IDs:            []uint32{123},
		Fields:         []string{"ID"},
		Limit:          1,
	})
	if err != nil {
		t.Fatalf("QueryDB2: %v", err)
	}
	if len(rows) != 1 || rows[0]["ID"] != uint32(123) {
		t.Fatalf("rows = %#v", rows)
	}
	if engine.calls != 1 {
		t.Fatalf("query engine calls = %d, want 1", engine.calls)
	}
	if !strings.Contains(engine.query, strings.ReplaceAll(parquetPath, `\`, `/`)) {
		t.Fatalf("query = %q, want parquet path %q", engine.query, parquetPath)
	}
	if len(engine.args) != 1 || engine.args[0] != uint32(123) {
		t.Fatalf("query args = %#v", engine.args)
	}
}

func TestServiceQueryDB2RejectsUnsafeFilter(t *testing.T) {
	db, err := metadata.Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open metadata db: %v", err)
	}
	defer db.Close()
	if err := metadata.UpsertMaterializedTable(db, metadata.MaterializedTable{
		Region:              "cn",
		Product:             "wow",
		BuildKey:            "build-key",
		BuildName:           "12.0.0.61234",
		Locale:              "zhCN",
		TableName:           "SpellName",
		DB2FileDataID:       123,
		DBDDefinitionHash:   "dbd-hash",
		DecoderVersion:      "decoder-v1",
		MaterializerVersion: "materializer-v1",
		ParquetPath:         filepath.Join(t.TempDir(), "SpellName.parquet"),
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert materialized table: %v", err)
	}
	engine := &fakeParquetQueryEngine{}
	svc := NewService(config.DefaultHTTPConfig(), nil)
	svc.SetMetadataDBForTest(db)
	svc.SetQueryEngineForTest(engine)
	svc.SetMaterializeFuncForTest(func(context.Context, RequestContext, string) error { return nil })

	_, err = svc.QueryDB2(context.Background(), DB2Query{
		RequestContext: RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"},
		Table:          "SpellName",
		Filter:         "Name_lang = 'Fireball' OR 1=1",
	})
	if err == nil {
		t.Fatal("QueryDB2 unsafe filter error = nil")
	}
	if engine.calls != 0 {
		t.Fatalf("query engine calls = %d, want 0", engine.calls)
	}
}

func TestServiceQueryDB2BuildsSearchPredicate(t *testing.T) {
	db, err := metadata.Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open metadata db: %v", err)
	}
	defer db.Close()
	if err := metadata.UpsertMaterializedTable(db, metadata.MaterializedTable{
		Region:              "cn",
		Product:             "wow",
		BuildKey:            "build-key",
		BuildName:           "12.0.0.61234",
		Locale:              "zhCN",
		TableName:           "SpellName",
		DB2FileDataID:       123,
		DBDDefinitionHash:   "dbd-hash",
		DecoderVersion:      "decoder-v1",
		MaterializerVersion: "materializer-v1",
		ParquetPath:         filepath.Join(t.TempDir(), "SpellName.parquet"),
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert materialized table: %v", err)
	}
	engine := &fakeParquetQueryEngine{rows: []map[string]interface{}{{"ID": uint32(1)}}}
	svc := NewService(config.DefaultHTTPConfig(), nil)
	svc.SetMetadataDBForTest(db)
	svc.SetQueryEngineForTest(engine)
	svc.SetMaterializeFuncForTest(func(context.Context, RequestContext, string) error { return nil })

	if _, err := svc.QueryDB2(context.Background(), DB2Query{
		RequestContext: RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"},
		Table:          "SpellName",
		SearchField:    "Name_lang",
		SearchQuery:    "Fire",
		Limit:          3,
	}); err != nil {
		t.Fatalf("QueryDB2 search: %v", err)
	}
	if !strings.Contains(engine.query, `lower(CAST("Name_lang" AS VARCHAR)) LIKE ?`) {
		t.Fatalf("query = %q, want search predicate", engine.query)
	}
	if len(engine.args) != 1 || engine.args[0] != "%fire%" {
		t.Fatalf("args = %#v, want %%fire%%", engine.args)
	}
}

func TestServiceSchemaDB2DescribesMaterializedParquet(t *testing.T) {
	db, err := metadata.Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open metadata db: %v", err)
	}
	defer db.Close()
	parquetPath := filepath.Join(t.TempDir(), "SpellName.parquet")
	if err := metadata.UpsertMaterializedTable(db, metadata.MaterializedTable{
		Region:              "cn",
		Product:             "wow",
		BuildKey:            "build-key",
		BuildName:           "12.0.0.61234",
		Locale:              "zhCN",
		TableName:           "SpellName",
		DB2FileDataID:       123,
		DBDDefinitionHash:   "dbd-hash",
		DecoderVersion:      "decoder-v1",
		MaterializerVersion: "materializer-v1",
		ParquetPath:         parquetPath,
		RowCount:            42,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert materialized table: %v", err)
	}
	engine := &fakeParquetQueryEngine{rows: []map[string]interface{}{
		{"column_name": "ID", "column_type": "INTEGER"},
		{"column_name": "Name_lang", "column_type": "VARCHAR"},
	}}
	svc := NewService(config.DefaultHTTPConfig(), nil)
	svc.SetMetadataDBForTest(db)
	svc.SetQueryEngineForTest(engine)
	svc.SetMaterializeFuncForTest(func(context.Context, RequestContext, string) error { return nil })

	schema, err := svc.SchemaDB2(context.Background(), RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}, "SpellName")
	if err != nil {
		t.Fatalf("SchemaDB2: %v", err)
	}
	if schema.RowCount != 42 || schema.Fields["ID"] != "INTEGER" || schema.Fields["Name_lang"] != "VARCHAR" {
		t.Fatalf("schema = %#v", schema)
	}
	if !strings.Contains(engine.query, "DESCRIBE SELECT * FROM read_parquet") || !strings.Contains(engine.query, strings.ReplaceAll(parquetPath, `\`, `/`)) {
		t.Fatalf("query = %q, want DESCRIBE parquet path", engine.query)
	}
}

func TestServiceStatusReportsCacheAndContextLimits(t *testing.T) {
	cfg := config.DefaultHTTPConfig()
	cfg.Cache.Root = "test-cache"
	cfg.Contexts.MaxContexts = 7
	svc := NewService(cfg, nil)

	status := svc.Status()

	if !status.OK {
		t.Fatal("status OK = false, want true")
	}
	if status.Cache.Root != "test-cache" {
		t.Fatalf("cache root = %q, want test-cache", status.Cache.Root)
	}
	if status.Memory.MaxContexts != 7 {
		t.Fatalf("max contexts = %d, want 7", status.Memory.MaxContexts)
	}
}

type fakeParquetQueryEngine struct {
	rows  []map[string]interface{}
	err   error
	calls int
	query string
	args  []interface{}
}

func (f *fakeParquetQueryEngine) QueryParquet(ctx context.Context, query string, args []interface{}) ([]map[string]interface{}, error) {
	f.calls++
	f.query = query
	f.args = args
	return f.rows, f.err
}

type fakeRuntimeContextResolver struct {
	ctx   *appruntime.Context
	err   error
	calls int
}

func (f *fakeRuntimeContextResolver) ResolveContext(context.Context, RequestContext) (*appruntime.Context, error) {
	f.calls++
	return f.ctx, f.err
}
