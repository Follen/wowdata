# wowdata HTTP MCP Optimal Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the HTTP MCP data layer as a dedicated `wowdata-server` product line while preserving local CLI and stdio behavior.

**Architecture:** Split public runtime surfaces into local and server product lines. The server path uses SQLite for metadata/listfile/CASC indexes, Parquet for DB2 table bodies, DuckDB for queries, disk raw cache for CASC blobs, `/files/...` for artifacts, and background prepare/refresh workflows. CLI and stdio remain local-runtime users and do not import server storage.

**Tech Stack:** Go 1.26.1, Cobra, Streamable HTTP MCP, SQLite migrations via `modernc.org/sqlite`, Parquet via `parquet-go`, DuckDB via `go-duckdb` with `wowdata_duckdb` build tag, Docker on remote `211.154.18.253`, PowerShell remote verification scripts under untracked `.local/wowdata/`.

---

## Source Spec

This plan implements:

```text
docs/superpowers/specs/2026-06-02-wowdata-http-mcp-optimal-server-design.md
```

## Progress Discipline

Agents must not mark checkboxes by vibes.

- A checkbox can be marked only after the exact command in that step has been run and produced the expected result.
- A checkbox for a test-writing step can be marked only after the new test exists in the named file and the listed command has failed for the expected missing behavior.
- A checkbox for an implementation step can be marked only after the implementation diff exists and the immediately preceding focused test command passes.
- If the command fails, leave the checkbox unchecked and record the command, exit code, and failure text in the task handoff.
- If a step is completed by an equivalent command, record the exact command and why it is equivalent before marking the checkbox.
- If a task changes public behavior, the task must include a failing test first and the red failure must be observed.
- Every task ends with a commit unless the task is explicitly read-only.
- Do not batch-check boxes at the end. Update each checkbox immediately after its evidence is available.
- Do not mark a step complete because another agent said it was done. The reviewing agent must read the diff and rerun the command.
- Do not mark a task complete if `git diff --check` fails, if generated local scripts are staged, or if a required commit is missing.
- Implementation step titles such as "Implement X" are not completion criteria by themselves. The completion criteria are the public API shape, state transitions, and test commands written inside that task.
- If an implementation step is too large to verify in one diff, split it during execution into smaller commits, but do not skip any test or final task command.
- The task owner may check boxes while executing. The reviewer must remove or reject any checked box that lacks the evidence block below.

Every task handoff must include this evidence block:

```text
Task:
Branch:
Commit:
Red command:
Red result:
Green command:
Green result:
Files changed:
Checkboxes marked:
Unchecked steps and why:
```

If a task has multiple red tests, include every red command. If a task intentionally has no commit, the task text must explicitly say so; otherwise missing commit means the task is incomplete.

## Review Gate For Every Task

Before the next task starts, the reviewer must run:

```powershell
git diff --check HEAD~1..HEAD
git show --stat --oneline --name-only HEAD
```

Expected:

```text
git diff --check prints nothing and exits 0
git show lists only files declared in the task, except necessary go.mod/go.sum changes
```

If a task touched files outside its declared list, the reviewer must write the reason in the handoff before accepting the task. If the reason is "cleanup", "while here", or unrelated refactor, reject the task.

## Global Preconditions

Run before Task 1:

```powershell
git status --short
$env:PATH='C:\msys64\ucrt64\bin;' + $env:PATH
$env:CGO_ENABLED='1'
$env:CC='gcc'
$env:CXX='g++'
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./... -count=1
```

Expected:

```text
git status --short prints no tracked modifications
go test ./... exits 0
```

If the baseline is not clean, stop and report the exact failing package or dirty files before editing.

## Planned File Structure

Create or modify these files during the plan.

### Product Entrypoints

- Create: `cmd/wowdata-server/main.go`
- Modify: `cmd/wowdata/main.go`
- Modify: `cmd/wowdata/cli.go`
- Modify: `cmd/wowdata/mcp.go`
- Modify: `cmd/wowdata/mcp_http.go`
- Modify: `cmd/wowdata/mcp_stdio.go`

### Public Naming and Local Surface

- Modify: `internal/app/commands.go`
- Modify: `internal/app/root.go`
- Modify: `internal/app/db2_handler.go`
- Modify: `internal/app/root_test.go`
- Modify: `internal/app/runtime_handlers_test.go`
- Modify: `internal/app/truthful_handlers_test.go`
- Modify: `internal/adapter/mcp/stdio_tools.go`
- Modify: `internal/adapter/mcp/stdio_tools_test.go`
- Modify: `internal/adapter/mcp/http_tools.go`
- Modify: `internal/adapter/mcp/http_tools_test.go`
- Modify: `cmd/wowdata/mcp_test.go`
- Modify: `cmd/wowdata/mcp_compatibility_test.go`

### Server Packages

- Create: `internal/server/config/config.go`
- Create: `internal/server/config/config_test.go`
- Create: `internal/server/runtime/runtime.go`
- Create: `internal/server/runtime/runtime_test.go`
- Create: `internal/server/health/health.go`
- Create: `internal/server/health/health_test.go`
- Create: `internal/server/storage/metadata/db.go`
- Create: `internal/server/storage/metadata/builds.go`
- Create: `internal/server/storage/metadata/tables.go`
- Create: `internal/server/storage/metadata/artifacts.go`
- Create: `internal/server/storage/metadata/db_test.go`
- Create: `internal/server/storage/listfile/index.go`
- Create: `internal/server/storage/listfile/index_test.go`
- Create: `internal/server/storage/cascindex/index.go`
- Create: `internal/server/storage/cascindex/index_test.go`
- Create: `internal/server/storage/rawcache/cache.go`
- Create: `internal/server/storage/rawcache/cache_test.go`
- Create: `internal/server/storage/parquet/materializer.go`
- Create: `internal/server/storage/parquet/materializer_test.go`
- Create: `internal/server/storage/duckdb/query.go`
- Create: `internal/server/storage/duckdb/query_test.go`
- Create: `internal/server/storage/artifacts/store.go`
- Create: `internal/server/storage/artifacts/store_test.go`
- Create: `internal/server/prepare/scheduler.go`
- Create: `internal/server/prepare/scheduler_test.go`
- Create: `internal/server/refresh/refresh.go`
- Create: `internal/server/refresh/refresh_test.go`
- Create: `internal/server/mcphttp/tools.go`
- Create: `internal/server/mcphttp/tools_test.go`
- Create: `internal/server/service/query.go`
- Create: `internal/server/service/query_test.go`
- Create: `internal/server/service/business_spell.go`
- Create: `internal/server/service/business_spell_test.go`

### Dependency Guard and Remote Verification

- Create: `internal/architecture/dependency_test.go`
- Create: `.local/wowdata/test-http-node-parity.ps1` but do not commit it.
- Create: `.local/wowdata/test-http-update-flow.ps1` but do not commit it.
- Modify: `.gitignore` if needed to keep `.local/` ignored.
- Modify: `Dockerfile.http`
- Modify: `config/http-mcp.example.yaml`
- Modify: `docs/deployment.md`
- Modify: `docs/mcp-tools.md`
- Modify: `docs/performance.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`

## Task Count

This plan has 14 tasks. Each task is intended to be independently reviewable and committable.

## Task 1: Rename Public Query Surface

**Files:**

- Modify: `internal/app/commands.go`
- Modify: `internal/app/root.go`
- Modify: `internal/app/db2_handler.go`
- Modify: `internal/app/root_test.go`
- Modify: `internal/app/runtime_handlers_test.go`
- Modify: `internal/app/truthful_handlers_test.go`
- Modify: `internal/adapter/mcp/stdio_tools.go`
- Modify: `internal/adapter/mcp/stdio_tools_test.go`
- Modify: `internal/adapter/mcp/http_tools.go`
- Modify: `internal/adapter/mcp/http_tools_test.go`
- Modify: `cmd/wowdata/mcp.go`
- Modify: `cmd/wowdata/mcp_test.go`
- Modify: `cmd/wowdata/mcp_compatibility_test.go`

- [x] **Step 1: Write failing CLI help tests for `query`**

Update `internal/app/root_test.go` so `TestRootHelp` expects `query`, and `TestAllPlannedCommandHelp` uses:

```go
{"query"}, {"query", "schema"}, {"query", "rows"}, {"query", "search"}, {"query", "foreign-key"}, {"query", "stream"},
```

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/app -run "TestRootHelp|TestAllPlannedCommandHelp" -count=1
```

Expected: FAIL with `unknown command "query"`.

- [x] **Step 2: Write failing MCP tool-name tests for `wow_query`**

Update `internal/adapter/mcp/stdio_tools_test.go`, `internal/adapter/mcp/http_tools_test.go`, and `cmd/wowdata/mcp_test.go` to expect `wow_query` and not `wow_db2`.

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/adapter/mcp ./cmd/wowdata -run "TestStdio|TestHTTPToolNames|TestAllBusinessMCPToolsAreCallable|TestMCPServerLists" -count=1
```

Expected: FAIL because `wow_query` is missing and `wow_db2` still exists.

- [x] **Step 3: Implement the public rename**

Make these exact behavior changes:

```text
CLI command:
  db2 -> query

MCP tool:
  wow_db2 -> wow_query

Response commands:
  db2 schema -> query schema
  db2 rows -> query rows
  db2 search -> query search
  db2 foreign-key -> query foreign-key
  db2 stream -> query stream
```

Keep internal names such as `DB2Store`, `DB2Query`, `internal/db2`, and cache path `db2` when they refer to the real DB2 data format.

- [x] **Step 4: Verify no legacy public surface remains**

Run:

```powershell
rg -n "wow_db2|wowdata db2|`\"db2 (schema|rows|search|foreign-key|stream)`\"" README.md docs cmd internal
```

Expected: no matches except historical specs/plans that explicitly discuss old names. If a new current doc or current test matches, fix it.

- [x] **Step 5: Run focused tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/app ./internal/adapter/mcp ./cmd/wowdata -count=1
```

Expected: PASS.

- [x] **Step 6: Commit**

Run:

```powershell
git add internal/app internal/adapter/mcp cmd/wowdata README.md docs
git commit -m "rename public query surface"
```

Expected: commit succeeds.

## Task 2: Split Product Entrypoints and Enforce Dependency Boundaries

**Files:**

- Create: `cmd/wowdata-server/main.go`
- Create: `internal/architecture/dependency_test.go`
- Create: `internal/server/runtime/runtime.go`
- Modify: `Dockerfile.http`

- [ ] **Step 1: Write failing dependency guard tests**

Create `internal/architecture/dependency_test.go`:

```go
package architecture

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestServerDoesNotImportLocalPackages(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./internal/server/...")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list server deps: %v\n%s", err, out.String())
	}
	for _, dep := range strings.Fields(out.String()) {
		if strings.Contains(dep, "/internal/local/") || strings.HasSuffix(dep, "/internal/app") {
			t.Fatalf("server dependency imports local package: %s", dep)
		}
	}
}

func TestLocalDoesNotImportServerPackages(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./cmd/wowdata", "./internal/app/...", "./internal/adapter/mcp/...")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list local deps: %v\n%s", err, out.String())
	}
	for _, dep := range strings.Fields(out.String()) {
		if strings.Contains(dep, "/internal/server/") {
			t.Fatalf("local dependency imports server package: %s", dep)
		}
	}
}
```

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/architecture -count=1
```

Expected: FAIL because `internal/server/...` does not exist yet.

- [ ] **Step 2: Add minimal server runtime package**

Create `internal/server/runtime/runtime.go`:

```go
package runtime

type Runtime struct {
	ServiceName string
}

func New() *Runtime {
	return &Runtime{ServiceName: "wowdata-server"}
}
```

- [ ] **Step 3: Add `cmd/wowdata-server` entrypoint**

Create `cmd/wowdata-server/main.go`:

```go
package main

import (
	"fmt"
	"os"

	serverruntime "wowdata/internal/server/runtime"
)

func main() {
	rt := serverruntime.New()
	if rt.ServiceName == "" {
		fmt.Fprintln(os.Stderr, "server runtime name is empty")
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Update Dockerfile target**

Modify `Dockerfile.http` so the build command is:

```dockerfile
go build -tags wowdata_duckdb -trimpath -ldflags="-s -w" -o /out/wowdata-server ./cmd/wowdata-server
```

and the runtime entrypoint invokes:

```dockerfile
ENTRYPOINT ["/usr/local/bin/wowdata-server"]
```

- [ ] **Step 5: Verify builds and dependency guard**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/architecture ./internal/server/runtime -count=1
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' build ./cmd/wowdata ./cmd/wowdata-server
```

Expected: both commands exit 0.

- [ ] **Step 6: Commit**

Run:

```powershell
git add cmd/wowdata-server internal/architecture internal/server/runtime Dockerfile.http
git commit -m "split http server product target"
```

Expected: commit succeeds.

## Task 3: Server Config Matrix and Resource Limits

**Files:**

- Create: `internal/server/config/config.go`
- Create: `internal/server/config/config_test.go`
- Modify: `config/http-mcp.example.yaml`

- [ ] **Step 1: Write failing matrix tests**

Create `internal/server/config/config_test.go` with tests:

```go
package config

import "testing"

func TestDefaultPrepareMatrixHasTwentyThreeTargets(t *testing.T) {
	cfg := Default()
	targets := cfg.Prepare.Targets
	if len(targets) != 23 {
		t.Fatalf("targets = %d, want 23", len(targets))
	}
	assertTarget(t, targets, "CN Retail", "cn", "wow", "zhCN")
	assertTarget(t, targets, "US PTR", "us", "wowt", "enUS")
	assertTarget(t, targets, "EU PTR", "eu", "wowt", "enUS")
	assertTarget(t, targets, "TW Classic Titan", "tw", "wow_classic_titan", "zhTW")
}

func TestDefaultResourceLimitsFitTenGBServer(t *testing.T) {
	limits := Default().Limits
	if limits.MaxParallelContextPrepares != 2 {
		t.Fatalf("context prepares = %d, want 2", limits.MaxParallelContextPrepares)
	}
	if limits.MaxParallelTableMaterializations != 2 {
		t.Fatalf("table materializations = %d, want 2", limits.MaxParallelTableMaterializations)
	}
	if limits.MaxParallelDownloads != 16 || limits.MaxParallelQueries != 16 {
		t.Fatalf("download/query limits = %d/%d, want 16/16", limits.MaxParallelDownloads, limits.MaxParallelQueries)
	}
	if limits.MemorySoftLimitMB != 4096 || limits.MemoryHardLimitMB != 8192 {
		t.Fatalf("memory limits = %d/%d, want 4096/8192", limits.MemorySoftLimitMB, limits.MemoryHardLimitMB)
	}
}

func assertTarget(t *testing.T, targets []PrepareTarget, label, region, product, locale string) {
	t.Helper()
	for _, target := range targets {
		if target.Label == label {
			if target.Region != region || target.Product != product || target.Locale != locale {
				t.Fatalf("%s = %#v, want %s/%s/%s", label, target, region, product, locale)
			}
			return
		}
	}
	t.Fatalf("target %q not found in %#v", label, targets)
}
```

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/config -count=1
```

Expected: FAIL because package does not exist.

- [ ] **Step 2: Implement config types and defaults**

Create `internal/server/config/config.go` with:

```go
package config

type Config struct {
	Server  ServerConfig
	Cache   CacheConfig
	Prepare PrepareConfig
	Limits  LimitsConfig
}

type ServerConfig struct {
	Host    string
	Port    int
	BaseURL string
}

type CacheConfig struct {
	Root       string
	MetadataDB string
	RawDir     string
	DB2Dir     string
	DuckDBPath string
}

type PrepareConfig struct {
	Targets []PrepareTarget
}

type PrepareTarget struct {
	Label  string
	Region string
	Product string
	Locale string
	Strict bool
}

type LimitsConfig struct {
	MaxParallelContextPrepares      int
	MaxParallelTableMaterializations int
	MaxParallelDownloads            int
	MaxParallelQueries              int
	MemorySoftLimitMB               int
	MemoryHardLimitMB               int
}

func Default() Config {
	return Config{
		Server: ServerConfig{Host: "0.0.0.0", Port: 9788},
		Cache: CacheConfig{
			Root:       "/var/lib/wowdata/cache",
			MetadataDB: "/var/lib/wowdata/cache/metadata.sqlite",
			RawDir:     "/var/lib/wowdata/cache/raw",
			DB2Dir:     "/var/lib/wowdata/cache/db2",
			DuckDBPath: "/var/lib/wowdata/cache/duckdb/wowdata.duckdb",
		},
		Prepare: PrepareConfig{Targets: defaultTargets()},
		Limits: LimitsConfig{
			MaxParallelContextPrepares:      2,
			MaxParallelTableMaterializations: 2,
			MaxParallelDownloads:            16,
			MaxParallelQueries:              16,
			MemorySoftLimitMB:               4096,
			MemoryHardLimitMB:               8192,
		},
	}
}
```

Add `defaultTargets()` in the same file with all 23 explicit targets from the spec.

- [ ] **Step 3: Verify config tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/config -count=1
```

Expected: PASS.

- [ ] **Step 4: Update example YAML**

Update `config/http-mcp.example.yaml` to include the 23 target matrix and the five resource limit fields. Keep public base URL example as `http://211.154.18.253:11223`.

- [ ] **Step 5: Commit**

Run:

```powershell
git add internal/server/config config/http-mcp.example.yaml
git commit -m "add server prepare matrix config"
```

Expected: commit succeeds.

## Task 4: Server Metadata Store and Migrations

**Files:**

- Create: `internal/server/storage/metadata/db.go`
- Create: `internal/server/storage/metadata/builds.go`
- Create: `internal/server/storage/metadata/tables.go`
- Create: `internal/server/storage/metadata/artifacts.go`
- Create: `internal/server/storage/metadata/db_test.go`
- Create: `migrations/server/0001_init.sql`
- Create: `migrations/server/0002_builds.sql`
- Create: `migrations/server/0003_materialized_tables.sql`
- Create: `migrations/server/0004_listfile.sql`
- Create: `migrations/server/0005_casc_index.sql`
- Create: `migrations/server/0006_artifacts.sql`
- Create: `migrations/server/0007_refresh.sql`

- [ ] **Step 1: Write failing migration and state tests**

Create tests in `internal/server/storage/metadata/db_test.go` with these exact test names:

```go
func TestMigrationsCreateRequiredTables(t *testing.T)
func TestBuildCandidateActivationIsAtomic(t *testing.T)
func TestFailedCandidateDoesNotReplaceActiveBuild(t *testing.T)
func TestMaterializedTableStateTransitions(t *testing.T)
func TestListfileSourceHashUpdateMarksIndexStale(t *testing.T)
func TestCASCIndexVersionUpdateMarksIndexStale(t *testing.T)
```

Each test must open a temp SQLite DB through the new metadata package and assert concrete rows.

The required table assertion in `TestMigrationsCreateRequiredTables` must check these table names exactly:

```text
schema_migrations
server_builds
server_materialized_tables
server_listfile_sources
server_casc_sources
server_artifacts
server_refresh_runs
```

`TestBuildCandidateActivationIsAtomic` must create an old active build and a newer candidate build for the same `region/product/locale`, call `ActivateBuild`, and assert exactly one active row remains.

`TestFailedCandidateDoesNotReplaceActiveBuild` must create an old active build, mark a newer candidate failed, and assert `ActiveBuild` returns the old build.

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/metadata -count=1
```

Expected: FAIL because package and migrations do not exist.

- [ ] **Step 2: Implement SQLite open and migrations**

Implement `Open(path string) (*sql.DB, error)` and migration runner in `db.go`. Use `modernc.org/sqlite`. Migrations are read from `migrations/server`.

The migration runner must insert each applied filename into `schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)` inside the same transaction as the migration body. A second `Open` on the same DB must not reapply already recorded migrations.

- [ ] **Step 3: Implement build repository**

Implement build functions:

```go
UpsertDiscoveredBuild(ctx context.Context, db *sql.DB, build Build) error
MarkBuildPreparing(ctx context.Context, db *sql.DB, key BuildKey) error
MarkBuildReady(ctx context.Context, db *sql.DB, key BuildKey) error
MarkBuildFailed(ctx context.Context, db *sql.DB, key BuildKey, message string) error
ActivateBuild(ctx context.Context, db *sql.DB, key BuildKey) error
ActiveBuild(ctx context.Context, db *sql.DB, region, product, locale string) (Build, error)
```

`ActivateBuild` must use a transaction.

- [ ] **Step 4: Implement materialized table repository**

Implement:

```go
UpsertMaterializedTable(ctx context.Context, db *sql.DB, table MaterializedTable) error
MarkMaterializedTableState(ctx context.Context, db *sql.DB, key TableKey, state TableState, message string) error
LatestValidMaterializedTable(ctx context.Context, db *sql.DB, key TableLookup) (MaterializedTable, error)
```

- [ ] **Step 5: Implement listfile, CASC, and artifact metadata rows**

Implement source hash/version state functions required by the tests. Use explicit `valid`, `stale`, `preparing`, `failed` state strings.

The listfile and CASC source update functions must compare the previous source hash/version with the incoming value and mark dependent index rows stale only when the value changes.

- [ ] **Step 6: Verify metadata tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/metadata -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

Run:

```powershell
git add internal/server/storage/metadata migrations/server
git commit -m "add server metadata store"
```

Expected: commit succeeds.

## Task 5: Health Model Shared by `/health` and `wow_status`

**Files:**

- Create: `internal/server/health/health.go`
- Create: `internal/server/health/health_test.go`
- Create: `internal/server/mcphttp/status.go`
- Create: `internal/server/mcphttp/status_test.go`

- [ ] **Step 1: Write failing health tests**

Create tests for:

```go
func TestHealthReportsLivenessReadinessMatrixMemoryStorageAndErrors(t *testing.T)
func TestNoBuildDoesNotBlockReadinessUnlessStrict(t *testing.T)
func TestWowStatusUsesSameHealthSnapshotAsHTTPHealth(t *testing.T)
```

The test snapshot must include:

```json
{
  "liveness": {"ok": true},
  "readiness": {"ok": false, "requiredTargetsReady": 1, "requiredTargetsTotal": 2},
  "matrix": {"targetsTotal": 2, "ready": 1, "preparing": 1},
  "memory": {"memorySoftLimitMB": 4096, "memoryHardLimitMB": 8192},
  "storage": {"metadataDBBytes": 1},
  "contexts": [{"label": "CN Retail", "state": "ready"}]
}
```

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/health ./internal/server/mcphttp -run "TestHealth|TestWowStatus" -count=1
```

Expected: FAIL because health package does not exist.

- [ ] **Step 2: Implement health snapshot types**

Create `internal/server/health/health.go` with concrete structs for `Snapshot`, `Liveness`, `Readiness`, `Matrix`, `Memory`, `Storage`, and `ContextStatus`.

- [ ] **Step 3: Implement readiness calculation**

Readiness is true only when every supported strict or default-required target is ready. `no_build` targets do not block unless strict.

- [ ] **Step 4: Implement MCP status wrapper**

Implement `wow_status` handler in `internal/server/mcphttp/status.go` that returns the exact `health.Snapshot` from the same provider used by `/health`.

- [ ] **Step 5: Verify health tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/health ./internal/server/mcphttp -run "TestHealth|TestWowStatus" -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add internal/server/health internal/server/mcphttp
git commit -m "add shared server health status"
```

Expected: commit succeeds.

## Task 6: Server Query Store Using DuckDB and Parquet

**Files:**

- Create: `internal/server/storage/duckdb/query.go`
- Create: `internal/server/storage/duckdb/query_test.go`
- Create: `internal/server/storage/parquet/materializer.go`
- Create: `internal/server/storage/parquet/materializer_test.go`

- [ ] **Step 1: Write failing DuckDB query builder tests**

Create tests for `BuildRowsSQL`, `BuildSearchSQL`, `BuildForeignKeySQL`, `BuildStreamSQL`, and `BuildSchemaSQL`. Each test asserts:

- quoted identifiers
- parameter markers
- no interpolated user values
- invalid identifiers rejected
- Parquet path traversal rejected

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/duckdb -count=1
```

Expected: FAIL because package does not exist.

- [ ] **Step 2: Implement query builder**

Implement a `QueryBuilder` that builds SQL only from validated identifiers and a trusted Parquet path from metadata.

- [ ] **Step 3: Write failing materializer reuse tests**

Create tests proving:

- valid metadata and valid Parquet skips decode
- invalid footer marks stale
- writer uses temp file and rename
- decoded rows are released after write

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/parquet -count=1
```

Expected: FAIL before implementation.

- [ ] **Step 4: Implement materializer**

Implement a materializer that accepts a table decoder interface, writes Parquet atomically, validates metadata, writes SQLite state, then drops row buffers.

- [ ] **Step 5: Verify query and materializer tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/duckdb ./internal/server/storage/parquet -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add internal/server/storage/duckdb internal/server/storage/parquet
git commit -m "add server parquet query storage"
```

Expected: commit succeeds.

## Task 7: SQLite Listfile Index

**Files:**

- Create: `internal/server/storage/listfile/index.go`
- Create: `internal/server/storage/listfile/index_test.go`

- [ ] **Step 1: Write failing listfile index tests**

Create `internal/server/storage/listfile/index_test.go` with these exact test names:

```go
func TestIndexLookupByFileDataID(t *testing.T)
func TestIndexLookupByFilename(t *testing.T)
func TestIndexPathSearchHonorsLimit(t *testing.T)
func TestIndexExtensionSearchHonorsLimit(t *testing.T)
func TestIndexSourceHashChangeReplacesRows(t *testing.T)
```

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/listfile -count=1
```

Expected: FAIL because package `internal/server/storage/listfile` or the listed functions do not exist.

- [ ] **Step 2: Implement listfile repository**

Implement batch upsert and query methods over SQLite. Normalize paths to lowercase forward-slash paths and store extension without leading dot.

The repository API must include:

```go
type Entry struct {
	FileDataID uint32
	Path       string
	Extension  string
}

func ReplaceSource(ctx context.Context, db *sql.DB, sourceHash string, entries []Entry) error
func LookupByFileDataID(ctx context.Context, db *sql.DB, id uint32) (Entry, error)
func LookupByFilename(ctx context.Context, db *sql.DB, path string) (Entry, error)
func SearchPath(ctx context.Context, db *sql.DB, contains string, limit int) ([]Entry, error)
func SearchExtension(ctx context.Context, db *sql.DB, ext string, limit int) ([]Entry, error)
```

- [ ] **Step 3: Verify listfile tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/listfile -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit**

Run:

```powershell
git add internal/server/storage/listfile
git commit -m "add server listfile index"
```

Expected: commit succeeds.

## Task 8: Persisted CASC Index and Raw Cache

**Files:**

- Create: `internal/server/storage/cascindex/index.go`
- Create: `internal/server/storage/cascindex/index_test.go`
- Create: `internal/server/storage/rawcache/cache.go`
- Create: `internal/server/storage/rawcache/cache_test.go`

- [ ] **Step 1: Write failing CASC index tests**

Create `internal/server/storage/cascindex/index_test.go` with these exact test names:

```go
func TestResolveFileDataIDToArchiveSpan(t *testing.T)
func TestSourceVersionChangeMarksIndexStale(t *testing.T)
func TestMissingFileDataIDReturnsNotFound(t *testing.T)
```

`TestResolveFileDataIDToArchiveSpan` must prove this chain:

```text
fileDataID -> content key -> encoding key -> archive key + offset + size
```

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/cascindex -count=1
```

Expected: FAIL because package `internal/server/storage/cascindex` or the resolver does not exist.

- [ ] **Step 2: Implement CASC index repository**

Implement SQLite-backed root, encoding, and archive lookup methods with source version fields.

- [ ] **Step 3: Write failing raw cache tests**

Create `internal/server/storage/rawcache/cache_test.go` with these exact test names:

```go
func TestValidBlobHitAvoidsRemoteFetch(t *testing.T)
func TestCorruptBlobRefetches(t *testing.T)
func TestSameEncodingKeySingleflights(t *testing.T)
func TestDifferentEncodingKeysRunConcurrently(t *testing.T)
```

The fake remote fetcher must expose an atomic call count. `TestSameEncodingKeySingleflights` must start at least 16 goroutines for the same key and assert the call count is 1. `TestDifferentEncodingKeysRunConcurrently` must block two different keys on a channel and assert both fetches started before either completes.

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/rawcache -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement raw cache**

Implement integrity-checked disk cache and per-key singleflight. Do not use a global runtime lock.

- [ ] **Step 5: Verify CASC/raw cache tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/cascindex ./internal/server/storage/rawcache -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add internal/server/storage/cascindex internal/server/storage/rawcache
git commit -m "add server casc index raw cache"
```

Expected: commit succeeds.

## Task 9: Artifact Store and `/files/...`

**Files:**

- Create: `internal/server/storage/artifacts/store.go`
- Create: `internal/server/storage/artifacts/store_test.go`
- Modify: `cmd/wowdata-server/main.go`

- [ ] **Step 1: Write failing artifact tests**

Create `internal/server/storage/artifacts/store_test.go` with these exact test names:

```go
func TestReservePathStaysUnderArtifactRoot(t *testing.T)
func TestStoreResponseContainsDownloadURL(t *testing.T)
func TestStaticFileHandlerServesBytes(t *testing.T)
func TestTraversalPathsAreRejected(t *testing.T)
```

`TestTraversalPathsAreRejected` must try all of these paths and assert HTTP 403 or 404:

```text
../secret.txt
..%2fsecret.txt
subdir/../../secret.txt
subdir\..\secret.txt
```

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/artifacts -count=1
```

Expected: FAIL.

- [ ] **Step 2: Implement artifact store**

Implement safe path reservation, SHA256 calculation, metadata insert, and URL generation from configured base URL.

- [ ] **Step 3: Implement `/files/` handler in server entrypoint**

Use `http.ServeFile` or `http.FileServer` with path-cleaning that rejects `..` and backslash traversal.

- [ ] **Step 4: Verify artifact tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/storage/artifacts -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```powershell
git add internal/server/storage/artifacts cmd/wowdata-server/main.go
git commit -m "serve server artifacts"
```

Expected: commit succeeds.

## Task 10: Prepare and Refresh State Machines

**Files:**

- Create: `internal/server/prepare/scheduler.go`
- Create: `internal/server/prepare/scheduler_test.go`
- Create: `internal/server/refresh/refresh.go`
- Create: `internal/server/refresh/refresh_test.go`

- [ ] **Step 1: Write failing prepare scheduler tests**

Create `internal/server/prepare/scheduler_test.go` with these exact test names:

```go
func TestBackgroundPrepareStartsAfterListenerReady(t *testing.T)
func TestMaxParallelContextPreparesIsEnforced(t *testing.T)
func TestMaxParallelTableMaterializationsIsEnforced(t *testing.T)
func TestUserQueryReturnsContextNotReadyWithoutTriggeringPrepare(t *testing.T)
```

The concurrency tests must use atomic counters for current and max observed parallelism. They must fail if max observed parallelism exceeds the configured limit.

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/prepare -count=1
```

Expected: FAIL.

- [ ] **Step 2: Implement prepare scheduler**

Implement target queue, per-target state updates, semaphores, and progress reporting.

- [ ] **Step 3: Write failing refresh tests**

Create `internal/server/refresh/refresh_test.go` with these exact test names:

```go
func TestRefreshNoNewBuildLeavesActiveBuildUnchanged(t *testing.T)
func TestRefreshCandidateSuccessActivatesNewBuild(t *testing.T)
func TestRefreshCandidateFailurePreservesOldActiveBuild(t *testing.T)
func TestRefreshListfileSourceHashChangeMarksListfileStale(t *testing.T)
func TestRefreshCASCIndexVersionChangeMarksCASCStale(t *testing.T)
func TestRefreshDB2FingerprintChangeMarksOnlyAffectedTableStale(t *testing.T)
func TestRefreshUnaffectedTableRemainsValid(t *testing.T)
```

The tests must use fixture discoverers/materializers. No test in this package may download real Blizzard data.

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/refresh -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement refresh workflow**

Implement candidate prepare and atomic activation using metadata repository transactions. Failed candidates must preserve old active builds.

- [ ] **Step 5: Verify prepare and refresh tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/prepare ./internal/server/refresh -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add internal/server/prepare internal/server/refresh
git commit -m "add server prepare refresh workflows"
```

Expected: commit succeeds.

## Task 11: HTTP MCP Tools on Server Services

**Files:**

- Create: `internal/server/mcphttp/tools.go`
- Create: `internal/server/mcphttp/tools_test.go`
- Create: `internal/server/service/query.go`
- Create: `internal/server/service/query_test.go`
- Modify: `cmd/wowdata-server/main.go`

- [ ] **Step 1: Write failing MCP tool list tests**

Tests must assert the default HTTP tools include:

```text
wow_status
wow_builds
wow_query
wow_item
wow_spell
wow_file
wow_icon
wow_creature
wow_encounter
wow_decor
wow_video
```

and do not include:

```text
wow_warmup
wow_db2
```

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/mcphttp -run TestHTTPToolList -count=1
```

Expected: FAIL.

- [ ] **Step 2: Write failing `wow_query` mode tests**

Test `schema`, `rows`, `search`, `foreign-key`, and `stream` using a fake query service. Assert each mode calls the service method and returns `query <mode>` command names.

- [ ] **Step 3: Implement server MCP tools**

Implement tools against server service interfaces. Do not import `internal/app` or call Cobra handlers.

- [ ] **Step 4: Wire MCP HTTP endpoint in `wowdata-server`**

Expose `/mcp`, `/health`, and `/files/`.

- [ ] **Step 5: Verify MCP tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/mcphttp ./internal/server/service -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add internal/server/mcphttp internal/server/service cmd/wowdata-server/main.go
git commit -m "add server mcp tools"
```

Expected: commit succeeds.

## Task 12: Bounded Business Assemblers

**Files:**

- Create: `internal/server/service/business_spell.go`
- Create: `internal/server/service/business_spell_test.go`
- Create: `internal/server/service/business_item.go`
- Create: `internal/server/service/business_item_test.go`
- Create: `internal/server/service/business_creature.go`
- Create: `internal/server/service/business_creature_test.go`
- Create: `internal/server/service/business_encounter.go`
- Create: `internal/server/service/business_encounter_test.go`
- Create: `internal/server/service/business_decor.go`
- Create: `internal/server/service/business_decor_test.go`

- [ ] **Step 1: Write failing spell bounded-query test**

Test `SpellInfo` with a fake row source that fails if called with an unbounded full-table query for `SpellEffect`.

Expected query pattern:

```text
SpellEffect WHERE SpellID IN (...)
Spell WHERE ID IN (...)
SpellName WHERE ID IN (...)
SpellMisc WHERE SpellID IN (...)
SpellCastTimes WHERE ID IN (...)
SpellDuration WHERE ID IN (...)
SpellRange WHERE ID IN (...)
```

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/service -run TestSpellAssemblerUsesBoundedQueries -count=1
```

Expected: FAIL.

- [ ] **Step 2: Implement bounded spell assembler**

Implement frontier traversal with `maxDepth` and batch ID queries. Preserve response semantics from `internal/wowdata.SpellService`.

- [ ] **Step 3: Write failing bounded tests for item, creature, encounter, and decor**

Each test uses a fake row source that fails on unbounded full-table reads and asserts only necessary tables are queried.

- [ ] **Step 4: Implement remaining bounded assemblers**

Implement item, creature, encounter, and decor assemblers against the same query service interface.

- [ ] **Step 5: Verify business assembler tests**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./internal/server/service -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```powershell
git add internal/server/service
git commit -m "add bounded server business assemblers"
```

Expected: commit succeeds.

## Task 13: Remote Verification Harness

**Files:**

- Create: `.local/wowdata/test-http-node-parity.ps1` untracked
- Create: `.local/wowdata/test-http-update-flow.ps1` untracked
- Modify: `.gitignore` only if `.local/` is not already ignored
- Create: `docs/deployment.md` updates

- [ ] **Step 1: Write Node parity full-data script**

Create `.local/wowdata/test-http-node-parity.ps1`. It must:

- call `/health`
- call MCP `wow_status`
- derive the complete 23-target matrix from server status and fail if any configured target is missing
- require all 23 configured targets to be server-ready and Node-resolvable; a target reported as no-build, missing, stale, failed, or preparing is a parity failure unless the script is explicitly run with a diagnostic `-AllowNotReady` flag
- call the legacy Node implementation for each target and discover that target's full Node-readable DB2 table list from Node output
- require the legacy Node oracle to print its resolved build key for every target, and fail before comparison if that build key differs from the server target's active build key
- call the new Go HTTP MCP implementation for each target
- use the legacy Node implementation as the oracle table list for each target; the script must not ask Go for the table list, read Go materialized cache files, or use a static checked-in table list
- fail if any target produces zero Node-readable tables
- fail if the Go HTTP result is missing any Node-readable table
- report and fail on Go-only extra tables until a later spec revision explicitly accepts them
- compare every Node-readable table
- compare every field in the schema
- compare every row and every field value through a deterministic canonical representation
- include every Node-emitted field of every row in the canonical hash input; sampled rows, first-page-only hashes, or table-level smoke checks do not satisfy this task
- stream canonical hashes so the script does not need to hold a full large table in memory
- print one line per target/table with row count, field count, Node schema hash, Go schema hash, Node data hash, Go data hash, and pass/fail
- print one target summary line containing target label, Node-readable table count, compared table count, missing table count, extra table count, and failed table count
- print `REMOTE_NODE_PARITY_PASS=x/y`
- exit 1 unless `x == y`

On any mismatch, the script must also print:

```text
first_mismatch_target=<label>
first_mismatch_table=<table>
first_mismatch_kind=<schema|row_count|field_value|missing_table|extra_table>
```

and at least 20 concrete row/field diffs when 20 are available. A hash-only mismatch report is incomplete and must fail review.

The denominator `y` must be computed at runtime as the sum of every Node-readable DB2 table across all 23 configured targets. The script must not contain `93`, `92`, or any other historical fixed denominator except inside comments explaining that those values are obsolete. A run that only proves a fixed smoke set, a bounded business-query set, compares fewer than 23 targets, or uses the old matrix denominator is a failure even if it prints `x == y`.

- [ ] **Step 2: Write update-flow script**

Create `.local/wowdata/test-http-update-flow.ps1`. It must call the server fixture/test mode endpoints or commands created by Tasks 10 and 14 to exercise these named checks:

```text
UpdateNoNewBuild
UpdateCandidateSuccess
UpdateCandidateFailure
UpdateListfileSourceHashChange
UpdateCASCIndexVersionChange
UpdateDB2FingerprintChange
UpdateUnaffectedTableStillValid
```

It must print `REMOTE_UPDATE_PASS=x/y` and exit 1 unless `x == y`.

- [ ] **Step 3: Confirm scripts are untracked**

Run:

```powershell
git status --short --ignored .local/wowdata
```

Expected: scripts appear under ignored files, not staged tracked files.

- [ ] **Step 4: Run local full tests before remote**

Run:

```powershell
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./... -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit docs or ignore changes only**

Run:

```powershell
git add .gitignore docs/deployment.md
git commit -m "document remote server verification"
```

Expected: commit succeeds if tracked docs changed. If no tracked files changed, record "no commit needed" in the task handoff.

## Task 14: Remote Docker Deploy and Final Acceptance

**Files:**

- Modify: `Dockerfile.http`
- Modify: `docs/deployment.md`
- Modify: `docs/mcp-tools.md`
- Modify: `docs/performance.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Build and deploy to remote Docker**

Run the project deployment script or updated equivalent:

```powershell
.\.local\wowdata\deploy-http-docker.ps1 -SkipTests
```

Expected:

```text
Remote deploy complete.
/health returns ok process liveness JSON.
```

- [ ] **Step 2: Run remote Node parity full-data test**

Run:

```powershell
.\.local\wowdata\test-http-node-parity.ps1 -BaseUrl 'http://211.154.18.253:11223'
```

Expected:

```text
REMOTE_NODE_PARITY_PASS=x/y
x equals y
```

The script must fail if fewer than the 23 configured targets are compared, unless it is explicitly running in diagnostic `-AllowNotReady` mode. It must fail if any target is no-build/missing/stale/failed/preparing, if the legacy Node oracle cannot resolve that target's build, if any Node-readable table is missing, if any Go-only table appears, if any schema field differs, if any field order differs, if any row count differs, or if any canonical full-data hash differs from the legacy Node oracle.

The denominator in `REMOTE_NODE_PARITY_PASS=x/y` must be the runtime full-table denominator discovered from the legacy Node implementation across all 23 targets. A result shaped like the old smoke test, such as `REMOTE_NODE_PARITY_PASS=93/93`, is not acceptable unless the Node oracle genuinely discovered exactly 93 total DB2 tables across all 23 targets and the log shows each target/table discovery line proving that number. The log must also show, for every target, that the Node oracle build key equals the server active build key; otherwise the comparison is invalid even if hashes match.

- [ ] **Step 3: Run remote update-flow test**

Run:

```powershell
.\.local\wowdata\test-http-update-flow.ps1 -BaseUrl 'http://211.154.18.253:11223'
```

Expected:

```text
REMOTE_UPDATE_PASS=x/y
x equals y
```

- [ ] **Step 4: Run remote restart/reuse test**

Run:

```powershell
ssh -i .local\wowdata\config\cert\Follen.pem -p 10042 -o StrictHostKeyChecking=no root@211.154.18.253 'docker restart wowdata-mcp >/dev/null && sleep 15 && curl -fsS http://127.0.0.1:9443/health'
.\.local\wowdata\test-http-node-parity.ps1 -BaseUrl 'http://211.154.18.253:11223' -ReuseOnly
```

Expected:

```text
health returns liveness ok
reuse test reports no full rematerialization
idle RSS is below configured memory budget
```

- [ ] **Step 5: Run final local verification**

Run:

```powershell
git status --short
& 'C:\Users\follen\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.1.windows-amd64\bin\go.exe' test ./... -count=1
```

Expected:

```text
Only intentional tracked docs/source changes appear before commit
go test ./... exits 0
```

- [ ] **Step 6: Commit docs and final adjustments**

Run:

```powershell
git add Dockerfile.http config README.md CHANGELOG.md docs cmd internal migrations
git commit -m "complete optimal http mcp server"
```

Expected: commit succeeds.

## Self-Review Checklist

- [x] Public naming is covered by Task 1.
- [x] local/server dependency isolation is covered by Task 2.
- [x] 23-target default matrix is covered by Task 3.
- [x] SQLite metadata, listfile, CASC index, artifacts, and refresh state are covered by Tasks 4, 7, 8, 9, and 10.
- [x] Health and `wow_status` shared state is covered by Task 5.
- [x] Parquet/DuckDB query path is covered by Task 6.
- [x] User requests not triggering prepare is covered by Task 10 and Task 11.
- [x] Bounded business assemblers are covered by Task 12.
- [x] Remote Node-parity full-data comparison, artifact download, restart reuse, and update-flow tests are covered by Tasks 13 and 14.
- [x] Checkbox discipline is defined in "Progress Discipline".
- [x] No source implementation is part of this plan-writing task.
