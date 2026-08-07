---
name: wowdata
description: Query and export World of Warcraft data with the wowdata CLI, including local or CDN CASC/DB2 data, read-only SQL atomic queries, independent Hotfix records, files, icons, textures, spells, encounters, items, creatures, decor, Builds, profiles, cache, and diagnostics. Use when a user asks to inspect, search, stream, decode, or export WoW data or diagnose wowdata.
---

# wowdata

Use the `wowdata` executable from `PATH`. This Skill is CLI-only: do not search for a bundled executable and do not use MCP or another server transport.

## Choose the command

- Use `sql` for a read-only DB2 query, including joins, subqueries, CTEs, aggregates, ordering, limits, `EXPLAIN`, or `ANALYZE`. Read [sql.md](references/sql.md).
- Use `hotfix query` for Hotfix records. Hotfix is an independent query system, not a DB2 SQL table. Read [hotfix.md](references/hotfix.md).
- Use `db2` for schema, rows, search, foreign-key, and stream operations; use domain commands for semantic workflows; use `casc`, `file`, `icon`, `video`, `profile`, `cache`, `doctor`, or `warmup` for their named purposes. Read [commands.md](references/commands.md) when a flag or command shape is unfamiliar, and [tables.yaml](references/tables.yaml) when mapping a semantic request to DB2 tables.

## Resolve the target

Resolve the target before reading CASC or DB2 data.

1. If the user gives a Profile, pass only `--profile <name>`; do not combine it with target flags.
2. Resolve product from the user's words or conversation context. Read [clients.yaml](references/clients.yaml) for aliases. Product has no default; if it remains ambiguous, ask one short product question before running a data command. Treat “硬核服” as a server rule and resolve its actual product from context.
3. Preserve every explicit source, path, region, product, Build, and locale. Read [locales.yaml](references/locales.yaml) for locale aliases.
4. If no source or local path is stated, use remote CDN. For an otherwise unspecified remote target, pass `--source remote --region cn --build latest --locale zhCN` plus the resolved product.
5. For `--source local`, require `--path <client>` and complete the remaining target fields explicitly; never guess missing local-client facts.

Never rely on CLI target defaults; the CLI intentionally has none. `--build latest` resolves at execution time and must not be hard-coded in this Skill.

Commands that do not read a game target do not need the full target: `video demux`, `doctor`, `cache`, `profile`, `update`, and `uninstall`. `casc products` needs only source discovery flags.

## Execute and report

Call the business command directly. Do not run `warmup` before an ordinary query; the CLI prepares required dependencies in-process. Keep exports at the requested path, or the current workspace when no path is given. Do not run destructive maintenance (`cache clear`, `profile remove`, `update`, `uninstall`) unless explicitly requested.

Treat stdout, stderr, and exit status as one contract: progress on stderr is informational; stdout is the result. Exit `0` is success. Exit `1` with JSON stdout is a structured failure—report `error.code` and `error.message`; plain stderr indicates CLI parsing/setup failure. A successful zero-row response is valid; check ID, product, Build, and locale before changing the query.

Preserve the resolved product, Build, region, locale, table/record identifiers, format, and output path/hash when present. Return requested data rather than download narration. See [errors.md](references/errors.md) for recovery decisions.
