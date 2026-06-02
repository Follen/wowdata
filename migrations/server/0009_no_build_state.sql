ALTER TABLE server_builds RENAME TO server_builds_old_no_build_state;

CREATE TABLE server_builds (
  region TEXT NOT NULL,
  product TEXT NOT NULL,
  locale TEXT NOT NULL,
  build_key TEXT NOT NULL,
  build_name TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN ('valid', 'stale', 'preparing', 'failed', 'no_build')),
  active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0, 1)),
  error TEXT NOT NULL DEFAULT '',
  discovered_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(region, product, locale, build_key)
);

INSERT INTO server_builds (
  region, product, locale, build_key, build_name, state, active, error, discovered_at, updated_at
)
SELECT
  region,
  product,
  locale,
  build_key,
  build_name,
  CASE
    WHEN state = 'failed' AND error LIKE 'no build found for %' THEN 'no_build'
    WHEN state = 'failed' AND error LIKE 'no build key found for %' THEN 'no_build'
    ELSE state
  END,
  active,
  error,
  discovered_at,
  updated_at
FROM server_builds_old_no_build_state;

DROP TABLE server_builds_old_no_build_state;

CREATE UNIQUE INDEX IF NOT EXISTS server_builds_one_active
ON server_builds(region, product, locale)
WHERE active = 1;
