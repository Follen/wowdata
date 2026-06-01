package metadata

import "database/sql"

type Build struct {
	Region    string
	Product   string
	BuildKey  string
	BuildName string
	Active    bool
	Ready     bool
	Error     string
}

func UpsertBuild(db *sql.DB, build Build) error {
	_, err := db.Exec(`
INSERT INTO builds (region, product, build_key, build_name, active, ready, error)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(region, product, build_key) DO UPDATE SET
  build_name = excluded.build_name,
  active = excluded.active,
  ready = excluded.ready,
  error = excluded.error`,
		build.Region,
		build.Product,
		build.BuildKey,
		build.BuildName,
		boolInt(build.Active),
		boolInt(build.Ready),
		build.Error,
	)
	return err
}

func GetBuild(db *sql.DB, region, product, buildKey string) (Build, error) {
	var build Build
	var active, ready int
	err := db.QueryRow(`
SELECT region, product, build_key, build_name, active, ready, error
FROM builds
WHERE region = ? AND product = ? AND build_key = ?`,
		region,
		product,
		buildKey,
	).Scan(
		&build.Region,
		&build.Product,
		&build.BuildKey,
		&build.BuildName,
		&active,
		&ready,
		&build.Error,
	)
	build.Active = active != 0
	build.Ready = ready != 0
	return build, err
}

func ActiveBuild(db *sql.DB, region, product string) (Build, error) {
	var build Build
	var active, ready int
	err := db.QueryRow(`
SELECT region, product, build_key, build_name, active, ready, error
FROM builds
WHERE region = ? AND product = ? AND active = 1
LIMIT 1`,
		region,
		product,
	).Scan(
		&build.Region,
		&build.Product,
		&build.BuildKey,
		&build.BuildName,
		&active,
		&ready,
		&build.Error,
	)
	build.Active = active != 0
	build.Ready = ready != 0
	return build, err
}

func ListBuilds(db *sql.DB, region, product string) ([]Build, error) {
	rows, err := db.Query(`
SELECT region, product, build_key, build_name, active, ready, error
FROM builds
WHERE region = ? AND product = ?
ORDER BY discovered_at ASC, build_key ASC`,
		region,
		product,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []Build
	for rows.Next() {
		var build Build
		var active, ready int
		if err := rows.Scan(
			&build.Region,
			&build.Product,
			&build.BuildKey,
			&build.BuildName,
			&active,
			&ready,
			&build.Error,
		); err != nil {
			return nil, err
		}
		build.Active = active != 0
		build.Ready = ready != 0
		builds = append(builds, build)
	}
	return builds, rows.Err()
}

func ActivateBuild(db *sql.DB, region, product, buildKey string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
UPDATE builds
SET active = 0
WHERE region = ? AND product = ?`,
		region,
		product,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`
UPDATE builds
SET active = 1, ready = 1, error = ''
WHERE region = ? AND product = ? AND build_key = ?`,
		region,
		product,
		buildKey,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
