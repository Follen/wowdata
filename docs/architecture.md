# Runtime Architecture

wowdata has three public runtime surfaces that share data readers and query
services while keeping process policy separate.

Related documents:

- [MCP tools](mcp-tools.md)
- [HTTP service runtime](http-service-runtime.md)
- [Cache layout](cache-layout.md)
- [Docker deployment](deployment.md)
- [Performance verification](performance.md)

## CLI Runtime

The CLI is the direct command runtime. It resolves flags, creates one active
context, runs the requested command, prints JSON or text output, and exits. CLI
commands can use a local WoW client path or remote Blizzard CDN metadata. The
CLI does not load HTTP service config, does not start a context pool, and does
not keep background jobs alive after the command exits.

## Stdio Runtime

`wowdata mcp stdio` exposes the local MCP transport for tools such as Codex,
Claude Code, and cc-switch. The stdio runtime keeps the existing CLI-backed
tool behavior: MCP tool calls map to the same command handlers and the same
single active local runtime. It intentionally exposes `wow_warmup`, because a
local MCP client can explicitly warm the one context it wants to query.

## HTTP Service Runtime

`wowdata mcp http` is the remote service runtime. It loads the HTTP YAML config,
binds `/wowdata`, `/health`, `/help`, and `/files/`, and serves MCP Streamable HTTP
requests. HTTP uses lazy prepare instead of exposing `wow_warmup` by default:
requests resolve a `region/product/locale` context, materialize the required
table or artifact path, and then serve the result from cache.

The HTTP runtime owns service-only policy:

- context pool size and pinned contexts
- materialization singleflight and concurrency limits
- SQLite metadata
- Parquet DB2 cache
- DuckDB query engine
- artifact download URLs
- build watcher and prune policy
- admin tool gate

CLI and stdio stay independent from these service policies.

## Shared Query Services

Shared query packages sit below all three surfaces. DB2, spell, item, creature,
encounter, decor, file, icon, and video logic reuse the same CASC, DBD, DB2,
listfile, and export primitives. The transport layer decides how to expose the
result; the query layer decides how to read WoW data.

## Cache Layers

The runtime uses layered caches:

- raw CASC cache for remote CDN data
- DBD and listfile caches for schema and filename lookup inputs
- SQLite metadata for discovered builds, materialized tables, and cache audit
- Parquet files for materialized DB2 tables
- DuckDB database file for HTTP DB2 query execution
- artifact output root for exported files and icons

Local CLI and stdio caches are process-local by configuration. HTTP cache paths
are configured explicitly so Docker volumes can persist data across container
restarts.

## Failure and Rollback Behavior

Build switching is atomic at the metadata level. The build watcher prepares a
new build first; if prepare fails, the old active build remains active. Prune
planning protects active, pinned, and in-flight builds before recording audit
rows. HTTP container deployment uses a next-container health check before
promoting the new container, so the existing container is replaced only after
the next one reports healthy.
