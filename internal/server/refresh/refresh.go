package refresh

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"wowdata/internal/server/storage/metadata"
)

type Target struct {
	Region  string
	Product string
	Locale  string
}

type TableFingerprint struct {
	TableName           string
	DB2FileDataID       int
	DBDHash             string
	DecoderVersion      string
	MaterializerVersion string
}

type BuildCandidate struct {
	Region             string
	Product            string
	Locale             string
	BuildKey           string
	BuildName          string
	ListfileSourceHash string
	CASCBuildConfig    string
	CASCCDNConfig      string
	Tables             []TableFingerprint
}

type Discoverer interface {
	LatestBuild(context.Context, Target) (BuildCandidate, error)
}

type CandidatePreparer interface {
	PrepareCandidate(context.Context, BuildCandidate) error
}

type Workflow struct {
	DB         *sql.DB
	Discoverer Discoverer
	Preparer   CandidatePreparer
}

type Result struct {
	Candidate   BuildCandidate
	Activated   bool
	StaleTables []string
}

func (w Workflow) RefreshTarget(ctx context.Context, target Target) (Result, error) {
	if w.DB == nil {
		return Result{}, errors.New("metadata db is required")
	}
	if w.Discoverer == nil {
		return Result{}, errors.New("discoverer is required")
	}

	candidate, err := w.Discoverer.LatestBuild(ctx, target)
	if err != nil {
		return Result{}, err
	}
	if candidate.BuildKey == "" {
		return Result{}, nil
	}
	candidate = candidate.withTargetDefaults(target)

	result := Result{Candidate: candidate}
	buildKey := metadata.BuildKey{
		Region: candidate.Region, Product: candidate.Product, Locale: candidate.Locale, BuildKey: candidate.BuildKey,
	}

	active, activeErr := metadata.ActiveBuild(ctx, w.DB, candidate.Region, candidate.Product, candidate.Locale)
	if activeErr != nil && !errors.Is(activeErr, sql.ErrNoRows) {
		return result, activeErr
	}
	hasActive := activeErr == nil
	if hasActive && active.Key.BuildKey == candidate.BuildKey {
		unchanged, err := candidateUnchangedForActive(ctx, w.DB, active, candidate)
		if err != nil {
			return result, err
		}
		if unchanged {
			return result, nil
		}
	}
	sameActive := hasActive && active.Key.BuildKey == candidate.BuildKey

	if !sameActive {
		if err := metadata.UpsertDiscoveredBuild(ctx, w.DB, metadata.Build{
			Key:       buildKey,
			BuildName: candidate.BuildName,
			State:     metadata.StatePreparing,
		}); err != nil {
			return result, err
		}
	}
	markCandidateFailed := func(err error) {
		if !sameActive {
			_ = metadata.MarkBuildFailed(ctx, w.DB, buildKey, err.Error())
		}
	}

	if w.Preparer != nil {
		if err := w.Preparer.PrepareCandidate(ctx, candidate); err != nil {
			markCandidateFailed(err)
			return result, err
		}
	}
	if err := w.upsertSources(ctx, candidate); err != nil {
		markCandidateFailed(err)
		return result, err
	}

	if err := metadata.MarkBuildReady(ctx, w.DB, buildKey); err != nil {
		return result, err
	}

	if hasActive && active.Key.BuildKey == candidate.BuildKey {
		staleTables, err := markChangedTablesStaleForBuild(ctx, w.DB, active.Key, candidate.Tables)
		if err != nil {
			return result, err
		}
		result.StaleTables = staleTables
		return result, nil
	}

	if err := metadata.ActivateBuild(ctx, w.DB, buildKey); err != nil {
		return result, err
	}
	result.Activated = true

	if hasActive {
		staleTables, err := markChangedTablesStaleForBuild(ctx, w.DB, active.Key, candidate.Tables)
		if err != nil {
			return result, err
		}
		result.StaleTables = staleTables
	}
	return result, nil
}

func (w Workflow) upsertSources(ctx context.Context, candidate BuildCandidate) error {
	if candidate.ListfileSourceHash != "" {
		_, err := metadata.UpsertListfileSource(ctx, w.DB, metadata.ListfileSource{
			Region:     candidate.Region,
			Product:    candidate.Product,
			Locale:     candidate.Locale,
			BuildKey:   candidate.BuildKey,
			SourceHash: candidate.ListfileSourceHash,
			State:      metadata.StateValid,
		})
		if err != nil {
			return err
		}
	}
	if candidate.CASCBuildConfig != "" || candidate.CASCCDNConfig != "" {
		_, err := metadata.UpsertCASCSource(ctx, w.DB, metadata.CASCSource{
			Region:      candidate.Region,
			Product:     candidate.Product,
			Locale:      candidate.Locale,
			BuildKey:    candidate.BuildKey,
			BuildConfig: candidate.CASCBuildConfig,
			CDNConfig:   candidate.CASCCDNConfig,
			State:       metadata.StateValid,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func markChangedTablesStaleForBuild(ctx context.Context, db *sql.DB, key metadata.BuildKey, tables []TableFingerprint) ([]string, error) {
	stale := make([]string, 0)
	for _, table := range tables {
		existing, err := materializedTableForBuild(ctx, db, metadata.TableKey{
			Region: key.Region, Product: key.Product, Locale: key.Locale, BuildKey: key.BuildKey, TableName: table.TableName,
		})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if tableMatches(existing, table) {
			continue
		}
		if err := metadata.MarkMaterializedTableState(ctx, db, existing.Key, metadata.StateStale, "db2 fingerprint changed"); err != nil {
			return nil, fmt.Errorf("mark %s stale: %w", table.TableName, err)
		}
		stale = append(stale, table.TableName)
	}
	return stale, nil
}

func candidateUnchangedForActive(ctx context.Context, db *sql.DB, active metadata.Build, candidate BuildCandidate) (bool, error) {
	if candidate.ListfileSourceHash == "" {
		exists, err := listfileSourceExists(ctx, db, active.Key)
		if err != nil || exists {
			return !exists, err
		}
	} else {
		listfileUnchanged, err := listfileSourceMatches(ctx, db, active.Key, candidate.ListfileSourceHash)
		if err != nil || !listfileUnchanged {
			return listfileUnchanged, err
		}
	}
	if candidate.CASCBuildConfig == "" && candidate.CASCCDNConfig == "" {
		exists, err := cascSourceExists(ctx, db, active.Key)
		if err != nil || exists {
			return !exists, err
		}
	} else {
		cascUnchanged, err := cascSourceMatches(ctx, db, active.Key, candidate.CASCBuildConfig, candidate.CASCCDNConfig)
		if err != nil || !cascUnchanged {
			return cascUnchanged, err
		}
	}
	if len(candidate.Tables) == 0 {
		exists, err := materializedTablesExist(ctx, db, active.Key)
		if err != nil || exists {
			return !exists, err
		}
	}
	for _, table := range candidate.Tables {
		existing, err := materializedTableForBuild(ctx, db, metadata.TableKey{
			Region: active.Key.Region, Product: active.Key.Product, Locale: active.Key.Locale, BuildKey: active.Key.BuildKey, TableName: table.TableName,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !tableMatches(existing, table) {
			return false, nil
		}
	}
	return true, nil
}

func listfileSourceExists(ctx context.Context, db *sql.DB, key metadata.BuildKey) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, `
SELECT 1 FROM server_listfile_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?
LIMIT 1`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func listfileSourceMatches(ctx context.Context, db *sql.DB, key metadata.BuildKey, sourceHash string) (bool, error) {
	if sourceHash == "" {
		return true, nil
	}
	var current string
	err := db.QueryRowContext(ctx, `
SELECT source_hash FROM server_listfile_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return current == sourceHash, nil
}

func cascSourceExists(ctx context.Context, db *sql.DB, key metadata.BuildKey) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, `
SELECT 1 FROM server_casc_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?
LIMIT 1`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func cascSourceMatches(ctx context.Context, db *sql.DB, key metadata.BuildKey, buildConfig, cdnConfig string) (bool, error) {
	if buildConfig == "" && cdnConfig == "" {
		return true, nil
	}
	var currentBuildConfig, currentCDNConfig string
	err := db.QueryRowContext(ctx, `
SELECT build_config, cdn_config FROM server_casc_sources
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&currentBuildConfig, &currentCDNConfig)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return currentBuildConfig == buildConfig && currentCDNConfig == cdnConfig, nil
}

func materializedTablesExist(ctx context.Context, db *sql.DB, key metadata.BuildKey) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, `
SELECT 1 FROM server_materialized_tables
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?
LIMIT 1`,
		key.Region, key.Product, key.Locale, key.BuildKey,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func materializedTableForBuild(ctx context.Context, db *sql.DB, key metadata.TableKey) (metadata.MaterializedTable, error) {
	var table metadata.MaterializedTable
	err := db.QueryRowContext(ctx, `
SELECT region, product, locale, build_key, table_name,
  db2_file_data_id, dbd_hash, decoder_version, materializer_version,
  parquet_path, row_count, state, error
FROM server_materialized_tables
WHERE region = ? AND product = ? AND locale = ? AND build_key = ? AND table_name = ? AND state = ?
LIMIT 1`,
		key.Region,
		key.Product,
		key.Locale,
		key.BuildKey,
		key.TableName,
		metadata.StateValid,
	).Scan(
		&table.Key.Region,
		&table.Key.Product,
		&table.Key.Locale,
		&table.Key.BuildKey,
		&table.Key.TableName,
		&table.DB2FileDataID,
		&table.DBDHash,
		&table.DecoderVersion,
		&table.MaterializerVersion,
		&table.ParquetPath,
		&table.RowCount,
		&table.State,
		&table.Error,
	)
	return table, err
}

func tableMatches(existing metadata.MaterializedTable, next TableFingerprint) bool {
	return existing.DB2FileDataID == next.DB2FileDataID &&
		existing.DBDHash == next.DBDHash &&
		existing.DecoderVersion == next.DecoderVersion &&
		existing.MaterializerVersion == next.MaterializerVersion
}

func (c BuildCandidate) withTargetDefaults(target Target) BuildCandidate {
	if c.Region == "" {
		c.Region = target.Region
	}
	if c.Product == "" {
		c.Product = target.Product
	}
	if c.Locale == "" {
		c.Locale = target.Locale
	}
	if c.BuildName == "" {
		c.BuildName = c.BuildKey
	}
	return c
}
