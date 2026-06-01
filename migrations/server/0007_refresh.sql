CREATE TABLE IF NOT EXISTS server_refresh_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  region TEXT NOT NULL DEFAULT '',
  product TEXT NOT NULL DEFAULT '',
  locale TEXT NOT NULL DEFAULT '',
  build_key TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN ('valid', 'stale', 'preparing', 'failed')),
  error TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT
);
