# wowdata-sql-v1 atomic queries

Use `wowdata sql` for one read-only, atomic DB2 query. It executes against static DB2 data and emits one structured response; it does not query Hotfix records.

## Input forms

Provide exactly one SQL source:

```powershell
wowdata sql "SELECT ID, Name_lang FROM SpellName WHERE ID = :spell_id" --param spell_id=133 --format json <target>
Get-Content query.sql -Raw | wowdata sql --stdin --format jsonl <target>
wowdata sql --file query.sql --format csv <target>
```

`--param name=value` may be repeated. Parameters are named and should be used instead of interpolating user input. `--format` accepts `json` (default), `jsonl`, or `csv`.

## Supported query shape

The engine supports projections and aliases, predicates, `IN`, joins, scalar/subqueries, CTEs including recursive CTEs, grouping/aggregates, ordering, and limits. Use `EXPLAIN` for a plan without row reads and `EXPLAIN ANALYZE` (or `EXPLAIN ANALYZE SELECT ...`) for execution counters and timings:

```sql
EXPLAIN SELECT ID FROM SpellEffect WHERE SpellID = :spell_id;
EXPLAIN ANALYZE SELECT se.ID, COUNT(*) AS n
FROM SpellEffect se WHERE se.SpellID = :spell_id GROUP BY se.ID;
```

Table and field names must match the resolved Build's DBD schema. Use `wowdata db2 schema <table> <target>` first when uncertain. Keep result sets bounded with predicates and `LIMIT`; use `db2 stream` for a simple full-table stream.

## Target and output rules

Append a complete target or `--profile`. Local SQL regression should use `--source local --path <client>` and explicit product/Build/locale/region. SQL has no Hotfix fallback. Report the query source, parameters (redacting secrets), target, format, row count, and plan/timing fields when requested.
