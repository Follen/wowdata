CREATE TABLE IF NOT EXISTS materialized_tables (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  build_key TEXT NOT NULL,
  build_name TEXT NOT NULL,
  locale TEXT NOT NULL,
  table_name TEXT NOT NULL,
  db2_file_data_id INTEGER NOT NULL,
  dbd_definition_hash TEXT NOT NULL,
  decoder_version TEXT NOT NULL,
  materializer_version TEXT NOT NULL,
  parquet_path TEXT NOT NULL,
  row_count INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product, build_key, locale, table_name)
);
