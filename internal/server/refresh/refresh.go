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
	if err := metadata.UpsertDiscoveredBuild(ctx, w.DB, metadata.Build{
		Key:       buildKey,
		BuildName: candidate.BuildName,
		State:     metadata.StatePreparing,
	}); err != nil {
		return result, err
	}

	if w.Preparer != nil {
		if err := w.Preparer.PrepareCandidate(ctx, candidate); err != nil {
			_ = metadata.MarkBuildFailed(ctx, w.DB, buildKey, err.Error())
			return result, err
		}
	}
	if err := w.upsertSources(ctx, candidate); err != nil {
		_ = metadata.MarkBuildFailed(ctx, w.DB, buildKey, err.Error())
		return result, err
	}

	staleTables, err := markChangedTablesStale(ctx, w.DB, candidate)
	if err != nil {
		_ = metadata.MarkBuildFailed(ctx, w.DB, buildKey, err.Error())
		return result, err
	}
	result.StaleTables = staleTables

	if err := metadata.MarkBuildReady(ctx, w.DB, buildKey); err != nil {
		return result, err
	}

	active, err := metadata.ActiveBuild(ctx, w.DB, candidate.Region, candidate.Product, candidate.Locale)
	if err == nil && active.Key.BuildKey == candidate.BuildKey {
		return result, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}

	if err := metadata.ActivateBuild(ctx, w.DB, buildKey); err != nil {
		return result, err
	}
	result.Activated = true
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

func markChangedTablesStale(ctx context.Context, db *sql.DB, candidate BuildCandidate) ([]string, error) {
	stale := make([]string, 0)
	for _, table := range candidate.Tables {
		existing, err := metadata.LatestValidMaterializedTable(ctx, db, metadata.TableLookup{
			Region:    candidate.Region,
			Product:   candidate.Product,
			Locale:    candidate.Locale,
			TableName: table.TableName,
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
