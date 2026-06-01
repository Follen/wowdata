CREATE TABLE IF NOT EXISTS products (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product)
);

CREATE TABLE IF NOT EXISTS builds (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  build_key TEXT NOT NULL,
  build_name TEXT NOT NULL,
  discovered_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  active INTEGER NOT NULL DEFAULT 0,
  ready INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(region, product, build_key)
);
