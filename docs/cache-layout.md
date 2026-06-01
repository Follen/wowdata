# Cache Layout

This document describes the cache and output paths used by wowdata runtimes.
Concrete roots come from CLI flags, local defaults, or the HTTP YAML config.

HTTP config mapping:

- `cache.raw_dir` controls the remote CASC cache root.
- `cache.db2_dir` controls Parquet DB2 materialization output.
- `cache.metadata_db` controls the SQLite metadata database path.
- `cache.duckdb_path` controls the DuckDB database file path.
- `artifacts.root` controls exported file and icon storage.
- `artifacts.base_url` controls public artifact URLs returned by HTTP MCP.

## Raw CASC

Remote CASC data is cached below:

```text
<cache-root>/raw/casc/<region>/<product>/<build-key>/
```

The path stores downloaded config, index, encoding, root, install, download,
and data fragments required to read files from a Blizzard CDN build. The
`build-key` is the CASC build/config identity for a specific product build.

## SQLite Metadata

HTTP metadata is stored at:

```text
<cache-root>/metadata.sqlite
```

The metadata database records discovered builds, active build flags,
materialized DB2 tables, stale fingerprints, and cache audit rows. Build
activation updates the active build transactionally so a failed prepare cannot
silently replace a known-good active build.

## Parquet DB2 Cache

Materialized DB2 tables are stored below:

```text
<cache-root>/db2/<region>/<product>/<locale>/<build-key>/<table>.parquet
```

Each materialized table records a source fingerprint in SQLite. If the source
build, schema, locale, or materialization options change, the previous
fingerprint is stale and the table must be materialized again before query.

## DuckDB

The HTTP query engine opens:

```text
<cache-root>/duckdb/wowdata.duckdb
```

DuckDB reads materialized Parquet files for HTTP `wow_query` queries. The engine
creates the parent directory before opening the database file so fresh Docker
volumes work without manual directory setup.

## Artifacts

Exported files are written below the configured artifact root:

```text
<artifact-root>/icons/
<artifact-root>/files/
```

HTTP serves artifact downloads from `/files/...` when the artifact root and
public base URL are configured. Artifact URLs are returned only for paths that
stay inside the configured root.

## Build Keys

Build keys identify the cached build content used by CASC and DB2 materialized
data. Cache paths include the build key whenever content can differ between
builds. Metadata also tracks active build status by region and product.

## Stale Fingerprints

A materialized DB2 table is considered stale when its recorded fingerprint no
longer matches the current build, schema, locale, or materialization settings.
Stale tables are not trusted for HTTP queries; the materializer prepares a new
Parquet file and updates metadata.

## Prune Rules

Prune planning must not delete:

- the active build for a region/product
- pinned contexts from HTTP config
- in-flight materialization or prepare targets

Eligible old builds can be added to a prune plan and recorded in cache audit.
Actual deletion must keep every path under the configured cache root and reject
path traversal metadata before touching the filesystem.
