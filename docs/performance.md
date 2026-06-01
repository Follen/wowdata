# Performance Verification

This document describes how to read private wowdata runtime benchmark results.
Benchmark scripts and result JSON files live under `.local/wowdata/` and are not
committed.

## Benchmark Categories

- HTTP cold `wow_query` query: first remote HTTP DB2 request after a fresh service
  start or empty materialized table cache. This includes build resolution,
  table materialization, DuckDB initialization, and query execution.
- HTTP warm `wow_query` query: same HTTP query after the table has already been
  materialized. This should avoid the expensive prepare path.
- HTTP repeated `wow_query` query: multiple warm HTTP calls in sequence. This is
  the best signal for steady-state MCP latency.
- stdio warm `wow_query` query: local MCP stdio path after local warmup. This
  measures local process overhead without HTTP transport or remote service
  startup costs.
- CLI auto-warmup DB2 query: direct CLI path that may initialize local context
  before serving the command. This is useful as a local baseline, not as a
  remote service target.

## Expected Ordering

Cold HTTP is expected to be the slowest path because it can download manifests,
prepare context, materialize Parquet, open DuckDB, and then query. Warm HTTP
should be much faster once the requested table is present. Repeated warm HTTP
queries should be close to the minimum remote MCP latency for the current
network and server.

Local stdio and CLI numbers are not directly comparable to remote HTTP because
they run on the caller machine and use local cache paths. They are still useful
for detecting regressions in shared DB2 and query code.

## Result Location

Private benchmark output is written to:

```text
.local/wowdata/bench-YYYYMMDD-HHMMSS.json
```

Those files can include hostnames, cache paths, timings, and command output, so
they stay ignored with the rest of `.local/`.

## Remote Smoke Baseline

The remote HTTP deployment must pass a real DB2 smoke before release readiness:

```json
{
  "tool": "wow_query",
  "table": "SpellName",
  "field": "ID",
  "id": 1,
  "limit": 1
}
```

Expected result: `ok: true`, `count >= 1`, and no `query_engine_unavailable`,
`materializer_unavailable`, TLS, or DuckDB path errors.
