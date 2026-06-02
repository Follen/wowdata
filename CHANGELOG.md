# Changelog

## v0.0.1 - 2026-06-01

- Separated the public runtimes into CLI, local MCP stdio, and remote MCP HTTP so service-only policy no longer leaks into local commands.
- Added the HTTP service runtime architecture with lazy context prepare, reusable build contexts, service config isolation, `/mcp`, `/health`, `/help`, and built-in `/files/...` artifact downloads.
- Added layered HTTP caches using raw CASC data, SQLite metadata, Parquet DB2 materialization, and DuckDB query execution.
- Added build watcher and prune policy support for active, pinned, and in-flight build protection.
- Added pure Docker HTTP deployment support for the public IP endpoint and documented the release deployment path.
- Fixed local CASC file reads to decode BLTE payloads consistently with remote CASC reads.
- Fixed DB2 schema output to preserve array field lengths, such as `uint32[3]`.
- Fixed item inventory slot display names so DB2 `InventoryType` values map to their real item slots, including Trinket.
- Fixed fresh DuckDB cache startup by creating the database parent directory before opening the engine.
- Fixed HTTP Docker image TLS access by installing runtime CA certificates.
- Improved item model selection for race, gender, neutral variants, and paired shoulder model resources.
- Kept listfile warmup unfiltered so generated asset lookups remain available after initialization.
- Added same-configuration warmup reuse so repeated explicit `wow_warmup` calls avoid resetting hot in-memory state when the requested build and warmed resources are already covered.
- Added HTTP MCP `--max-contexts` to keep multiple warmed build contexts resident in memory with LRU eviction.
- Added HTTP MCP `wow_query mode=tables` catalog support for parity harness extra-table detection.
- Added regression coverage for local CASC reads, DB2 schema arrays, item slot names, and item model selection.
- Added architecture, MCP tool, HTTP runtime, deployment, cache layout, and performance documentation.
- Verified Retail, PTR, Beta, Classic, Classic Era, and Classic Titan DB2 coverage across CN, US, EU, KR, and TW remote products.
