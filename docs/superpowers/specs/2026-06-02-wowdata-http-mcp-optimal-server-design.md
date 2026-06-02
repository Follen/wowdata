# wowdata HTTP MCP Optimal Server Design

Date: 2026-06-02

## Decision

Rewrite the HTTP MCP data layer as a dedicated server product line.

This is a full HTTP data-layer rewrite, not a full repository rewrite. The CLI and stdio MCP local flows keep their existing local runtime model. Shared DB2, DBD, CASC, BLP, export, artifact, and domain business semantics are reused where correct.

## Goals

- Serve HTTP MCP as a remote, multi-user, long-running service.
- Keep large data out of long-lived Go heap memory.
- Prepare configured builds in the background before normal user queries depend on them.
- Store DB2 data in Parquet and query it through DuckDB.
- Store build state, prepare state, DB2 metadata, listfile index, CASC index, artifact metadata, and refresh state in SQLite.
- Store real CASC blobs and exported artifacts on disk.
- Allow concurrent reads across builds, products, tables, files, and artifacts.
- Make remote data refresh atomic: a new build becomes active only after full prepare and validation succeeds.
- Keep CLI and stdio MCP behavior stable and isolated from HTTP server storage and runtime changes.

## Non-Goals

- Do not rewrite the CLI command behavior.
- Do not rewrite stdio MCP into the server storage model.
- Do not duplicate DB2, DBD, CASC, BLP, export, or domain business code when a shared implementation remains correct.
- Do not let user HTTP requests perform large prepare or full-table materialization work.
- Do not keep decoded DB2 rows, full listfile contents, full CASC indexes, or file blobs in the context pool.

## Product Targets

The repository will build two product targets:

```text
cmd/wowdata
  Local CLI and stdio MCP target.

cmd/wowdata-server
  Remote HTTP MCP server target.
```

The server Docker image uses `wowdata-server`. The local release keeps `wowdata`.

## Public Naming

The public query surface is named `query`, not `db2`.

Required external names:

```text
CLI:
  wowdata query schema
  wowdata query rows
  wowdata query search
  wowdata query foreign-key
  wowdata query stream

MCP:
  wow_query
```

The rename applies to CLI, stdio MCP, HTTP MCP, help text, README examples, release docs, performance docs, and tests. No legacy `wowdata db2 ...` command or `wow_db2` MCP tool is kept for compatibility.

Internal packages, storage paths, and type names may still use `DB2` where they refer to the actual WoW DB2 file/table format, such as DB2 decoder code, DB2 metadata, and DB2 Parquet cache paths.

## File Architecture

The codebase will be separated by runtime scenario:

```text
cmd/
  wowdata/
    main.go
  wowdata-server/
    main.go

internal/
  shared/
    db2/
    dbd/
    casc/
    blte/
    blp/
    export/
    artifact/
    wowdata/
    mcpserver/

  local/
    runtime/
    cli/
    mcpstdio/
    diagnostics/
    cache/

  server/
    runtime/
    mcphttp/
    service/
    storage/
      metadata/
      listfile/
      cascindex/
      parquet/
      duckdb/
      rawcache/
      artifacts/
    prepare/
    refresh/
    prune/
    health/
    config/
```

Hard dependency rules:

- `internal/server/...` must not import `internal/local/...`.
- `internal/local/...` must not import `internal/server/...`.
- `cmd/wowdata-server` must not depend on Cobra CLI handlers.
- `cmd/wowdata` must not require server-only DuckDB storage to run local CLI or stdio MCP.
- Dependency checks must be automated with tests.

## Default Server Build Matrix

The server discovers and prepares the latest configured builds for this default matrix:

```text
Retail:
  CN / US / EU / KR / TW

PTR:
  CN / US / EU

Classic:
  CN / US / EU / KR / TW

Classic Era:
  CN / US / EU / KR / TW

Classic Titan:
  CN
```

Total default prepare targets: 19.

Beta (`wowxptr`) targets are intentionally excluded from default prepare. They may be handled only by an explicit custom configuration or later spec revision; the default server must not discover, prepare, count, or block readiness on Beta targets.

Default locale mapping:

```text
CN -> zhCN
US -> enUS
EU -> enUS
KR -> koKR
TW -> zhTW
```

The default matrix must contain only region/product pairs that the current Blizzard product discovery and the legacy Node oracle can resolve. Unsupported pairs are rejected from the default matrix instead of being counted as `no_build` skips.

## Request Classes

HTTP MCP requests are classified before touching storage:

```text
DB2/business request
  -> context/build resolution
  -> SQLite metadata
  -> DuckDB read_parquet
  -> bounded business assembler
  -> JSON response

File/artifact request
  -> optional SQLite listfile lookup
  -> SQLite CASC index lookup
  -> local raw cache read or bounded remote range fetch
  -> optional artifact write
  -> JSON response with /files URL
```

User requests must not trigger full build prepare, full table materialization, full listfile indexing, or full CASC index construction. If a requested target is not ready, return `context_not_ready`, `build_not_ready`, or `table_not_ready`.

## Context Pool

The server context pool is a build-level coordination layer. It stores small handles and status, not large data.

Allowed in context pool:

- region, product, locale, build key, build name
- active, preparing, stale, failed, ready state
- handles to SQLite, DuckDB, raw cache, artifact store, and CDN selection
- bounded per-request or short-lived helper objects
- locks, semaphores, and singleflight groups

Forbidden in context pool:

- decoded DB2 rows
- full listfile
- full CASC root, encoding, or archive index
- file blobs
- artifact bytes

## DB2 Storage and Query Path

DB2 tables are materialized during prepare:

```text
CASC DB2 file
  -> decode
  -> Parquet table file
  -> SQLite materialized_tables row with valid state
  -> DuckDB read_parquet query path
```

SQLite stores:

- region
- product
- locale
- build key
- table name
- DB2 FileDataID
- DBD hash
- decoder version
- materializer version
- parquet path
- row count
- state: preparing, valid, stale, failed
- error text when failed

The HTTP `wow_query` tool must support:

- schema
- rows
- search
- foreign-key
- stream

All HTTP `wow_query` modes query Parquet through DuckDB after verifying SQLite metadata. They must not fall back to CLI handlers or local `MemoryDB2Store`.

## Business Assemblers

Domain semantics are reused, but HTTP assemblers must be bounded and query-driven.

For `wow_spell`, the HTTP path must not load full `SpellEffect` into memory. It must:

- start from seed spell IDs
- batch query `SpellEffect` for the current frontier
- follow `EffectTriggerSpell` and relevant `EffectMiscValue` references up to `maxDepth`
- parse spell references from `Spell.Description_lang` and `Spell.AuraDescription_lang`
- batch query only the needed rows from `Spell`, `SpellName`, `SpellMisc`, `SpellCastTimes`, `SpellDuration`, and `SpellRange`
- return the same response semantics as the current spell business API

The same rule applies to item, creature, encounter, and decor tools: HTTP may reuse business semantics, but not unbounded full-table row loading.

## Listfile Storage

Listfile is indexed into SQLite, with FTS if available.

Required fields:

- source hash
- file data ID
- filename
- extension
- normalized path
- build key or source version
- indexed_at

Required query support:

- fileDataID to filename
- filename to fileDataID
- path search
- extension search
- bounded limit

If a request already provides FileDataID, raw CASC lookup must not require listfile.

## CASC Index and Raw Cache

CASC is split into persisted index and raw cache.

SQLite CASC index stores:

- build config identity
- CDN config identity
- root mappings needed for FileDataID to content key
- encoding mappings needed for content key to encoding key
- archive index mappings needed for encoding key to archive, offset, and size
- state and fetched_at

Raw cache stores real bytes on disk:

```text
cache/casc/<region>/<product>/<buildKey>/data/<encodingKey>
```

Raw cache integrity must be verified before reuse. Remote fetch happens only when local cache is missing, corrupt, pruned, or the build changed.

Different CASC blobs may download concurrently. The same blob must use singleflight to avoid duplicate downloads. Large range downloads may use bounded chunk concurrency.

## Artifact Store

Exported files are written under the server artifact root and exposed through `/files/...`.

Artifact responses must include:

- local artifact path
- public download URL
- MIME type
- size
- SHA256 when available

`/files/...` must be served by `wowdata-server` directly in the pure HTTP/IP Docker deployment. It must not depend on nginx or a Python gateway.

## Prepare and Refresh Flow

Prepare is a background server workflow, not a user request side effect.

Startup:

```text
start HTTP listener
discover configured target matrix
start background prepare workers
serve /health and MCP status immediately
MCP requests to not-ready targets return not_ready errors
```

Refresh:

```text
discover latest product builds
insert or update candidate build rows
prepare CASC index
materialize required DB2 tables
index listfile
validate Parquet, schema, hashes, and versions
mark candidate ready
atomically activate candidate
mark old build stale according to retention policy
```

Failure:

```text
candidate state = failed
active build remains unchanged
old valid data remains queryable
health/status exposes failure details
```

No request may observe mixed old/new build data for one logical build target.

## Health and MCP Status

There is one health model shared by:

```text
GET /health
MCP tool wow_status
```

`/health` returns HTTP 200 when the process is alive and can report status. Readiness is represented inside the JSON body. HTTP 500 is reserved for cases where the process cannot inspect core server state, such as metadata DB open failure.

Health includes:

- liveness
- readiness
- default matrix summary
- per-target state
- prepare progress
- active build
- DB2 table readiness
- listfile index readiness
- CASC index readiness
- memory stats
- storage usage
- artifact settings
- concurrency limits
- recent refresh errors

`readiness.ok` is true when all required supported targets are ready. Custom-config `no_build` targets do not block readiness unless strict; the default 19-target matrix must not include `no_build` entries.

## Concurrency Model

Normal requests are concurrent by default.

The server must not globally lock all users while one user queries or downloads a different build or blob. These requests must be able to run concurrently:

```text
User A: CN Retail
User B: CN Classic
User C: CN Classic Titan
User D: CN PTR
```

Locks and singleflight are resource-scoped:

- per target prepare
- per build/table materialization
- per CASC encoding key raw-cache write
- per artifact destination path
- active build transaction

Different builds, different tables, different blobs, and different artifact paths may proceed concurrently subject to resource limits.

Default resource limits for a 10 GB server:

```text
max_parallel_context_prepares = 2
max_parallel_table_materializations = 2
max_parallel_downloads = 16
max_parallel_queries = 16
memory_soft_limit_mb = 4096
memory_hard_limit_mb = 8192
```

These are configurable server settings.

## Memory Rules

The HTTP server must not retain decoded DB2 rows after materialization. Prepare workers must release per-table buffers after writing and validating Parquet.

HTTP business queries must not call unbounded full-table row loads for large tables. Listfile and CASC index queries must be bounded and persisted. Raw file bytes are streamed or read from disk for the current request only.

After the full default matrix is prepared and the server is idle, RSS must stay below the configured memory budget.

## MCP Tools

HTTP MCP default tools:

- `wow_status`
- `wow_builds`
- `wow_query`
- `wow_item`
- `wow_spell`
- `wow_file`
- `wow_icon`
- `wow_creature`
- `wow_encounter`
- `wow_decor`
- `wow_video` when implemented or truthfully unavailable

Admin/debug tools are explicit config opt-ins.

HTTP MCP must not expose `wow_warmup` as a user-triggered normal tool. Prepare belongs to server background/admin flow.

## Required Tests

All implementation must be test-driven. Each feature or behavior change requires a failing test first, then implementation, then full verification.

### Unit Tests

- Dependency isolation: server does not import local and local does not import server.
- Server config validates default matrix, locale mapping, strict/no_build behavior, and resource limits.
- Health model reports liveness, readiness, matrix summary, per-target state, memory, storage, artifacts, and errors.
- `wow_status` and `/health` use the same health source.
- DB2 metadata records transition through preparing, valid, stale, and failed.
- DB2 materializer reuses valid Parquet without decoding or downloading again.
- DB2 materializer marks stale on invalid Parquet metadata.
- DuckDB query builder covers schema, rows, search, foreign-key, and stream.
- Business assemblers perform bounded queries and do not issue unbounded full-table loads for large tables.
- Listfile SQLite index supports ID lookup, filename lookup, path search, extension search, limits, and source hash updates.
- CASC index lookup resolves FileDataID to content key, encoding key, archive offset, and size.
- Raw cache validates integrity and avoids remote fetch on valid local hit.
- Artifact store returns downloadable `/files/...` URLs and serves bytes directly.
- Refresh candidate success atomically activates a new build.
- Refresh candidate failure preserves the old active build.
- Same-resource singleflight deduplicates work.
- Different-build and different-blob operations can run concurrently.

### Local Full Test

Run:

```text
go test ./... -count=1
```

The local test suite must include both local CLI/stdio compatibility tests and server HTTP tests.

### Remote Docker Build Test

Build the server image on the remote Docker host. The image must contain `wowdata-server`, server migrations, default server config, and no Python gateway.

### Remote Node-Parity Full Data Test

Against the remote HTTP endpoint, run a generated parity test against the legacy Node implementation for all 19 default configured build targets. This replaces the historical fixed-denominator matrix check and is the authoritative business-equivalence gate for DB2 data.

Required coverage:

- all 19 default configured build targets; a target that cannot be resolved by either the server or the legacy Node oracle is a parity failure, not a skipped pass
- the exact active build key for each target; the Node oracle must report its resolved build key and the test must fail if it differs from the server target build key
- every DB2 table that the legacy Node implementation can read for each target build; this is the full table set discovered from the Node oracle, not the old sampled smoke set and not a handpicked list
- every row in every compared table
- every field in every compared row
- schema equality: table list, field names, field order, field cardinality, and field value normalization
- row equality: row count and deterministic row identity/order
- value equality: canonical JSON value for every scalar, array, localized string, null, and numeric field

The legacy Node implementation is the oracle for this acceptance test. The table list must be discovered by running the old Node implementation for the same region/product/locale/build target, not by asking the new Go code, not by reading the Go materialized cache, and not by reusing a static checked-in list. Before comparing tables, the harness must verify `node.build.buildKey == serverTarget.buildKey`; if the legacy Node implementation resolves a different build, or cannot resolve a build for one of the 19 default configured targets, that target fails before any table hash is accepted. A Go HTTP result is a failure if it is missing any Node-readable table, missing any Node-emitted field, has a different field order, has a different row count, or has a different canonical value. Go-only extra tables must be reported as extras and fail the parity test until deliberately reviewed in a later spec revision.

The test must not hard-code `93/93`, `92/92`, or any other stale denominator. It must compute the total from the 19 default configured targets and the full table list discovered from the legacy Node implementation for each target. The only acceptable final denominator is the runtime sum of all Node-readable DB2 tables across the 19 default targets. If a target has zero Node-readable tables, that target must fail with a diagnostic explaining why the Node oracle produced no table list. A table-level pass is only valid when every row and every field emitted by Node has been included in the canonical hash input.

The comparison may use streaming canonical hashes to avoid loading all rows into memory, but the hash input must include every field of every row. Hashing is only a fast equality proof; on mismatch, the harness must perform or retain enough row-level comparison state to print concrete diffs. For each target/table it must report:

- target label, region, product, locale, and build
- server build key and legacy Node build key
- table name
- compared row count
- compared field count
- legacy Node schema hash
- new Go HTTP schema hash
- legacy Node full-data hash
- new Go HTTP full-data hash
- pass/fail

On mismatch, the test must emit the first mismatched table and at least the first 20 concrete row/field diffs. A hash mismatch without concrete diagnostic diffs is not an acceptable failure report.

### Remote Business Tool Test

Run remote MCP calls for:

- `wow_item`
- `wow_spell`
- `wow_creature`
- `wow_encounter`
- `wow_decor`

These tests must verify successful responses and must check that server logs/status do not show user-request-triggered prepare for already ready data.

### Remote Artifact Test

Run remote MCP calls for:

- `wow_file lookup`
- `wow_file exists`
- `wow_file get` or `wow_file export`
- `wow_icon export`

For each returned `downloadUrl`, perform an HTTP GET and verify:

- status 200
- nonzero byte length
- expected content type when known
- artifact path remains under artifact root

### Remote Reuse and Restart Test

After the default matrix is ready:

1. Record materialized table count, active build rows, and Parquet mtimes.
2. Restart `wowdata-server`.
3. Verify `/health` and `wow_status`.
4. Verify no full rematerialization starts for valid data.
5. Verify DB2 table count stays the same.
6. Verify idle RSS returns below memory budget.

### Remote Update Flow Test

The test suite must cover remote data update behavior without relying on Blizzard releasing a live build during the test.

Use a controlled test mode or fixture-backed remote source that simulates:

1. no new build
2. new candidate build discovered
3. candidate prepare success
4. candidate prepare failure
5. listfile source hash changed
6. CASC index changed
7. DB2 table hash or decoder version changed

Required assertions:

- no-new-build keeps active unchanged
- successful candidate activates atomically
- failed candidate does not replace old active
- changed listfile source reindexes SQLite listfile records
- changed CASC index updates SQLite CASC index state
- changed DB2 fingerprint rematerializes affected Parquet tables
- unaffected tables remain valid and are not rematerialized
- `/health` and `wow_status` expose refresh state and errors

### Performance and Memory Test

After preparing the configured matrix:

- concurrent read queries across Retail, PTR, Classic, Classic Era, and Titan must run without global runtime serialization
- different raw-cache blobs must download concurrently
- same raw-cache blob must singleflight
- idle RSS must stay under `memory_soft_limit_mb` or the test must fail with diagnostics
- no HTTP business query may perform an unbounded full-table load for large tables

## Acceptance Criteria

The rewrite is complete only when all of these are true:

- `cmd/wowdata` and `cmd/wowdata-server` both build.
- CLI and stdio MCP tests still pass.
- Server dependency isolation tests pass.
- Server unit tests pass.
- Remote Docker deployment succeeds.
- `/health` and `wow_status` report the same health state.
- The remote Node-parity full data test reports full pass across the prepared 19-build default matrix, every comparable table, every row, and every field.
- Remote artifact URLs are downloadable.
- Remote restart proves valid DB2/Listfile/CASC data is reused.
- Remote update-flow tests prove successful candidate activation and failed candidate rollback.
- Memory and concurrency tests pass under configured limits.
- No Python gateway or nginx-dependent artifact serving is required for pure HTTP/IP deployment.

## Current Implementation Gaps This Spec Intentionally Replaces

- Current HTTP runtime still shares too much with local runtime.
- Current materialization still stages whole DB2 tables in Go memory before Parquet.
- Current listfile is not a server SQLite/FTS index.
- Current CASC index is not fully persisted as server storage.
- Current context pool is not yet a slim build-control layer.
- Current remote tests are partial and must be replaced with computed coverage.
- Current update-flow coverage is insufficient for candidate build/listfile/CASC/DB2 refresh behavior.
