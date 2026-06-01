CREATE TABLE IF NOT EXISTS server_materialized_tables (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  locale TEXT NOT NULL,
  build_key TEXT NOT NULL,
  table_name TEXT NOT NULL,
  db2_file_data_id INTEGER NOT NULL,
  dbd_hash TEXT NOT NULL DEFAULT '',
  decoder_version TEXT NOT NULL DEFAULT '',
  materializer_version TEXT NOT NULL DEFAULT '',
  parquet_path TEXT NOT NULL DEFAULT '',
  row_count INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL CHECK(state IN ('valid', 'stale', 'preparing', 'failed')),
  error TEXT NOT NULL DEFAULT '',
  updated_seq INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product, locale, build_key, table_name)
);

CREATE INDEX IF NOT EXISTS server_materialized_tables_lookup
ON server_materialized_tables(region, product, locale, table_name, state, updated_seq);

CREATE TABLE IF NOT EXISTS server_metadata_sequences (
  name TEXT PRIMARY KEY,
  value INTEGER NOT NULL DEFAULT 0
);
