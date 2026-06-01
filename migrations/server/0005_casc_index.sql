CREATE TABLE IF NOT EXISTS server_casc_sources (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  locale TEXT NOT NULL,
  build_key TEXT NOT NULL,
  build_config TEXT NOT NULL DEFAULT '',
  cdn_config TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN ('valid', 'stale', 'preparing', 'failed')),
  error TEXT NOT NULL DEFAULT '',
  fetched_at TEXT,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product, locale, build_key)
);
