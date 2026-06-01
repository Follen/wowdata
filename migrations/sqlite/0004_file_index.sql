CREATE TABLE IF NOT EXISTS file_index (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  build_key TEXT NOT NULL,
  file_data_id INTEGER NOT NULL,
  filename TEXT NOT NULL,
  PRIMARY KEY(region, product, build_key, file_data_id)
);
