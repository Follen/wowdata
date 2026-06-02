package metadata

import (
	"context"
	"database/sql"
)

type BuildKey struct {
	Region   string
	Product  string
	Locale   string
	BuildKey string
}

type Build struct {
	Key       BuildKey
	BuildName string
	State     string
	Active    bool
	Error     string
}

func UpsertDiscoveredBuild(ctx context.Context, db *sql.DB, build Build) error {
	state := build.State
	if state == "" {
		state = StatePreparing
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO server_builds (
  region, product, locale, build_key, build_name, state, active, error, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(region, product, locale, build_key) DO UPDATE SET
  build_name = excluded.build_name,
  state = excluded.state,
  error = excluded.error,
  updated_at = CURRENT_TIMESTAMP`,
		build.Key.Region,
		build.Key.Product,
		build.Key.Locale,
		build.Key.BuildKey,
		build.BuildName,
		state,
		boolInt(build.Active),
		build.Error,
	)
	return err
}

func MarkBuildPreparing(ctx context.Context, db *sql.DB, key BuildKey) error {
	return markBuildState(ctx, db, key, StatePreparing, "")
}

func MarkBuildReady(ctx context.Context, db *sql.DB, key BuildKey) error {
	return markBuildState(ctx, db, key, StateValid, "")
}

func MarkBuildFailed(ctx context.Context, db *sql.DB, key BuildKey, message string) error {
	return markBuildState(ctx, db, key, StateFailed, message)
}

func MarkBuildNoBuild(ctx context.Context, db *sql.DB, key BuildKey, message string) error {
	return markBuildState(ctx, db, key, StateNoBuild, message)
}

func ActivateBuild(ctx context.Context, db *sql.DB, key BuildKey) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var state string
	if err = tx.QueryRowContext(ctx, `
SELECT state FROM server_builds
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&state); err != nil {
		return err
	}
	if state != StateValid {
		err = ErrBuildNotReady
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE server_builds
SET active = 0, updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND locale = ?`,
		key.Region, key.Product, key.Locale,
	); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE server_builds
SET active = 1, updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

func ActiveBuild(ctx context.Context, db *sql.DB, region, product, locale string) (Build, error) {
	var build Build
	var active int
	err := db.QueryRowContext(ctx, `
SELECT region, product, locale, build_key, build_name, state, active, error
FROM server_builds
WHERE region = ? AND product = ? AND locale = ? AND active = 1
LIMIT 1`,
		region, product, locale,
	).Scan(
		&build.Key.Region,
		&build.Key.Product,
		&build.Key.Locale,
		&build.Key.BuildKey,
		&build.BuildName,
		&build.State,
		&active,
		&build.Error,
	)
	build.Active = active != 0
	return build, err
}

func LatestBuildForTarget(ctx context.Context, db *sql.DB, region, product, locale string) (Build, error) {
	var build Build
	var active int
	err := db.QueryRowContext(ctx, `
SELECT region, product, locale, build_key, build_name, state, active, error
FROM server_builds
WHERE region = ? AND product = ? AND locale = ?
ORDER BY updated_at DESC, discovered_at DESC, build_key DESC
LIMIT 1`,
		region, product, locale,
	).Scan(
		&build.Key.Region,
		&build.Key.Product,
		&build.Key.Locale,
		&build.Key.BuildKey,
		&build.BuildName,
		&build.State,
		&active,
		&build.Error,
	)
	build.Active = active != 0
	return build, err
}

func markBuildState(ctx context.Context, db *sql.DB, key BuildKey, state string, message string) error {
	_, err := db.ExecContext(ctx, `
UPDATE server_builds
SET state = ?, error = ?, updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		state,
		message,
		key.Region,
		key.Product,
		key.Locale,
		key.BuildKey,
	)
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
