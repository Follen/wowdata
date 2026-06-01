CREATE TABLE IF NOT EXISTS server_metadata_sequences (
  name TEXT PRIMARY KEY,
  value INTEGER NOT NULL DEFAULT 0
);

ALTER TABLE server_materialized_tables
ADD COLUMN updated_seq INTEGER NOT NULL DEFAULT 0;

WITH ordered AS (
  SELECT
    rowid AS row_id,
    ROW_NUMBER() OVER (
      ORDER BY updated_at, region, product, locale, table_name, build_key
    ) AS sequence_value
  FROM server_materialized_tables
)
UPDATE server_materialized_tables
SET updated_seq = (
  SELECT sequence_value
  FROM ordered
  WHERE ordered.row_id = server_materialized_tables.rowid
)
WHERE updated_seq = 0;

INSERT OR IGNORE INTO server_metadata_sequences(name, value)
VALUES ('server_materialized_tables', 0);

UPDATE server_metadata_sequences
SET value = max(
  value,
  (SELECT COALESCE(MAX(updated_seq), 0) FROM server_materialized_tables)
)
WHERE name = 'server_materialized_tables';

DROP INDEX IF EXISTS server_materialized_tables_lookup;

CREATE INDEX IF NOT EXISTS server_materialized_tables_lookup
ON server_materialized_tables(region, product, locale, table_name, state, updated_seq);
