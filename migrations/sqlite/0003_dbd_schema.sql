CREATE TABLE IF NOT EXISTS dbd_schema (
  table_name TEXT NOT NULL,
  build_name TEXT NOT NULL,
  dbd_definition_hash TEXT NOT NULL,
  decoder_version TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(table_name, build_name, dbd_definition_hash, decoder_version)
);
