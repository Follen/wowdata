# wowdata Runtime Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor wowdata so CLI, local MCP stdio, and remote MCP HTTP have separated runtimes while preserving CLI and stdio behavior and upgrading HTTP into a shared cached data service.

**Architecture:** Keep the current CLI and stdio command semantics stable while extracting reusable runtime context and query services underneath them. Build HTTP as a separate service runtime with its own config, context pool, lazy prepare, SQLite metadata, Parquet materialization, DuckDB querying, artifact manager, build watcher, and Docker deployment path.

**Tech Stack:** Go 1.26.1, Cobra, existing CASC/DB2/DBD/listfile readers, Go MCP server, SQLite, Parquet, DuckDB, Docker 20.10.24 on remote `211.154.18.253:11224`, nginx reverse proxy, PowerShell deployment scripts under `.local/wowdata/`.

---

## Scope Lock

This plan implements the accepted spec at `docs/superpowers/specs/2026-06-01-wowdata-runtime-refactor-design.md`.

The implementation must preserve these invariants in every task:

- CLI commands keep their current flags, JSON envelope shape, and error code semantics.
- `wowdata mcp stdio` keeps current tool names, including `wow_warmup`.
- CLI and stdio do not load HTTP config files.
- HTTP ordinary query tools do not expose or require `wow_warmup`.
- HTTP service code does not execute business queries by shelling through Cobra JSON stdout.
- `.local/`, SSH keys, private server config, benchmark output, and generated cache files are not committed.

Before starting Task 1, run:

```powershell
git status --short
go test ./... -count=1
```

Expected:

```text
git status --short prints no tracked modifications
go test ./... -count=1 exits 0
```

If the full test suite fails before any edit, record the failing package and failure text in the task notes, then fix only if the failure blocks the first test you need to add.

## File Structure

Create or modify these files. Each file has one responsibility.

- Modify `cmd/wowdata/main.go`: keep the root command factory and CLI startup, then delegate runtime construction to smaller packages.
- Modify `cmd/wowdata/mcp.go`: split stdio and HTTP startup into separate command helpers while keeping command text compatible.
- Create `cmd/wowdata/cli.go`: CLI-specific runtime assembly and persistent flag synchronization.
- Create `cmd/wowdata/mcp_stdio.go`: stdio-specific runtime assembly and MCP server registration.
- Create `cmd/wowdata/mcp_http.go`: HTTP-specific config loading, flag overrides, handler registration, and server startup.
- Create `internal/runtime/context.go`: build context identity and shared initialized state.
- Create `internal/runtime/context_key.go`: canonical context keys for local and remote contexts.
- Create `internal/runtime/local_runtime.go`: CLI and stdio single-context runtime.
- Create `internal/runtime/remote_runtime.go`: HTTP service-facing context constructor helpers.
- Create `internal/runtime/warmup.go`: reusable warmup orchestration shared by CLI and stdio, with no HTTP service policy.
- Create `internal/query/service.go`: service bundle interface that adapters call directly.
- Create `internal/query/db2/service.go`: DB2 query service around `runtime.DB2Store`.
- Create `internal/query/file/service.go`: file query/export service around `runtime.FileStore`.
- Create `internal/query/icon/service.go`: icon export service around `runtime.IconStore`.
- Create `internal/query/item/service.go`: item service adapter around current `internal/wowdata.ItemService`.
- Create `internal/query/spell/service.go`: spell service adapter around current `internal/wowdata.SpellService`.
- Create `internal/query/creature/service.go`: creature service adapter around current `internal/wowdata.CreatureService`.
- Create `internal/query/encounter/service.go`: encounter service adapter around current `internal/wowdata.EncounterService`.
- Create `internal/query/decor/service.go`: decor service adapter around current `internal/wowdata.DecorService`.
- Create `internal/query/video/service.go`: video demux service adapter around current video handler.
- Create `internal/adapter/cli/commands.go`: Cobra handler wiring from query services.
- Create `internal/adapter/mcp/stdio_tools.go`: stdio MCP tool definitions that preserve existing tool list.
- Create `internal/adapter/mcp/http_tools.go`: HTTP MCP tool definitions that omit `wow_warmup` by default.
- Create `internal/adapter/mcp/builds.go`: HTTP `wow_builds` tool.
- Create `internal/adapter/mcp/status.go`: HTTP `wow_status` tool.
- Create `internal/config/http.go`: HTTP config structs, defaults, YAML parsing, env overlays, and CLI flag overlays.
- Create `config/http-mcp.example.yaml`: public example config for HTTP service.
- Create `internal/service/http/service.go`: HTTP service runtime root.
- Create `internal/service/http/context_pool.go`: pinned/LRU build context pool.
- Create `internal/service/http/prepare_scheduler.go`: lazy prepare scheduler with concurrency limits.
- Create `internal/service/http/singleflight.go`: per context/table request coalescing.
- Create `internal/service/http/materializer.go`: DB2 table materialization coordinator.
- Create `internal/service/http/build_watcher.go`: product polling and atomic active context switch.
- Create `internal/service/http/status.go`: service health/status model.
- Create `internal/service/http/artifacts.go`: artifact creation and public URL mapping.
- Create `internal/cache/paths.go`: cache path normalization and traversal protection.
- Create `internal/cache/metadata/sqlite.go`: SQLite connection and migration runner.
- Create `internal/cache/metadata/builds.go`: product/build metadata persistence.
- Create `internal/cache/metadata/materialized.go`: materialized table state and stale marking.
- Create `internal/cache/metadata/audit.go`: cache prune audit records.
- Create `migrations/sqlite/0001_init.sql`: base metadata tables.
- Create `migrations/sqlite/0002_builds.sql`: products and builds.
- Create `migrations/sqlite/0003_dbd_schema.sql`: DBD schema metadata.
- Create `migrations/sqlite/0004_file_index.sql`: file index metadata.
- Create `migrations/sqlite/0005_listfile_index.sql`: listfile metadata.
- Create `migrations/sqlite/0006_materialized_tables.sql`: Parquet table metadata.
- Create `migrations/sqlite/0007_cache_audit.sql`: cache prune audit log.
- Create `internal/cache/parquet/store.go`: Parquet path and metadata validation API.
- Create `internal/cache/parquet/writer.go`: atomic Parquet writer wrapper.
- Create `internal/cache/parquet/reader.go`: Parquet reader metadata inspection.
- Create `internal/cache/duckdb/engine.go`: DuckDB engine lifecycle and availability checks.
- Create `internal/cache/duckdb/query.go`: parameterized query helpers.
- Create `internal/cache/memory/lru.go`: bounded memory cache.
- Create `internal/artifact/manager.go`: artifact manager root.
- Create `internal/artifact/paths.go`: output path validation.
- Create `internal/artifact/links.go`: local file URI and HTTP download URL generation.
- Create `internal/artifact/cleanup.go`: retention deletion.
- Create `Dockerfile.http`: HTTP MCP container image.
- Create `docs/architecture.md`: final runtime architecture.
- Create `docs/http-service-runtime.md`: HTTP runtime operation guide.
- Create `docs/cache-layout.md`: cache layout and invalidation rules.
- Create `docs/mcp-tools.md`: stdio and HTTP tool lists.
- Create `docs/deployment.md`: Docker and nginx deployment guide.
- Create `docs/performance.md`: benchmark interpretation guide.
- Modify `README.md`: public summary and links to detailed docs.
- Modify `CHANGELOG.md`: record the refactor.
- Create `.local/wowdata/bench-http.ps1`: local private benchmark driver, not committed.
- Create `.local/wowdata/deploy-http-docker.ps1`: local private Docker deployment driver, not committed.

## Task 1: Baseline Characterization

**Files:**

- Create: `internal/app/compatibility_baseline_test.go`
- Create: `cmd/wowdata/mcp_compatibility_test.go`

- [x] **Step 1: Write CLI compatibility tests**

Create `internal/app/compatibility_baseline_test.go`:

```go
package app

import (
	"encoding/json"
	"testing"
)

func TestResponseEnvelopeKeysStayStable(t *testing.T) {
	resp := NewSuccessResponse("db2 rows", map[string]interface{}{"rows": []interface{}{}})
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, key := range []string{"ok", "command", "data", "warnings"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("response envelope missing key %q in %#v", key, decoded)
		}
	}
	if decoded["ok"] != true {
		t.Fatalf("ok = %#v, want true", decoded["ok"])
	}
}

func TestErrorEnvelopeKeysStayStable(t *testing.T) {
	resp := NewErrorResponse("warmup", "not_ready", "CASC 未就绪，请先调用 wow_warmup")
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errObj, ok := decoded["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing in %#v", decoded)
	}
	if errObj["code"] != "not_ready" {
		t.Fatalf("error.code = %#v, want not_ready", errObj["code"])
	}
}
```

- [x] **Step 2: Write MCP tool compatibility tests**

Create `cmd/wowdata/mcp_compatibility_test.go`:

```go
package main

import "testing"

func TestStdioMCPToolNamesStayStable(t *testing.T) {
	rt := NewRuntime()
	server := newMCPServerForRuntime(rt)
	names := map[string]bool{}
	for _, name := range server.ToolNamesForTest() {
		names[name] = true
	}
	want := []string{
		"wow_warmup",
		"wow_casc",
		"wow_db2",
		"wow_file",
		"wow_icon",
		"wow_spell",
		"wow_encounter",
		"wow_item",
		"wow_creature",
		"wow_decor",
		"wow_video",
	}
	if len(names) != len(want) {
		t.Fatalf("tool count = %d, want %d: %#v", len(names), len(want), names)
	}
	for _, name := range want {
		if !names[name] {
			t.Fatalf("tool %q missing; all=%#v", name, names)
		}
	}
}
```

- [x] **Step 3: Add test-only tool name accessor**

Modify `internal/mcpserver/server.go`:

```go
func (s *Server) ToolNamesForTest() []string {
	names := make([]string, 0, len(s.tools))
	for name := range s.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
```

The file already imports `sort`; reuse that import.

- [x] **Step 4: Run baseline tests**

Run:

```powershell
go test ./internal/app ./cmd/wowdata -count=1
```

Expected:

```text
ok  	wowdata/internal/app
ok  	wowdata/cmd/wowdata
```

- [x] **Step 5: Run full baseline suite**

Run:

```powershell
go test ./... -count=1
```

Expected: all packages pass.

- [x] **Step 6: Commit**

Run:

```powershell
git add internal/app/compatibility_baseline_test.go cmd/wowdata/mcp_compatibility_test.go internal/mcpserver/server.go
git commit -m "test lock cli and stdio compatibility"
```

## Task 2: Runtime Context Identity

**Files:**

- Create: `internal/runtime/context_key.go`
- Create: `internal/runtime/context_key_test.go`
- Create: `internal/runtime/context.go`
- Create: `internal/runtime/context_test.go`

- [x] **Step 1: Write context key tests**

Create `internal/runtime/context_key_test.go`:

```go
package runtime

import "testing"

func TestRemoteContextKeyIncludesBuildAndLocale(t *testing.T) {
	key := RemoteContextKey{
		Region: "cn", Product: "wow", BuildKey: "abcd", Locale: "zhCN", CacheRoot: `D:\cache`,
	}.String()
	want := "remote\x00cn\x00wow\x00abcd\x00zhCN\x00D:\\cache"
	if key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
}

func TestLocalContextKeyIncludesCleanPathAndBuild(t *testing.T) {
	key := LocalContextKey{
		Path: `D:\World of Warcraft\_retail_\..\\_retail_`, Product: "wow", BuildKey: "efgh", Locale: "zhCN",
	}.String()
	if key == "" {
		t.Fatal("local key is empty")
	}
	if key == RemoteContextKey{Region: "cn", Product: "wow", BuildKey: "efgh", Locale: "zhCN"}.String() {
		t.Fatalf("local key must not equal remote key: %q", key)
	}
}
```

- [x] **Step 2: Implement context key types**

Create `internal/runtime/context_key.go`:

```go
package runtime

import (
	"path/filepath"
	"strings"
)

type RemoteContextKey struct {
	Region    string
	Product   string
	BuildKey  string
	Locale    string
	CacheRoot string
}

func (k RemoteContextKey) String() string {
	return strings.Join([]string{"remote", k.Region, k.Product, k.BuildKey, k.Locale, cleanKeyPath(k.CacheRoot)}, "\x00")
}

type LocalContextKey struct {
	Path     string
	Product  string
	BuildKey string
	Locale   string
}

func (k LocalContextKey) String() string {
	return strings.Join([]string{"local", cleanKeyPath(k.Path), k.Product, k.BuildKey, k.Locale}, "\x00")
}

func cleanKeyPath(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return filepath.Clean(value)
}
```

- [x] **Step 3: Write context state tests**

Create `internal/runtime/context_test.go`:

```go
package runtime

import "testing"

func TestContextIdentityReadyState(t *testing.T) {
	ctx := &Context{
		Source:    "remote",
		Region:    "cn",
		Product:   "wow",
		BuildName: "12.0.0.61234",
		BuildKey:  "abcd",
		Locale:    "zhCN",
	}
	if ctx.Ready() {
		t.Fatal("context without CASC metadata must not be ready")
	}
	ctx.CASCReady = true
	ctx.DBDReady = true
	ctx.TablesReady = map[string]bool{"SpellName": true}
	if !ctx.Ready() {
		t.Fatal("context with CASC, DBD, and table state should be ready")
	}
}
```

- [x] **Step 4: Implement context state**

Create `internal/runtime/context.go`:

```go
package runtime

type Context struct {
	Source      string
	Path        string
	Region      string
	Product     string
	BuildName   string
	BuildKey    string
	BuildIndex  int
	Locale      string
	CacheRoot   string
	CASCReady   bool
	DBDReady    bool
	Listfile    bool
	TablesReady map[string]bool
}

func (c *Context) Ready() bool {
	return c != nil && c.CASCReady && c.DBDReady && c.TablesReady != nil
}

func (c *Context) HasTable(table string) bool {
	return c != nil && c.TablesReady != nil && c.TablesReady[table]
}
```

- [x] **Step 5: Run tests**

Run:

```powershell
go test ./internal/runtime -count=1
```

Expected: package passes.

- [x] **Step 6: Commit**

Run:

```powershell
git add internal/runtime/context_key.go internal/runtime/context_key_test.go internal/runtime/context.go internal/runtime/context_test.go
git commit -m "runtime add context identity"
```

## Task 3: Local Runtime Boundary

**Files:**

- Create: `internal/runtime/local_runtime.go`
- Create: `internal/runtime/local_runtime_test.go`
- Modify: `cmd/wowdata/main.go`

- [x] **Step 1: Write local runtime tests**

Create `internal/runtime/local_runtime_test.go`:

```go
package runtime

import "testing"

func TestLocalRuntimeDoesNotEnableHTTPFeatures(t *testing.T) {
	rt := NewLocalRuntime(LocalRuntimeOptions{CacheRoot: "cache"})
	if rt.HTTPEnabled() {
		t.Fatal("local runtime must not enable HTTP service features")
	}
	if rt.CacheRoot() != "cache" {
		t.Fatalf("cache root = %q, want cache", rt.CacheRoot())
	}
}

func TestLocalRuntimeSingleActiveContext(t *testing.T) {
	rt := NewLocalRuntime(LocalRuntimeOptions{CacheRoot: "cache"})
	rt.SetActiveContext(&Context{Source: "remote", Region: "cn", Product: "wow", BuildKey: "a", Locale: "zhCN"})
	rt.SetActiveContext(&Context{Source: "remote", Region: "cn", Product: "wowt", BuildKey: "b", Locale: "zhCN"})
	active := rt.ActiveContext()
	if active.Product != "wowt" || active.BuildKey != "b" {
		t.Fatalf("active context = %#v, want wowt build b", active)
	}
}
```

- [x] **Step 2: Implement local runtime wrapper**

Create `internal/runtime/local_runtime.go`:

```go
package runtime

import "sync"

type LocalRuntimeOptions struct {
	CacheRoot string
}

type LocalRuntime struct {
	mu        sync.Mutex
	cacheRoot string
	active    *Context
}

func NewLocalRuntime(opts LocalRuntimeOptions) *LocalRuntime {
	return &LocalRuntime{cacheRoot: opts.CacheRoot}
}

func (r *LocalRuntime) HTTPEnabled() bool {
	return false
}

func (r *LocalRuntime) CacheRoot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cacheRoot
}

func (r *LocalRuntime) SetCacheRoot(value string) {
	r.mu.Lock()
	r.cacheRoot = value
	r.mu.Unlock()
}

func (r *LocalRuntime) SetActiveContext(ctx *Context) {
	r.mu.Lock()
	r.active = ctx
	r.mu.Unlock()
}

func (r *LocalRuntime) ActiveContext() *Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return nil
	}
	copy := *r.active
	return &copy
}
```

- [x] **Step 3: Add adapter field without changing behavior**

Modify `cmd/wowdata/main.go` `Runtime`:

```go
type Runtime struct {
	mu                 sync.Mutex
	localRuntime       *appruntime.LocalRuntime
	httpWarmupMu       sync.Mutex
	httpWarmupGate     bool
	httpWarmupInFlight bool
	CacheRoot          string
	CASC               *casc.CASCRemote
	Local              *casc.CASCLocal
	LF                 *listfile.Listfile
	DB2                *appruntime.MemoryDB2Store
	Keys               *tact.KeyRing
	Spell              *wowdata.SpellService
	Enc                *wowdata.EncounterService
	Item               *wowdata.ItemService
	Creat              *wowdata.CreatureService
	Decor              *wowdata.DecorService
	Diag               *diagnostics.DiagnosticsService
	warmup             *warmupState
	contextCacheMax    int
	contexts           map[string]*runtimeContext
	contextLRU         []string
}
```

Modify `NewRuntime` so `CacheRoot` and `localRuntime` agree:

```go
cacheRoot := resolveCacheRoot("", os.Executable)
return &Runtime{
	CacheRoot:    cacheRoot,
	localRuntime: appruntime.NewLocalRuntime(appruntime.LocalRuntimeOptions{CacheRoot: cacheRoot}),
	LF:           listfile.New(),
	DB2:          appruntime.NewMemoryDB2Store(),
	Spell:        wowdata.NewSpellService(),
	Enc:          wowdata.NewEncounterService(),
	Item:         wowdata.NewItemService(),
	Creat:        wowdata.NewCreatureService(),
	Decor:        wowdata.NewDecorService(),
	Diag:         diagnostics.NewDiagnosticsService(),
}
```

- [x] **Step 4: Run tests**

Run:

```powershell
go test ./internal/runtime ./cmd/wowdata -count=1
```

Expected: both packages pass.

- [x] **Step 5: Commit**

Run:

```powershell
git add internal/runtime/local_runtime.go internal/runtime/local_runtime_test.go cmd/wowdata/main.go
git commit -m "runtime add local boundary"
```

## Task 4: Query Service Extraction

**Files:**

- Create: `internal/query/service.go`
- Create: `internal/query/db2/service.go`
- Create: `internal/query/db2/service_test.go`
- Create: `internal/query/file/service.go`
- Create: `internal/query/icon/service.go`
- Create: `internal/query/item/service.go`
- Create: `internal/query/spell/service.go`
- Create: `internal/query/creature/service.go`
- Create: `internal/query/encounter/service.go`
- Create: `internal/query/decor/service.go`
- Create: `internal/query/video/service.go`

- [x] **Step 1: Write DB2 service tests**

Create `internal/query/db2/service_test.go`:

```go
package db2

import (
	"testing"

	appruntime "wowdata/internal/runtime"
)

type fakeStore struct{}

func (fakeStore) Schema(string) ([]appruntime.SchemaField, int, error) {
	return []appruntime.SchemaField{{Name: "ID", Type: "uint32"}}, 1, nil
}
func (fakeStore) Rows(string, []uint32, []string, string, int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"ID": uint32(1)}}, nil
}
func (fakeStore) Search(string, string, string, int) ([]map[string]interface{}, error) { return nil, nil }
func (fakeStore) ForeignKey(string, string, uint32, int) ([]map[string]interface{}, error) {
	return nil, nil
}
func (fakeStore) Stream(string, []string, string, int) ([]map[string]interface{}, error) {
	return nil, nil
}

func TestServiceRowsCallsStore(t *testing.T) {
	svc := NewService(fakeStore{})
	rows, err := svc.Rows(Query{Table: "SpellName", IDs: []uint32{1}, Limit: 1})
	if err != nil {
		t.Fatalf("Rows returned error: %v", err)
	}
	if len(rows) != 1 || rows[0]["ID"] != uint32(1) {
		t.Fatalf("rows = %#v, want ID 1", rows)
	}
}

func TestServiceRejectsEmptyTable(t *testing.T) {
	svc := NewService(fakeStore{})
	if _, err := svc.Rows(Query{}); err == nil {
		t.Fatal("Rows with empty table returned nil error")
	}
}
```

- [x] **Step 2: Implement DB2 service**

Create `internal/query/db2/service.go`:

```go
package db2

import (
	"fmt"
	"strings"

	appruntime "wowdata/internal/runtime"
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

func (s *Service) Rows(q Query) ([]map[string]interface{}, error) {
	if strings.TrimSpace(q.Table) == "" {
		return nil, fmt.Errorf("table is required")
	}
	return s.store.Rows(q.Table, q.IDs, q.Fields, q.Filter, q.Limit)
}

func (s *Service) Schema(table string) ([]appruntime.SchemaField, int, error) {
	if strings.TrimSpace(table) == "" {
		return nil, 0, fmt.Errorf("table is required")
	}
	return s.store.Schema(table)
}
```

- [x] **Step 3: Add service bundle**

Create `internal/query/service.go`:

```go
package query

import querydb2 "wowdata/internal/query/db2"

type Services struct {
	DB2 *querydb2.Service
}
```

- [x] **Step 4: Add spell wrapper**

Create `internal/query/spell/service.go`:

```go
package spell

import "wowdata/internal/wowdata"

type Service struct {
	inner *wowdata.SpellService
}

func NewService(inner *wowdata.SpellService) *Service {
	return &Service{inner: inner}
}
```

- [x] **Step 5: Add item wrapper**

Create `internal/query/item/service.go`:

```go
package item

import "wowdata/internal/wowdata"

type Service struct {
	inner *wowdata.ItemService
}

func NewService(inner *wowdata.ItemService) *Service {
	return &Service{inner: inner}
}
```

- [x] **Step 6: Add creature wrapper**

Create `internal/query/creature/service.go`:

```go
package creature

import "wowdata/internal/wowdata"

type Service struct {
	inner *wowdata.CreatureService
}

func NewService(inner *wowdata.CreatureService) *Service {
	return &Service{inner: inner}
}
```

- [x] **Step 7: Add encounter wrapper**

Create `internal/query/encounter/service.go`:

```go
package encounter

import "wowdata/internal/wowdata"

type Service struct {
	inner *wowdata.EncounterService
}

func NewService(inner *wowdata.EncounterService) *Service {
	return &Service{inner: inner}
}
```

- [x] **Step 8: Add decor wrapper**

Create `internal/query/decor/service.go`:

```go
package decor

import "wowdata/internal/wowdata"

type Service struct {
	inner *wowdata.DecorService
}

func NewService(inner *wowdata.DecorService) *Service {
	return &Service{inner: inner}
}
```

- [x] **Step 9: Add file wrapper**

Create `internal/query/file/service.go`:

```go
package file

import appruntime "wowdata/internal/runtime"

type Service struct {
	store appruntime.FileStore
}

func NewService(store appruntime.FileStore) *Service {
	return &Service{store: store}
}
```

- [x] **Step 10: Add icon wrapper**

Create `internal/query/icon/service.go`:

```go
package icon

import appruntime "wowdata/internal/runtime"

type Service struct {
	store appruntime.IconStore
}

func NewService(store appruntime.IconStore) *Service {
	return &Service{store: store}
}
```

- [x] **Step 11: Add video wrapper**

Create `internal/query/video/service.go`:

```go
package video

type Service struct{}

func NewService() *Service {
	return &Service{}
}
```

Do not move business logic in this task.

- [x] **Step 12: Run query tests**

Run:

```powershell
go test ./internal/query/... -count=1
```

Expected: all query packages pass.

- [x] **Step 13: Run compatibility tests**

Run:

```powershell
go test ./internal/app ./cmd/wowdata -count=1
```

Expected: compatibility tests still pass.

- [x] **Step 14: Commit**

Run:

```powershell
git add internal/query
git commit -m "query add service boundary"
```

## Task 5: CLI and stdio Adapter Split

**Files:**

- Create: `cmd/wowdata/cli.go`
- Create: `cmd/wowdata/mcp_stdio.go`
- Modify: `cmd/wowdata/main.go`
- Modify: `cmd/wowdata/mcp.go`
- Modify: `cmd/wowdata/mcp_compatibility_test.go`

- [x] **Step 1: Write command path tests**

Add to `cmd/wowdata/mcp_compatibility_test.go`:

```go
func TestMCPCommandHasStdioAndHTTPSubcommands(t *testing.T) {
	rt := NewRuntime()
	root := newRootCommandForRuntime(rt)
	mcp, _, err := root.Find([]string{"mcp"})
	if err != nil {
		t.Fatalf("find mcp: %v", err)
	}
	for _, name := range []string{"stdio", "http"} {
		if child, _, err := mcp.Find([]string{name}); err != nil || child.Name() != name {
			t.Fatalf("mcp subcommand %q missing: child=%v err=%v", name, child, err)
		}
	}
}
```

- [x] **Step 2: Move CLI assembly into `cli.go`**

Create `cmd/wowdata/cli.go` with:

```go
package main

import (
	"os"
	"strings"

	"wowdata/internal/app"
	appruntime "wowdata/internal/runtime"
	"wowdata/internal/wowdata"

	"github.com/spf13/cobra"
)

func newCLIServiceForRuntime(rt *Runtime) *app.Service {
	fileStore := appruntime.NewCASCFileStore(rt.LF, nil, rt)
	return &app.Service{
		Warmup:    warmupHandler(rt),
		Casc:      cascHandler(rt),
		DB2:       app.NewDB2HandlerWithStore(rt.DB2),
		Spell:     app.NewSpellHandler(wowdata.NewSpellServiceWithDB2(rt.DB2)),
		Encounter: app.NewEncounterHandler(wowdata.NewEncounterServiceWithDB2(rt.DB2)),
		File:      app.NewFileHandlerWithStore(fileStore),
		Icon:      app.NewIconHandlerWithStore(fileStore),
		Item:      app.NewItemHandler(wowdata.NewItemServiceWithDB2(rt.DB2)),
		Creature:  app.NewCreatureHandler(wowdata.NewCreatureServiceWithDB2(rt.DB2)),
		Decor:     app.NewDecorHandler(wowdata.NewDecorServiceWithDB2(rt.DB2)),
		Video:     app.NewVideoHandler(),
		Golden:    app.NewGoldenHandler(),
	}
}

func attachCLIPersistentPreRun(cmd *cobra.Command, rt *Runtime) {
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		syncRuntimeFromPersistentFlags(cmd, rt)
		autoWarmup, _ := commandBoolFlag(cmd, "auto-warmup")
		if !autoWarmup || cmd.CommandPath() == "wowdata warmup" || strings.HasPrefix(cmd.CommandPath(), "wowdata mcp") {
			return nil
		}
		opts := warmupOptionsFromCommand(cmd)
		_, err := rt.initialize(opts)
		return err
	}
}

func syncRuntimeFromPersistentFlags(cmd *cobra.Command, rt *Runtime) {
	cacheRoot, _ := commandStringFlag(cmd, "cache")
	if cacheRoot != "" {
		rt.CacheRoot = resolveCacheRoot(cacheRoot, os.Executable)
		if rt.localRuntime != nil {
			rt.localRuntime.SetCacheRoot(rt.CacheRoot)
		}
	}
}
```

- [x] **Step 3: Move stdio startup into `mcp_stdio.go`**

Create `cmd/wowdata/mcp_stdio.go`:

```go
package main

import (
	"os"

	"github.com/spf13/cobra"
)

func newMCPStdioCommand(rt *Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "stdio",
		Short: "Serve MCP tools over stdio.",
		Long: `Serve MCP tools over stdio.

Use this for local clients that launch wowdata as a subprocess.

Codex CLI:
  codex mcp add wowdata -- wowdata mcp stdio

Claude Code:
  claude mcp add wowdata -- wowdata mcp stdio

cc-switch custom MCP:
  {
    "type": "stdio",
    "command": "wowdata",
    "args": ["mcp", "stdio"]
  }

Legacy compatibility:
  wowdata --mcp is treated as wowdata mcp stdio.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			server := newMCPServerForRuntime(rt)
			return server.Serve(cmd.Context(), os.Stdin, os.Stdout)
		},
	}
}
```

- [x] **Step 4: Reduce `main.go` root construction**

Modify `newRootCommandForRuntime` in `cmd/wowdata/main.go`:

```go
func newRootCommandForRuntime(rt *Runtime) *cobra.Command {
	cmd := app.NewRootCommandWithService(newCLIServiceForRuntime(rt))
	registerMCPCommand(cmd, rt)
	attachCLIPersistentPreRun(cmd, rt)
	return cmd
}
```

Remove imports from `main.go` that are no longer used after moving service construction.

- [x] **Step 5: Run split tests**

Run:

```powershell
go test ./cmd/wowdata ./internal/app -count=1
```

Expected: both packages pass.

- [x] **Step 6: Commit**

Run:

```powershell
git add cmd/wowdata/main.go cmd/wowdata/cli.go cmd/wowdata/mcp.go cmd/wowdata/mcp_stdio.go cmd/wowdata/mcp_compatibility_test.go
git commit -m "cmd split cli and stdio startup"
```

## Task 6: HTTP Config Loader

**Files:**

- Create: `internal/config/http.go`
- Create: `internal/config/http_test.go`
- Create: `config/http-mcp.example.yaml`
- Modify: `go.mod`
- Modify: `go.sum`

- [x] **Step 1: Add YAML dependency**

Run:

```powershell
go get gopkg.in/yaml.v3@v3.0.1
```

Expected: `go.mod` and `go.sum` include `gopkg.in/yaml.v3`.

- [x] **Step 2: Write config tests**

Create `internal/config/http_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultHTTPConfig(t *testing.T) {
	cfg := DefaultHTTPConfig()
	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 9788 {
		t.Fatalf("server default = %#v", cfg.Server)
	}
	if cfg.Defaults.Region != "cn" || cfg.Defaults.Product != "wow" || cfg.Defaults.Locale != "zhCN" {
		t.Fatalf("defaults = %#v", cfg.Defaults)
	}
	if cfg.Tools.ExposeAdminTools {
		t.Fatal("admin tools must be disabled by default")
	}
}

func TestLoadHTTPConfigFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "http-mcp.yaml")
	data := []byte("server:\n  host: 0.0.0.0\n  port: 9999\ncontexts:\n  max_contexts: 2\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadHTTPConfig(path)
	if err != nil {
		t.Fatalf("LoadHTTPConfig: %v", err)
	}
	if cfg.Server.Host != "0.0.0.0" || cfg.Server.Port != 9999 || cfg.Contexts.MaxContexts != 2 {
		t.Fatalf("cfg = %#v", cfg)
	}
	if cfg.Defaults.Locale != "zhCN" {
		t.Fatalf("default locale not retained: %#v", cfg.Defaults)
	}
}

func TestHTTPConfigRejectsInvalidPort(t *testing.T) {
	cfg := DefaultHTTPConfig()
	cfg.Server.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted port 0")
	}
}
```

- [x] **Step 3: Implement config loader**

Create `internal/config/http.go` with structs matching the spec and these functions:

```go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type HTTPConfig struct {
	Server    HTTPServerConfig    `yaml:"server"`
	Defaults  HTTPDefaultsConfig  `yaml:"defaults"`
	Contexts  HTTPContextsConfig  `yaml:"contexts"`
	Cache     HTTPCacheConfig     `yaml:"cache"`
	Artifacts HTTPArtifactsConfig `yaml:"artifacts"`
	Prepare   HTTPPrepareConfig   `yaml:"prepare"`
	Refresh   HTTPRefreshConfig   `yaml:"refresh"`
	Limits    HTTPLimitsConfig    `yaml:"limits"`
	Tools     HTTPToolsConfig     `yaml:"tools"`
}

type HTTPServerConfig struct {
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"base_url"`
}

type HTTPDefaultsConfig struct {
	Region  string `yaml:"region"`
	Product string `yaml:"product"`
	Locale  string `yaml:"locale"`
}

type HTTPContextsConfig struct {
	MaxContexts int                  `yaml:"max_contexts"`
	Pinned      []HTTPPinnedContext  `yaml:"pinned"`
}

type HTTPPinnedContext struct {
	Region  string `yaml:"region"`
	Product string `yaml:"product"`
	Locale  string `yaml:"locale"`
	Label   string `yaml:"label"`
}

type HTTPCacheConfig struct {
	Root       string `yaml:"root"`
	MetadataDB string `yaml:"metadata_db"`
	RawDir     string `yaml:"raw_dir"`
	DB2Dir     string `yaml:"db2_dir"`
	DuckDBPath string `yaml:"duckdb_path"`
}

type HTTPArtifactsConfig struct {
	Root           string `yaml:"root"`
	BaseURL        string `yaml:"base_url"`
	RetentionHours int   `yaml:"retention_hours"`
}

type HTTPPrepareConfig struct {
	Lazy          bool     `yaml:"lazy"`
	PrewarmOnStart bool   `yaml:"prewarm_on_start"`
	DBDManifest   bool    `yaml:"dbd_manifest"`
	Listfile      bool    `yaml:"listfile"`
	DefaultTables []string `yaml:"default_tables"`
}

type HTTPRefreshConfig struct {
	ProductCheckIntervalMinutes int  `yaml:"product_check_interval_minutes"`
	AutoPrepareNewBuilds       bool `yaml:"auto_prepare_new_builds"`
	KeepBuildsPerProduct       int  `yaml:"keep_builds_per_product"`
	PruneOnStart               bool `yaml:"prune_on_start"`
	MaxCacheGB                 int  `yaml:"max_cache_gb"`
}

type HTTPLimitsConfig struct {
	MaxConcurrentPrepares         int `yaml:"max_concurrent_prepares"`
	MaxConcurrentMaterializations int `yaml:"max_concurrent_materializations"`
	MaxConcurrentQueries          int `yaml:"max_concurrent_queries"`
	RequestTimeoutSeconds         int `yaml:"request_timeout_seconds"`
	MaterializeTimeoutSeconds     int `yaml:"materialize_timeout_seconds"`
}

type HTTPToolsConfig struct {
	ExposeAdminTools bool `yaml:"expose_admin_tools"`
	ExposeDebugTools bool `yaml:"expose_debug_tools"`
}

func DefaultHTTPConfig() HTTPConfig {
	return HTTPConfig{
		Server:   HTTPServerConfig{Host: "127.0.0.1", Port: 9788},
		Defaults: HTTPDefaultsConfig{Region: "cn", Product: "wow", Locale: "zhCN"},
		Contexts: HTTPContextsConfig{MaxContexts: 4, Pinned: []HTTPPinnedContext{
			{Region: "cn", Product: "wow", Locale: "zhCN", Label: "CN Retail"},
			{Region: "cn", Product: "wowt", Locale: "zhCN", Label: "CN PTR"},
			{Region: "cn", Product: "wow_classic", Locale: "zhCN", Label: "CN Classic"},
			{Region: "cn", Product: "wow_classic_titan", Locale: "zhCN", Label: "CN Titan"},
		}},
		Cache: HTTPCacheConfig{
			Root: "/opt/wowdata/cache", MetadataDB: "/opt/wowdata/cache/metadata.sqlite",
			RawDir: "/opt/wowdata/cache/raw", DB2Dir: "/opt/wowdata/cache/db2", DuckDBPath: "/opt/wowdata/cache/duckdb/wowdata.duckdb",
		},
		Artifacts: HTTPArtifactsConfig{Root: "/opt/wowdata/output", RetentionHours: 24},
		Prepare: HTTPPrepareConfig{Lazy: true, PrewarmOnStart: true, DBDManifest: true, DefaultTables: []string{
			"SpellName", "Spell", "SpellEffect", "SpellMisc", "Item", "ItemSparse", "ItemEffect",
			"ItemModifiedAppearance", "ItemAppearance", "ItemDisplayInfo", "TextureFileData", "ModelFileData",
			"CreatureDisplayInfo", "CreatureModelData", "HouseDecor",
		}},
		Refresh: HTTPRefreshConfig{ProductCheckIntervalMinutes: 30, AutoPrepareNewBuilds: true, KeepBuildsPerProduct: 2, PruneOnStart: true, MaxCacheGB: 80},
		Limits:  HTTPLimitsConfig{MaxConcurrentPrepares: 1, MaxConcurrentMaterializations: 2, MaxConcurrentQueries: 8, RequestTimeoutSeconds: 120, MaterializeTimeoutSeconds: 600},
	}
}

func LoadHTTPConfig(path string) (HTTPConfig, error) {
	cfg := DefaultHTTPConfig()
	if path == "" {
		return cfg, cfg.Validate()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func (c HTTPConfig) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if c.Defaults.Region == "" || c.Defaults.Product == "" || c.Defaults.Locale == "" {
		return fmt.Errorf("defaults.region, defaults.product, and defaults.locale are required")
	}
	if c.Contexts.MaxContexts < 1 {
		return fmt.Errorf("contexts.max_contexts must be at least 1")
	}
	if c.Limits.MaxConcurrentPrepares < 1 || c.Limits.MaxConcurrentMaterializations < 1 || c.Limits.MaxConcurrentQueries < 1 {
		return fmt.Errorf("limits concurrency values must be at least 1")
	}
	return nil
}
```

- [x] **Step 4: Add example config**

Create `config/http-mcp.example.yaml` using the exact values from the spec, with `server.base_url: http://211.154.18.253:11224` and `artifacts.base_url: http://211.154.18.253:11224/files`.

- [x] **Step 5: Run config tests**

Run:

```powershell
go test ./internal/config -count=1
```

Expected: package passes.

- [x] **Step 6: Prove CLI and stdio do not read HTTP config**

Run:

```powershell
go test ./cmd/wowdata -run "TestStdioMCPToolNamesStayStable|TestMCPCommandHasStdioAndHTTPSubcommands" -count=1
```

Expected: tests pass without a config file.

- [x] **Step 7: Commit**

Run:

```powershell
git add go.mod go.sum internal/config config/http-mcp.example.yaml
git commit -m "config add http mcp settings"
```

## Task 7: HTTP Context Pool and Scheduler

**Files:**

- Create: `internal/service/http/context_pool.go`
- Create: `internal/service/http/context_pool_test.go`
- Create: `internal/service/http/singleflight.go`
- Create: `internal/service/http/singleflight_test.go`
- Create: `internal/service/http/prepare_scheduler.go`
- Create: `internal/service/http/prepare_scheduler_test.go`

- [x] **Step 1: Write context pool tests**

Create `internal/service/http/context_pool_test.go`:

```go
package http

import (
	"testing"

	appruntime "wowdata/internal/runtime"
)

func TestContextPoolKeepsPinnedDuringEviction(t *testing.T) {
	pool := NewContextPool(2)
	pool.Pin("pinned")
	pool.Put("pinned", &appruntime.Context{Product: "wow", BuildKey: "a"})
	pool.Put("normal-a", &appruntime.Context{Product: "wowt", BuildKey: "b"})
	pool.Put("normal-b", &appruntime.Context{Product: "wow_classic", BuildKey: "c"})
	if _, ok := pool.Get("pinned"); !ok {
		t.Fatal("pinned context was evicted")
	}
	if _, ok := pool.Get("normal-a"); ok {
		t.Fatal("least recently used normal context was not evicted")
	}
}
```

- [x] **Step 2: Implement context pool**

Create `internal/service/http/context_pool.go` with `NewContextPool(max int)`, `Pin(key string)`, `Put(key string, ctx *runtime.Context)`, `Get(key string) (*runtime.Context, bool)`, and LRU eviction that never removes pinned keys.

- [x] **Step 3: Write singleflight tests**

Create `internal/service/http/singleflight_test.go`:

```go
package http

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestSingleflightRunsSameKeyOnce(t *testing.T) {
	group := NewSingleflight()
	var calls int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := group.Do("cn/wow/SpellName", func() (interface{}, error) {
				atomic.AddInt32(&calls, 1)
				return "ok", nil
			})
			if err != nil || value.(string) != "ok" {
				t.Errorf("value=%#v err=%v", value, err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
```

- [x] **Step 4: Implement singleflight**

Create `internal/service/http/singleflight.go` with a mutex, in-flight map, per-call wait channel, result value, and error. Delete the key after waiters are released.

- [x] **Step 5: Write scheduler limit tests**

Create `internal/service/http/prepare_scheduler_test.go`:

```go
package http

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrepareSchedulerLimitsConcurrency(t *testing.T) {
	s := NewPrepareScheduler(1)
	var running int32
	var maxRunning int32
	run := func(context.Context) error {
		now := atomic.AddInt32(&running, 1)
		if now > atomic.LoadInt32(&maxRunning) {
			atomic.StoreInt32(&maxRunning, now)
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return nil
	}
	done := make(chan error, 2)
	go func() { done <- s.Run(context.Background(), run) }()
	go func() { done <- s.Run(context.Background(), run) }()
	if err := <-done; err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("second run: %v", err)
	}
	if maxRunning != 1 {
		t.Fatalf("maxRunning = %d, want 1", maxRunning)
	}
}
```

- [x] **Step 6: Implement scheduler**

Create `internal/service/http/prepare_scheduler.go` with a buffered channel semaphore and `Run(ctx context.Context, fn func(context.Context) error) error`.

- [x] **Step 7: Run tests**

Run:

```powershell
go test ./internal/service/http -count=1
```

Expected: package passes.

- [x] **Step 8: Commit**

Run:

```powershell
git add internal/service/http
git commit -m "http service add context pool scheduler"
```

## Task 8: SQLite Metadata and Cache Paths

**Files:**

- Create: `internal/cache/paths.go`
- Create: `internal/cache/paths_test.go`
- Create: `internal/cache/metadata/sqlite.go`
- Create: `internal/cache/metadata/sqlite_test.go`
- Create: `internal/cache/metadata/materialized.go`
- Create: `internal/cache/metadata/materialized_test.go`
- Create: `migrations/sqlite/0001_init.sql`
- Create: `migrations/sqlite/0002_builds.sql`
- Create: `migrations/sqlite/0003_dbd_schema.sql`
- Create: `migrations/sqlite/0004_file_index.sql`
- Create: `migrations/sqlite/0005_listfile_index.sql`
- Create: `migrations/sqlite/0006_materialized_tables.sql`
- Create: `migrations/sqlite/0007_cache_audit.sql`
- Modify: `go.mod`
- Modify: `go.sum`

- [x] **Step 1: Add SQLite dependency**

Run:

```powershell
go get modernc.org/sqlite@v1.34.5
```

Expected: `go.mod` and `go.sum` include `modernc.org/sqlite`.

- [x] **Step 2: Write cache path tests**

Create `internal/cache/paths_test.go`:

```go
package cache

import "testing"

func TestDB2ParquetPathIncludesBuildKey(t *testing.T) {
	p := DB2ParquetPath("root", "cn", "wow", "abcd", "zhCN", "SpellName")
	if want := "root/db2/cn/wow/abcd/zhCN/SpellName.parquet"; filepathSlash(p) != want {
		t.Fatalf("path = %q, want %q", filepathSlash(p), want)
	}
}

func TestRejectTraversalArtifactPath(t *testing.T) {
	if err := EnsureUnderRoot("root", "../evil"); err == nil {
		t.Fatal("traversal path accepted")
	}
}
```

- [x] **Step 3: Implement cache paths**

Create `internal/cache/paths.go` with `DB2ParquetPath`, `RawCASCPath`, `EnsureUnderRoot`, and unexported `filepathSlash`. `EnsureUnderRoot` must use `filepath.Abs`, `filepath.Clean`, and `filepath.Rel`, and must reject `..`, absolute child paths, and empty root.

- [x] **Step 4: Write SQLite migration tests**

Create `internal/cache/metadata/sqlite_test.go`:

```go
package metadata

import (
	"path/filepath"
	"testing"
)

func TestMigrationsAreIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db, err := Open(dbPath, "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open first: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	db, err = Open(dbPath, "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open second: %v", err)
	}
	defer db.Close()
}
```

- [x] **Step 5: Implement SQLite migration runner**

Create `internal/cache/metadata/sqlite.go` with:

```go
package metadata

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"
)

func Open(path string, migrationsDir string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := migrate(db, migrationsDir); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB, dir string) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE name = ?`, name).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(data)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(name) VALUES (?)`, name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
```

- [x] **Step 6: Add migration SQL**

Create each migration with concrete tables:

```sql
CREATE TABLE IF NOT EXISTS products (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product)
);
```

```sql
CREATE TABLE IF NOT EXISTS builds (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  build_key TEXT NOT NULL,
  build_name TEXT NOT NULL,
  discovered_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  active INTEGER NOT NULL DEFAULT 0,
  ready INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(region, product, build_key)
);
```

```sql
CREATE TABLE IF NOT EXISTS dbd_schema (
  table_name TEXT NOT NULL,
  build_name TEXT NOT NULL,
  dbd_definition_hash TEXT NOT NULL,
  decoder_version TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(table_name, build_name, dbd_definition_hash, decoder_version)
);
```

```sql
CREATE TABLE IF NOT EXISTS file_index (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  build_key TEXT NOT NULL,
  file_data_id INTEGER NOT NULL,
  filename TEXT NOT NULL,
  PRIMARY KEY(region, product, build_key, file_data_id)
);
```

```sql
CREATE TABLE IF NOT EXISTS listfile_index (
  source_hash TEXT NOT NULL PRIMARY KEY,
  source_url TEXT NOT NULL,
  row_count INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

```sql
CREATE TABLE IF NOT EXISTS materialized_tables (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  build_key TEXT NOT NULL,
  build_name TEXT NOT NULL,
  locale TEXT NOT NULL,
  table_name TEXT NOT NULL,
  db2_file_data_id INTEGER NOT NULL,
  dbd_definition_hash TEXT NOT NULL,
  decoder_version TEXT NOT NULL,
  materializer_version TEXT NOT NULL,
  parquet_path TEXT NOT NULL,
  row_count INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product, build_key, locale, table_name)
);
```

```sql
CREATE TABLE IF NOT EXISTS cache_audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_time TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  actor TEXT NOT NULL,
  path TEXT NOT NULL,
  bytes INTEGER NOT NULL,
  reason TEXT NOT NULL
);
```

- [x] **Step 7: Write materialized stale tests**

Create `internal/cache/metadata/materialized_test.go` to insert a `MaterializedTable` with matching fingerprints, assert `Valid`, then change `DecoderVersion` and assert `Stale`.

- [x] **Step 8: Implement materialized table repository**

Create `internal/cache/metadata/materialized.go` with `MaterializedTable`, `UpsertMaterializedTable`, `GetMaterializedTable`, `MarkMaterializedTableStale`, and `FingerprintMatches`.

- [x] **Step 9: Run metadata tests**

Run:

```powershell
go test ./internal/cache/... -count=1
```

Expected: cache packages pass.

- [x] **Step 10: Commit**

Run:

```powershell
git add go.mod go.sum internal/cache migrations/sqlite
git commit -m "cache add sqlite metadata"
```

## Task 9: Artifact Manager

**Files:**

- Create: `internal/artifact/manager.go`
- Create: `internal/artifact/manager_test.go`
- Create: `internal/artifact/paths.go`
- Create: `internal/artifact/links.go`
- Create: `internal/artifact/cleanup.go`
- Modify: `cmd/wowdata/mcp.go`

- [x] **Step 1: Write artifact manager tests**

Create `internal/artifact/manager_test.go`:

```go
package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerReturnsDownloadURL(t *testing.T) {
	root := t.TempDir()
	m := NewManager(Config{Root: root, BaseURL: "https://mcp.example.test/files", RetentionHours: 24})
	path, link, err := m.Reserve("icons", "134400.png", "image/png")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if !strings.HasPrefix(path, root) {
		t.Fatalf("path %q not under root %q", path, root)
	}
	if link.DownloadURL != "https://mcp.example.test/files/icons/134400.png" {
		t.Fatalf("download URL = %q", link.DownloadURL)
	}
}

func TestManagerRejectsTraversal(t *testing.T) {
	m := NewManager(Config{Root: t.TempDir(), BaseURL: "https://mcp.example.test/files"})
	if _, _, err := m.Reserve("icons", "../evil.png", "image/png"); err == nil {
		t.Fatal("traversal filename accepted")
	}
}

func TestCleanupDeletesExpiredFiles(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "icons", "old.png")
	if err := os.MkdirAll(filepath.Dir(old), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(Config{Root: root, BaseURL: "https://mcp.example.test/files", RetentionHours: 0})
	if err := m.CleanupExpired(); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old file still exists or unexpected err: %v", err)
	}
}
```

- [x] **Step 2: Implement artifact manager**

Create `internal/artifact/manager.go`, `paths.go`, `links.go`, and `cleanup.go` with `Config`, `Link`, `Manager`, `Reserve`, `LinkForPath`, `CleanupExpired`, and root traversal rejection.

- [x] **Step 3: Replace HTTP artifact link helper**

Modify `cmd/wowdata/mcp.go` so `artifactConfig` calls `artifact.Manager` for default output path and download URL mapping. Existing HTTP `wow_icon` and `wow_file` tests must keep passing.

- [x] **Step 4: Run tests**

Run:

```powershell
go test ./internal/artifact ./cmd/wowdata -count=1
```

Expected: both packages pass.

- [x] **Step 5: Commit**

Run:

```powershell
git add internal/artifact cmd/wowdata/mcp.go
git commit -m "artifact add managed download links"
```

## Task 10: HTTP Service Runtime Skeleton With Real Lazy Prepare

**Files:**

- Create: `internal/service/http/service.go`
- Create: `internal/service/http/service_test.go`
- Create: `internal/service/http/status.go`
- Create: `internal/service/http/materializer.go`
- Modify: `cmd/wowdata/mcp_http.go`
- Modify: `cmd/wowdata/mcp.go`

- [x] **Step 1: Write service tests**

Create `internal/service/http/service_test.go`:

```go
package http

import (
	"context"
	"testing"

	"wowdata/internal/config"
)

func TestServiceDefaultsToCNRetail(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)
	ctx := svc.ResolveRequestContext(RequestContext{})
	if ctx.Region != "cn" || ctx.Product != "wow" || ctx.Locale != "zhCN" {
		t.Fatalf("context = %#v", ctx)
	}
}

func TestOrdinaryToolsDoNotRequireWarmupTool(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)
	if svc.ToolPolicy().ExposeWarmup {
		t.Fatal("HTTP ordinary tools must not expose wow_warmup")
	}
}

func TestEnsureTableUsesSingleflight(t *testing.T) {
	svc := NewService(config.DefaultHTTPConfig(), nil)
	var calls int
	svc.SetMaterializeFuncForTest(func(context.Context, RequestContext, string) error {
		calls++
		return nil
	})
	if err := svc.EnsureTable(context.Background(), RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}, "SpellName"); err != nil {
		t.Fatalf("EnsureTable first: %v", err)
	}
	if err := svc.EnsureTable(context.Background(), RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}, "SpellName"); err != nil {
		t.Fatalf("EnsureTable second: %v", err)
	}
	if calls != 1 {
		t.Fatalf("materialize calls = %d, want 1", calls)
	}
}
```

- [x] **Step 2: Implement HTTP service root**

Create `internal/service/http/service.go` with `Service`, `RequestContext`, `ToolPolicy`, `ResolveRequestContext`, `EnsureContext`, `EnsureTable`, `Status`, and a test hook for materialization. `EnsureTable` must cache successful table ensures by `region/product/locale/table` and use the singleflight group from Task 7.

- [x] **Step 3: Implement status model**

Create `internal/service/http/status.go`:

```go
package http

type Status struct {
	OK        bool              `json:"ok"`
	Contexts []ContextStatus   `json:"contexts"`
	Cache    CacheStatus       `json:"cache"`
	Jobs      []JobStatus       `json:"jobs"`
	Memory   MemoryStatus      `json:"memory"`
	Errors   []StructuredError `json:"errors"`
}

type ContextStatus struct {
	Region string `json:"region"`
	Product string `json:"product"`
	Locale string `json:"locale"`
	BuildKey string `json:"buildKey"`
	State string `json:"state"`
}

type CacheStatus struct {
	Root string `json:"root"`
	Pressure bool `json:"pressure"`
}

type JobStatus struct {
	Key string `json:"key"`
	State string `json:"state"`
}

type MemoryStatus struct {
	MaxContexts int `json:"maxContexts"`
	ActiveContexts int `json:"activeContexts"`
}

type StructuredError struct {
	Code string `json:"code"`
	Message string `json:"message"`
}
```

- [x] **Step 4: Implement materializer boundary**

Create `internal/service/http/materializer.go` with `Materializer` interface:

```go
package http

import "context"

type Materializer interface {
	EnsureTable(ctx context.Context, rc RequestContext, table string) error
}
```

The first implementation may delegate to existing warmup code, but only through an in-process function call. It must not call `executeCLIJSON`.

- [x] **Step 5: Wire `mcp http --config`**

Create or modify `cmd/wowdata/mcp_http.go` so HTTP startup loads `internal/config.LoadHTTPConfig`, applies flags, creates `internal/service/http.Service`, and registers HTTP handlers.

- [x] **Step 6: Run tests**

Run:

```powershell
go test ./internal/service/http ./cmd/wowdata -count=1
```

Expected: both packages pass.

- [x] **Step 7: Commit**

Run:

```powershell
git add internal/service/http cmd/wowdata/mcp.go cmd/wowdata/mcp_http.go
git commit -m "http add service runtime"
```

## Task 11: HTTP MCP Tool Split

**Files:**

- Create: `internal/adapter/mcp/http_tools.go`
- Create: `internal/adapter/mcp/http_tools_test.go`
- Create: `internal/adapter/mcp/stdio_tools.go`
- Create: `internal/adapter/mcp/stdio_tools_test.go`
- Modify: `cmd/wowdata/mcp.go`
- Modify: `cmd/wowdata/mcp_http.go`
- Modify: `cmd/wowdata/mcp_stdio.go`

- [x] **Step 1: Write stdio tool list test**

Create `internal/adapter/mcp/stdio_tools_test.go`:

```go
package mcp

import "testing"

func TestStdioToolsIncludeWarmup(t *testing.T) {
	names := StdioToolNames()
	if !contains(names, "wow_warmup") {
		t.Fatalf("stdio tools missing wow_warmup: %#v", names)
	}
	if !contains(names, "wow_db2") || !contains(names, "wow_icon") {
		t.Fatalf("stdio tools missing query tools: %#v", names)
	}
}
```

- [x] **Step 2: Write HTTP tool list test**

Create `internal/adapter/mcp/http_tools_test.go`:

```go
package mcp

import "testing"

func TestHTTPToolsExcludeWarmupByDefault(t *testing.T) {
	names := HTTPToolNames(false)
	if contains(names, "wow_warmup") {
		t.Fatalf("HTTP default tools include wow_warmup: %#v", names)
	}
	for _, name := range []string{"wow_builds", "wow_status", "wow_db2", "wow_item", "wow_icon"} {
		if !contains(names, name) {
			t.Fatalf("HTTP tools missing %s: %#v", name, names)
		}
	}
}

func TestHTTPAdminToolsAreGated(t *testing.T) {
	names := HTTPToolNames(true)
	for _, name := range []string{"wow_refresh_builds", "wow_prepare", "wow_prune_cache"} {
		if !contains(names, name) {
			t.Fatalf("HTTP admin tools missing %s: %#v", name, names)
		}
	}
}
```

- [x] **Step 3: Implement adapter tool lists**

Create `internal/adapter/mcp/stdio_tools.go` and `http_tools.go` with exported `StdioToolNames`, `HTTPToolNames`, and tool builders that return `mcpserver.Tool`.

- [x] **Step 4: Move existing CLI-backed tool mapping to stdio only**

Modify `cmd/wowdata/mcp.go` so `executeCLIJSON` remains used only by stdio tool handlers. HTTP tool handlers must call `internal/service/http.Service`.

- [x] **Step 5: Add HTTP tool handler tests**

Add tests proving:

```text
wow_status returns status without warmup
wow_builds returns products using service method
wow_db2 invokes EnsureTable before query
wow_icon maps exported path to downloadUrl
```

Use fake service implementations in tests; do not hit Blizzard CDN.

- [x] **Step 6: Run adapter tests**

Run:

```powershell
go test ./internal/adapter/mcp ./cmd/wowdata -count=1
```

Expected: packages pass.

- [x] **Step 7: Commit**

Run:

```powershell
git add internal/adapter/mcp cmd/wowdata
git commit -m "mcp split stdio and http tools"
```

## Task 12: Parquet Store and DuckDB Query Engine

**Files:**

- Create: `internal/cache/parquet/store.go`
- Create: `internal/cache/parquet/store_test.go`
- Create: `internal/cache/parquet/writer.go`
- Create: `internal/cache/parquet/reader.go`
- Create: `internal/cache/duckdb/engine.go`
- Create: `internal/cache/duckdb/engine_test.go`
- Create: `internal/cache/duckdb/query.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [x] **Step 1: Add dependencies**

Run:

```powershell
go get github.com/parquet-go/parquet-go@v0.25.1
go get github.com/marcboeker/go-duckdb/v2@v2.3.3
```

Expected: dependencies are recorded in `go.mod` and `go.sum`.

- [x] **Step 2: Write Parquet metadata tests**

Create `internal/cache/parquet/store_test.go`:

```go
package parquet

import "testing"

func TestMetadataFingerprintMismatchIsInvalid(t *testing.T) {
	want := Metadata{Region: "cn", Product: "wow", BuildKey: "a", Locale: "zhCN", Table: "SpellName", DBDDefinitionHash: "h1", DecoderVersion: "d1", MaterializerVersion: "m1", DB2FileDataID: 123}
	got := want
	got.DecoderVersion = "d2"
	if got.Matches(want) {
		t.Fatalf("metadata mismatch accepted: got=%#v want=%#v", got, want)
	}
}
```

- [x] **Step 3: Implement Parquet metadata API**

Create `internal/cache/parquet/store.go` with `Metadata`, `Matches`, `PathFor`, and `ValidateExisting`. `ValidateExisting` must return `ErrStale` on fingerprint mismatch.

- [x] **Step 4: Write DuckDB parameter test**

Create `internal/cache/duckdb/engine_test.go`:

```go
package duckdb

import "testing"

func TestBuildQueryRejectsIdentifierInjection(t *testing.T) {
	if _, _, err := SelectByID("SpellName; DROP TABLE x", "ID", 1); err == nil {
		t.Fatal("table injection accepted")
	}
	if _, _, err := SelectByID("SpellName", "ID OR 1=1", 1); err == nil {
		t.Fatal("field injection accepted")
	}
}
```

- [x] **Step 5: Implement DuckDB engine**

Create `internal/cache/duckdb/engine.go` and `query.go` with:

```go
func IsSafeIdentifier(value string) bool
func SelectByID(table string, field string, id uint32) (string, []interface{}, error)
```

`SelectByID` must return SQL with a `?` parameter marker and args slice containing `id`.

- [x] **Step 6: Integrate materializer**

Modify `internal/service/http/materializer.go` so successful DB2 decode writes Parquet metadata, validates the written file, and records SQLite `materialized_tables`. If Parquet or DuckDB is unavailable, return `query_engine_unavailable` for paths that require DuckDB.

- [x] **Step 7: Run cache tests**

Run:

```powershell
go test ./internal/cache/... ./internal/service/http -count=1
```

Expected: all packages pass.

- [x] **Step 8: Commit**

Run:

```powershell
git add go.mod go.sum internal/cache/parquet internal/cache/duckdb internal/service/http docs/superpowers/plans/2026-06-01-wowdata-runtime-refactor.md
git commit -m "cache implement parquet duckdb engine"
```

## Task 13: Build Watcher, Atomic Switch, and Prune

**Files:**

- Create: `internal/service/http/build_watcher.go`
- Create: `internal/service/http/build_watcher_test.go`
- Create: `internal/service/http/prune.go`
- Create: `internal/service/http/prune_test.go`
- Modify: `internal/cache/metadata/builds.go`
- Modify: `internal/cache/metadata/audit.go`

- [x] **Step 1: Write atomic switch test**

Create `internal/service/http/build_watcher_test.go`:

```go
package http

import (
	"context"
	"errors"
	"testing"
)

func TestBuildWatcherKeepsOldContextWhenPrepareFails(t *testing.T) {
	svc := newServiceForWatcherTest("old")
	watcher := NewBuildWatcher(svc, func(context.Context, RequestContext) (string, error) {
		return "new", nil
	})
	svc.SetPrepareContextForTest(func(context.Context, RequestContext, string) error {
		return errors.New("network down")
	})
	if err := watcher.CheckOnce(context.Background(), RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}); err == nil {
		t.Fatal("CheckOnce returned nil error")
	}
	active := svc.ActiveBuildKeyForTest("cn", "wow", "zhCN")
	if active != "old" {
		t.Fatalf("active build = %q, want old", active)
	}
}

func TestBuildWatcherSwitchesAfterPrepareSuccess(t *testing.T) {
	svc := newServiceForWatcherTest("old")
	watcher := NewBuildWatcher(svc, func(context.Context, RequestContext) (string, error) {
		return "new", nil
	})
	svc.SetPrepareContextForTest(func(context.Context, RequestContext, string) error {
		return nil
	})
	if err := watcher.CheckOnce(context.Background(), RequestContext{Region: "cn", Product: "wow", Locale: "zhCN"}); err != nil {
		t.Fatalf("CheckOnce: %v", err)
	}
	active := svc.ActiveBuildKeyForTest("cn", "wow", "zhCN")
	if active != "new" {
		t.Fatalf("active build = %q, want new", active)
	}
}
```

- [x] **Step 2: Implement build watcher**

Create `internal/service/http/build_watcher.go` with `NewBuildWatcher`, `CheckOnce`, and atomic switch that updates only active context metadata after prepare succeeds.

- [x] **Step 3: Write prune protection test**

Create `internal/service/http/prune_test.go`:

```go
package http

import "testing"

func TestPruneRejectsActivePinnedAndInFlight(t *testing.T) {
	p := NewPruner(PruneState{
		ActiveKeys: map[string]bool{"active": true},
		PinnedKeys: map[string]bool{"pinned": true},
		InFlightKeys: map[string]bool{"busy": true},
	})
	for _, key := range []string{"active", "pinned", "busy"} {
		if p.CanDelete(key) {
			t.Fatalf("CanDelete(%q) = true, want false", key)
		}
	}
	if !p.CanDelete("old") {
		t.Fatal("old non-protected key should be deletable")
	}
}
```

- [x] **Step 4: Implement prune policy**

Create `internal/service/http/prune.go` with `PruneState`, `Pruner`, `CanDelete`, `Plan`, and audit recording through `metadata.RecordCacheAudit`.

- [x] **Step 5: Run tests**

Run:

```powershell
go test ./internal/service/http ./internal/cache/metadata -count=1
```

Expected: packages pass.

- [x] **Step 6: Commit**

Run:

```powershell
git add internal/service/http/build_watcher.go internal/service/http/build_watcher_test.go internal/service/http/prune.go internal/service/http/prune_test.go internal/cache/metadata
git commit -m "http add build refresh and prune"
```

## Task 14: HTTP Help, Status, and Tool Documentation

**Files:**

- Modify: `cmd/wowdata/mcp_http.go`
- Modify: `cmd/wowdata/mcp.go`
- Create: `docs/mcp-tools.md`
- Create: `docs/http-service-runtime.md`

- [x] **Step 1: Write help tests**

Add to `cmd/wowdata/mcp_compatibility_test.go`:

```go
func TestHTTPHelpListsCodexClaudeAndCCSwitch(t *testing.T) {
	html := mcpHelpHTML("https://mcp.lychee-addon.online:9443")
	for _, want := range []string{
		"codex mcp add wowdata --url https://mcp.lychee-addon.online:9443/mcp",
		"claude mcp add --transport http wowdata https://mcp.lychee-addon.online:9443/mcp",
		"cc-switch",
		"wow_builds",
		"wow_status",
		"wow_db2",
		"wow_icon",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("help missing %q in %s", want, html)
		}
	}
}
```

- [x] **Step 2: Update help HTML**

Modify `mcpHelpHTML` so `/help` shows:

```text
Codex HTTP config
Claude Code HTTP config
cc-switch HTTP config
Local stdio fallback
HTTP supported tools
Admin tools when enabled
Artifact download behavior
```

Do not mention internal golden fixtures.

- [x] **Step 3: Write docs**

Create `docs/mcp-tools.md` with exact stdio and HTTP tool lists. Create `docs/http-service-runtime.md` with config path, default contexts, lazy prepare, artifact URL behavior, build watcher, and admin tool gate.

- [x] **Step 4: Run tests**

Run:

```powershell
go test ./cmd/wowdata -count=1
```

Expected: package passes.

- [x] **Step 5: Commit**

Run:

```powershell
git add cmd/wowdata docs/mcp-tools.md docs/http-service-runtime.md
git commit -m "docs update http mcp help"
```

## Task 15: Docker HTTP Deployment

**Files:**

- Create: `Dockerfile.http`
- Create: `docs/deployment.md`
- Create: `.local/wowdata/deploy-http-docker.ps1`
- Modify: `.gitignore`

- [x] **Step 1: Add ignore rules**

Modify `.gitignore`:

```gitignore
.local/
dist/
cache/
*.sqlite
*.duckdb
*.parquet
```

- [x] **Step 2: Create Dockerfile**

Create `Dockerfile.http`:

```dockerfile
FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /srv/wowdata
COPY dist/linux-amd64/wowdata /usr/local/bin/wowdata
COPY migrations /srv/wowdata/migrations
COPY config/http-mcp.example.yaml /srv/wowdata/config/http-mcp.example.yaml
EXPOSE 9788
ENTRYPOINT ["/usr/local/bin/wowdata"]
CMD ["mcp", "http", "--config", "/etc/wowdata/http-mcp.yaml"]
```

- [x] **Step 3: Create private deploy script**

Create `.local/wowdata/deploy-http-docker.ps1` with these actions:

```powershell
$ErrorActionPreference = "Stop"
. "$PSScriptRoot\deploy-release.ps1"
go env -w GOOS=linux GOARCH=amd64
go build -o dist/linux-amd64/wowdata ./cmd/wowdata
docker build -f Dockerfile.http -t wowdata:http-refactor .
docker save wowdata:http-refactor -o dist/wowdata-http-refactor.tar
scp -P $DEPLOY_PORT -i $DEPLOY_KEY dist/wowdata-http-refactor.tar "$DEPLOY_USER@$DEPLOY_HOST:/tmp/wowdata-http-refactor.tar"
ssh -p $DEPLOY_PORT -i $DEPLOY_KEY "$DEPLOY_USER@$DEPLOY_HOST" "docker load -i /tmp/wowdata-http-refactor.tar && docker run -d --name wowdata-mcp-next --restart unless-stopped -p 127.0.0.1:9788:9788 -v /opt/wowdata/config/http-mcp.yaml:/etc/wowdata/http-mcp.yaml:ro -v /opt/wowdata/cache:/var/lib/wowdata/cache -v /opt/wowdata/output:/var/lib/wowdata/artifacts wowdata:http-refactor"
ssh -p $DEPLOY_PORT -i $DEPLOY_KEY "$DEPLOY_USER@$DEPLOY_HOST" "curl -fsS http://127.0.0.1:9788/health"
ssh -p $DEPLOY_PORT -i $DEPLOY_KEY "$DEPLOY_USER@$DEPLOY_HOST" "docker rm -f wowdata-mcp 2>/dev/null || true; docker rename wowdata-mcp-next wowdata-mcp"
```

Keep this script untracked because it reads private deployment variables.

- [x] **Step 4: Write deployment docs**

Create `docs/deployment.md` with public commands only:

```powershell
go build -o dist/linux-amd64/wowdata ./cmd/wowdata
docker build -f Dockerfile.http -t wowdata:http-refactor .
```

Document the container run shape from the spec and nginx proxy `211.154.18.253:11224 -> 127.0.0.1:9788`.

- [x] **Step 5: Build image locally**

Run:

```powershell
go build -o dist/linux-amd64/wowdata ./cmd/wowdata
docker build -f Dockerfile.http -t wowdata:http-refactor .
```

Expected: image build exits 0.

- [x] **Step 6: Commit tracked files**

Run:

```powershell
git add .gitignore Dockerfile.http docs/deployment.md
git commit -m "deploy add http docker image"
```

Do not add `.local/wowdata/deploy-http-docker.ps1`.

## Task 16: Remote Docker Verification

**Files:**

- Create: `docs/performance.md`
- Create: `.local/wowdata/bench-http.ps1`

- [x] **Step 1: Verify remote Docker**

Run:

```powershell
ssh -p 11224 211.154.18.253 "docker version --format '{{.Server.Version}}'"
```

Expected:

```text
29.5.2
```

If SSH user/key must be loaded, read `.local/wowdata/deploy-release.ps1` and use its variables in PowerShell. Do not print private key contents.

- [x] **Step 2: Deploy HTTP container**

Run:

```powershell
.local\wowdata\deploy-http-docker.ps1
```

Expected:

```text
HTTP health check returns a JSON object with ok true
existing container is replaced only after next container health passes
```

- [x] **Step 3: Verify public HTTP endpoints**

Run:

```powershell
curl.exe -fsS http://211.154.18.253:11223/health
curl.exe -fsS http://211.154.18.253:11223/help
```

Expected:

```text
/health contains "ok":true
/help contains Codex, Claude Code, cc-switch, wow_builds, wow_status, wow_db2, wow_icon
```

- [x] **Step 4: Verify MCP HTTP tools list**

Run an MCP JSON-RPC `tools/list` request against `/mcp` using the existing local MCP smoke pattern in `cmd/wowdata/mcp_test.go`. Expected tool list includes HTTP tools and excludes `wow_warmup` when admin tools are disabled.

- [x] **Step 5: Create benchmark script**

Create `.local/wowdata/bench-http.ps1` that records:

```text
HTTP cold wow_db2 SpellName id query
HTTP warm wow_db2 SpellName id query
HTTP repeated wow_db2 SpellName id query
stdio warm wow_db2 SpellName id query
CLI auto-warmup wowdata db2 rows SpellName --id 1
```

Write outputs to `.local/wowdata/bench-$(Get-Date -Format yyyyMMdd-HHmmss).json`.

- [x] **Step 6: Write performance docs**

Create `docs/performance.md` explaining benchmark categories, expected relative ordering, and where private benchmark results are stored.

- [x] **Step 7: Commit docs only**

Run:

```powershell
git add docs/performance.md
git commit -m "docs add performance verification"
```

Do not add `.local/wowdata/bench-http.ps1` or benchmark output.

## Task 17: Full Regression and Release Readiness

**Files:**

- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Create: `docs/architecture.md`
- Create: `docs/cache-layout.md`

- [ ] **Step 1: Update architecture docs**

Create `docs/architecture.md` with sections:

```text
CLI runtime
stdio runtime
HTTP service runtime
Shared query services
Cache layers
Failure and rollback behavior
```

Create `docs/cache-layout.md` with exact raw CASC, SQLite, Parquet, DuckDB, artifact, buildKey, stale fingerprint, and prune rules.

- [ ] **Step 2: Update README and CHANGELOG**

Modify `README.md` to link to architecture, MCP tools, deployment, cache layout, and performance docs. Modify `CHANGELOG.md` under version `v0.0.1` with the runtime refactor summary.

- [ ] **Step 3: Run full local tests**

Run:

```powershell
go test ./... -count=1
```

Expected: all packages pass.

- [ ] **Step 4: Run real remote audit**

Run the existing real-data audit command from `.local/wowdata/` for:

```text
CN Retail
CN PTR
CN Classic
CN Titan
CN Classic Era
US Retail
EU Retail
KR Retail
TW Retail
```

Expected result categories must be one of:

```text
pass
network
current_build_unavailable
schema_missing
business_bug
```

Any `business_bug` stops the release until fixed.

- [ ] **Step 5: Verify no private files are staged**

Run:

```powershell
git status --short
git check-ignore -v .local/wowdata/deploy-http-docker.ps1 .local/wowdata/bench-http.ps1
```

Expected:

```text
.local files are ignored
no cache, SQLite, DuckDB, Parquet, dist, SSH, or benchmark files are staged
```

- [ ] **Step 6: Commit final docs**

Run:

```powershell
git add README.md CHANGELOG.md docs/architecture.md docs/cache-layout.md
git commit -m "docs finalize runtime refactor"
```

- [ ] **Step 7: Final verification**

Run:

```powershell
git status --short
go test ./... -count=1
curl.exe -fsS http://211.154.18.253:11224/health
```

Expected:

```text
git status --short prints nothing
go test ./... -count=1 exits 0
remote health returns ok true
```

## Self-Review Checklist

- Spec coverage: every spec goal maps to at least one task: CLI/stdio compatibility in Tasks 1, 3, 5, 11, 17; HTTP service runtime in Tasks 6, 7, 10, 11; SQLite/Parquet/DuckDB in Tasks 8 and 12; artifact manager in Task 9; build watcher and prune in Task 13; Docker and remote validation in Tasks 15 and 16; docs in Tasks 14 and 17.
- Test-first coverage: each implementation task begins with failing tests before implementation steps.
- Config isolation: HTTP config appears only in Tasks 6, 10, 15, and HTTP docs; CLI/stdio tests explicitly prove no config dependency.
- Error classes pinned: invalid config, path traversal, injection-like identifiers, stale Parquet metadata, prepare failure, active/pinned/in-flight prune, and remote deployment failure are each tested or verified.
- Private material protection: `.local/`, cache, database, Parquet, DuckDB, dist, and benchmark files are ignored or explicitly not staged.
- Execution cadence: each task ends with a commit and a narrow verification command.
