# HTTP Service Runtime

This document describes the runtime behavior of `wowdata-server mcp http`.

## Config path

Start the HTTP service with an optional YAML config:

```sh
wowdata-server mcp http --config /path/to/http.yaml
```

When `--config` is omitted, the service uses the server default HTTP config. CLI flags override the loaded config only when the flag is explicitly set; without a config file, the HTTP command flag defaults provide the bind address and public artifact paths.

Important YAML sections:

- `server`: bind host, port, and public `base_url`.
- `defaults`: fallback `region`, `product`, and `locale`.
- `contexts`: max warmed contexts and pinned contexts returned by `wow_builds`.
- `cache`: cache root, metadata SQLite path, raw cache, DB2 parquet cache, and DuckDB path.
- `artifacts`: local artifact root, public artifact `base_url`, and retention window.
- `prepare`: lazy prepare behavior, startup prewarm intent, DBD/listfile options, and default tables.
- `refresh`: build watcher cadence and retention policy.
- `limits`: prepare, materialization, query, and timeout limits.
- `tools`: admin and debug tool gates.

## Default contexts

The default request context is:

- Region: `cn`
- Product: `wow`
- Locale: `zhCN`

The default pinned contexts are:

- `CN Retail`: `cn` / `wow` / `zhCN`
- `CN PTR`: `cn` / `wowt` / `zhCN`
- `CN Classic`: `cn` / `wow_classic` / `zhCN`
- `CN Titan`: `cn` / `wow_classic_titan` / `zhCN`

HTTP tool calls may pass `region`, `product`, and `locale`; omitted values are filled from the defaults above.

## Lazy prepare

HTTP requests use lazy prepare by default. A tool such as `wow_query` resolves the request context, ensures the requested table is materialized, and then serves the query from the materialized cache. Successful table materializations are cached per `region/product/locale/table`, and concurrent requests for the same table share one in-flight materialization.

The stdio-only `wow_warmup` tool is not exposed as a default HTTP tool. Explicit prepare is available through `wow_prepare` only when admin tools are enabled.

## Artifact URL behavior

The HTTP runtime wires an artifact manager from `artifacts.root` and `artifacts.base_url`. When a tool result points at a file inside the artifact root, the service can return:

- `downloadUrl`: public URL for the file.
- `uri`: public artifact URL.
- `fileURI`: original local `file://` URI when a local URI was rewritten.
- `mimeType`, `name`, `size`, and `sha256` when file metadata is available.

Paths outside the configured artifact root are not rewritten into public artifact URLs.

## Build watcher

The build watcher compares the active build for a region/product with discovered builds. When it finds a different build, it runs the configured prepare function for that build. The watcher activates the new build only after prepare succeeds, so a failed prepare leaves the current active build in place.

The runtime config contains the watcher policy in `refresh.product_check_interval_minutes`, `refresh.auto_prepare_new_builds`, `refresh.keep_builds_per_product`, `refresh.prune_on_start`, and `refresh.max_cache_gb`.

## Admin tool gate

Admin tools are controlled by:

```yaml
tools:
  expose_admin_tools: true
```

When the gate is off, HTTP exposes only the default HTTP tool list. When the gate is on, HTTP also exposes:

- `wow_refresh_builds`
- `wow_prepare`
- `wow_prune_cache`
