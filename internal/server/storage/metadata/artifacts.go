package metadata

import (
	"context"
	"database/sql"

	"wowdata/internal/server/storage/sqlitewrite"
)

type SourceKey struct {
	Region   string
	Product  string
	Locale   string
	BuildKey string
}

type ListfileSource struct {
	Region     string
	Product    string
	Locale     string
	BuildKey   string
	SourceHash string
	State      string
	Error      string
}

func (s ListfileSource) Key() SourceKey {
	return SourceKey{Region: s.Region, Product: s.Product, Locale: s.Locale, BuildKey: s.BuildKey}
}

type CASCSource struct {
	Region      string
	Product     string
	Locale      string
	BuildKey    string
	BuildConfig string
	CDNConfig   string
	State       string
	Error       string
}

func (s CASCSource) Key() SourceKey {
	return SourceKey{Region: s.Region, Product: s.Product, Locale: s.Locale, BuildKey: s.BuildKey}
}

type Artifact struct {
	Region      string
	Product     string
	Locale      string
	BuildKey    string
	Path        string
	DownloadURL string
	MIMEType    string
	Size        int64
	SHA256      string
}

func UpsertListfileSource(ctx context.Context, db *sql.DB, source ListfileSource) (bool, error) {
	var changed bool
	err := sqlitewrite.Do(ctx, func() error {
		var err error
		changed, err = upsertListfileSourceLocked(ctx, db, source)
		return err
	})
	return changed, err
}

func upsertListfileSourceLocked(ctx context.Context, db *sql.DB, source ListfileSource) (bool, error) {
	state := source.State
	if state == "" {
		state = StatePreparing
	}
	var oldHash string
	err := db.QueryRowContext(ctx, `
SELECT source_hash FROM server_listfile_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		source.Region, source.Product, source.Locale, source.BuildKey,
	).Scan(&oldHash)
	if err == sql.ErrNoRows {
		_, err := db.ExecContext(ctx, `
INSERT INTO server_listfile_sources (
  region, product, locale, build_key, source_hash, state, error, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			source.Region,
			source.Product,
			source.Locale,
			source.BuildKey,
			source.SourceHash,
			state,
			source.Error,
		)
		return false, err
	}
	if err != nil {
		return false, err
	}
	if oldHash == source.SourceHash {
		_, err := db.ExecContext(ctx, `
UPDATE server_listfile_sources
SET state = ?, error = ?, updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
			state,
			source.Error,
			source.Region,
			source.Product,
			source.Locale,
			source.BuildKey,
		)
		return false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `
UPDATE server_listfile_sources
SET source_hash = ?, state = ?, error = ?, updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		source.SourceHash,
		state,
		source.Error,
		source.Region,
		source.Product,
		source.Locale,
		source.BuildKey,
	); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE server_listfile_indexes
SET state = ?, error = '', updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		StateStale,
		source.Region,
		source.Product,
		source.Locale,
		source.BuildKey,
	); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func MarkListfileSourceState(ctx context.Context, db *sql.DB, key SourceKey, state string, message string) error {
	return sqlitewrite.Do(ctx, func() error {
		_, err := db.ExecContext(ctx, `
UPDATE server_listfile_sources
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
	})
}

func UpsertListfileIndexState(ctx context.Context, db *sql.DB, key SourceKey, indexName string, state string, message string) error {
	return sqlitewrite.Do(ctx, func() error {
		_, err := db.ExecContext(ctx, `
INSERT INTO server_listfile_indexes (
  region, product, locale, build_key, index_name, state, error, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(region, product, locale, build_key, index_name) DO UPDATE SET
  state = excluded.state,
  error = excluded.error,
  updated_at = CURRENT_TIMESTAMP`,
			key.Region,
			key.Product,
			key.Locale,
			key.BuildKey,
			indexName,
			state,
			message,
		)
		return err
	})
}

func ListfileIndexState(ctx context.Context, db *sql.DB, key SourceKey, indexName string) (string, error) {
	var state string
	err := db.QueryRowContext(ctx, `
SELECT state FROM server_listfile_indexes
WHERE region = ? AND product = ? AND locale = ? AND build_key = ? AND index_name = ?`,
		key.Region,
		key.Product,
		key.Locale,
		key.BuildKey,
		indexName,
	).Scan(&state)
	return state, err
}

func UpsertCASCSource(ctx context.Context, db *sql.DB, source CASCSource) (bool, error) {
	var changed bool
	err := sqlitewrite.Do(ctx, func() error {
		var err error
		changed, err = upsertCASCSourceLocked(ctx, db, source)
		return err
	})
	return changed, err
}

func upsertCASCSourceLocked(ctx context.Context, db *sql.DB, source CASCSource) (bool, error) {
	state := source.State
	if state == "" {
		state = StatePreparing
	}
	var oldBuildConfig, oldCDNConfig string
	err := db.QueryRowContext(ctx, `
SELECT build_config, cdn_config FROM server_casc_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		source.Region, source.Product, source.Locale, source.BuildKey,
	).Scan(&oldBuildConfig, &oldCDNConfig)
	if err == sql.ErrNoRows {
		_, err := db.ExecContext(ctx, `
INSERT INTO server_casc_sources (
  region, product, locale, build_key, build_config, cdn_config, state, error, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			source.Region,
			source.Product,
			source.Locale,
			source.BuildKey,
			source.BuildConfig,
			source.CDNConfig,
			state,
			source.Error,
		)
		return false, err
	}
	if err != nil {
		return false, err
	}
	if oldBuildConfig == source.BuildConfig && oldCDNConfig == source.CDNConfig {
		_, err := db.ExecContext(ctx, `
UPDATE server_casc_sources
SET state = ?, error = ?, updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
			state,
			source.Error,
			source.Region,
			source.Product,
			source.Locale,
			source.BuildKey,
		)
		return false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `
UPDATE server_casc_sources
SET build_config = ?, cdn_config = ?, state = ?, error = ?, updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		source.BuildConfig,
		source.CDNConfig,
		state,
		source.Error,
		source.Region,
		source.Product,
		source.Locale,
		source.BuildKey,
	); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE server_casc_indexes
SET state = ?, error = '', updated_at = CURRENT_TIMESTAMP
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		StateStale,
		source.Region,
		source.Product,
		source.Locale,
		source.BuildKey,
	); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func MarkCASCSourceState(ctx context.Context, db *sql.DB, key SourceKey, state string, message string) error {
	return sqlitewrite.Do(ctx, func() error {
		_, err := db.ExecContext(ctx, `
UPDATE server_casc_sources
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
	})
}

func UpsertCASCIndexState(ctx context.Context, db *sql.DB, key SourceKey, indexName string, state string, message string) error {
	return sqlitewrite.Do(ctx, func() error {
		_, err := db.ExecContext(ctx, `
INSERT INTO server_casc_indexes (
  region, product, locale, build_key, index_name, state, error, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(region, product, locale, build_key, index_name) DO UPDATE SET
  state = excluded.state,
  error = excluded.error,
  updated_at = CURRENT_TIMESTAMP`,
			key.Region,
			key.Product,
			key.Locale,
			key.BuildKey,
			indexName,
			state,
			message,
		)
		return err
	})
}

func CASCIndexState(ctx context.Context, db *sql.DB, key SourceKey, indexName string) (string, error) {
	var state string
	err := db.QueryRowContext(ctx, `
SELECT state FROM server_casc_indexes
WHERE region = ? AND product = ? AND locale = ? AND build_key = ? AND index_name = ?`,
		key.Region,
		key.Product,
		key.Locale,
		key.BuildKey,
		indexName,
	).Scan(&state)
	return state, err
}

func UpsertArtifact(ctx context.Context, db *sql.DB, artifact Artifact) error {
	return sqlitewrite.Do(ctx, func() error {
		_, err := db.ExecContext(ctx, `
INSERT INTO server_artifacts (
  region, product, locale, build_key, artifact_path, download_url,
  mime_type, size_bytes, sha256, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(artifact_path) DO UPDATE SET
  region = excluded.region,
  product = excluded.product,
  locale = excluded.locale,
  build_key = excluded.build_key,
  download_url = excluded.download_url,
  mime_type = excluded.mime_type,
  size_bytes = excluded.size_bytes,
  sha256 = excluded.sha256`,
			artifact.Region,
			artifact.Product,
			artifact.Locale,
			artifact.BuildKey,
			artifact.Path,
			artifact.DownloadURL,
			artifact.MIMEType,
			artifact.Size,
			artifact.SHA256,
		)
		return err
	})
}
