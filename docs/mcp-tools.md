# wowdata MCP Tools

This document lists the MCP tool names exposed by each wowdata transport.

## stdio transport

The local stdio transport is started with:

```sh
wowdata mcp stdio
```

Exact stdio tool list:

- `wow_warmup`
- `wow_casc`
- `wow_query`
- `wow_file`
- `wow_icon`
- `wow_spell`
- `wow_encounter`
- `wow_item`
- `wow_creature`
- `wow_decor`
- `wow_video`

## HTTP transport

The Streamable HTTP transport exposes MCP at `/mcp`.

Exact default HTTP tool list:

- `wow_builds`
- `wow_status`
- `wow_query`
- `wow_item`
- `wow_spell`
- `wow_file`
- `wow_icon`
- `wow_creature`
- `wow_encounter`
- `wow_decor`
- `wow_video`

`wow_warmup` is not an ordinary HTTP tool. HTTP prepares data lazily for request contexts, and explicit prepare operations are admin-only.

`wow_query` supports the normal table-scoped query modes plus an internal verification catalog mode:

- `mode=tables`: lists Go-readable DB2 tables for the requested `region`, `product`, `locale`, and optional `buildKey`. This mode does not require `table` and is intended for parity harness/admin verification, including extra-table detection.

## HTTP admin tools

Admin tools are hidden by default. Enable them with:

```yaml
tools:
  expose_admin_tools: true
```

Exact admin tool list:

- `wow_refresh_builds`
- `wow_prepare`
- `wow_prune_cache`

When admin tools are enabled, the HTTP MCP tool list is the default HTTP list plus the admin tool list above.
