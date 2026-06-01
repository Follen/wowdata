CREATE TABLE IF NOT EXISTS server_metadata_sequences (
  name TEXT PRIMARY KEY,
  value INTEGER NOT NULL DEFAULT 0
);

ALTER TABLE server_materialized_tables
ADD COLUMN updated_seq INTEGER NOT NULL DEFAULT 0;

DROP INDEX IF EXISTS server_materialized_tables_lookup;

CREATE INDEX IF NOT EXISTS server_materialized_tables_lookup
ON server_materialized_tables(region, product, locale, table_name, state, updated_seq);
