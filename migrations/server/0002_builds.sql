CREATE TABLE IF NOT EXISTS server_builds (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  locale TEXT NOT NULL,
  build_key TEXT NOT NULL,
  build_name TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN ('valid', 'stale', 'preparing', 'failed')),
  active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0, 1)),
  error TEXT NOT NULL DEFAULT '',
  discovered_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product, locale, build_key)
);

CREATE UNIQUE INDEX IF NOT EXISTS server_builds_one_active
ON server_builds(region, product, locale)
WHERE active = 1;
