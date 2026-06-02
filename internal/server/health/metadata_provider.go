package health

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"wowdata/internal/server/config"
	"wowdata/internal/server/storage/metadata"
)

type metadataOpener func(string) (*sql.DB, error)

type MetadataProvider struct {
	Config         config.Config
	MetadataDBPath string
	db             *sql.DB
}

func NewMetadataProvider(cfg config.Config, metadataDBPath string) (MetadataProvider, error) {
	return newMetadataProviderWithOpener(cfg, metadataDBPath, metadata.Open)
}

func NewMetadataProviderWithDB(cfg config.Config, metadataDBPath string, db *sql.DB) MetadataProvider {
	if metadataDBPath == "" {
		metadataDBPath = cfg.Cache.MetadataDB
	}
	return MetadataProvider{
		Config:         cfg,
		MetadataDBPath: metadataDBPath,
		db:             db,
	}
}

func newMetadataProviderWithOpener(cfg config.Config, metadataDBPath string, opener metadataOpener) (MetadataProvider, error) {
	if metadataDBPath == "" {
		metadataDBPath = cfg.Cache.MetadataDB
	}
	db, err := opener(metadataDBPath)
	if err != nil {
		return MetadataProvider{}, err
	}
	return MetadataProvider{
		Config:         cfg,
		MetadataDBPath: metadataDBPath,
		db:             db,
	}, nil
}

func (p MetadataProvider) Close() error {
	if p.db == nil {
		return nil
	}
	return p.db.Close()
}

func (p MetadataProvider) DB() *sql.DB {
	return p.db
}

func (p MetadataProvider) HealthSnapshot(ctx context.Context) (Snapshot, error) {
	metadataDBPath := p.MetadataDBPath
	if metadataDBPath == "" {
		metadataDBPath = p.Config.Cache.MetadataDB
	}
	if p.db == nil {
		return Snapshot{}, fmt.Errorf("metadata provider database is unavailable")
	}

	targets := make([]TargetInput, 0, len(p.Config.Prepare.Targets))
	for _, target := range p.Config.Prepare.Targets {
		input := TargetInput{
			Label:   target.Label,
			Region:  target.Region,
			Product: target.Product,
			Locale:  target.Locale,
			Strict:  target.Strict,
			State:   StatePreparing,
		}

		active, err := metadata.ActiveBuild(ctx, p.db, target.Region, target.Product, target.Locale)
		if err == nil {
			input.ActiveBuild = active.Key.BuildKey
			if active.State == metadata.StateValid {
				tables, err := metadata.ListValidMaterializedTables(ctx, p.db, metadata.TableCatalogLookup{
					Region:   target.Region,
					Product:  target.Product,
					Locale:   target.Locale,
					BuildKey: active.Key.BuildKey,
				})
				if err != nil {
					return Snapshot{}, err
				}
				input.PrepareCurrent, input.PrepareTotal = requiredTableProgress(p.Config.Prepare.DefaultTables, tables)
				if requiredTablesReady(input.PrepareCurrent, input.PrepareTotal, len(tables)) {
					input.State = StateReady
					input.DB2Ready = true
				}
				sourceKey := metadata.SourceKey{
					Region:   target.Region,
					Product:  target.Product,
					Locale:   target.Locale,
					BuildKey: active.Key.BuildKey,
				}
				input.ListfileReady = metadataStateReady(ctx, p.db, func(ctx context.Context, db *sql.DB) (string, error) {
					return metadata.ListfileIndexState(ctx, db, sourceKey, "sqlite")
				})
				input.CASCReady = metadataStateReady(ctx, p.db, func(ctx context.Context, db *sql.DB) (string, error) {
					return metadata.CASCIndexState(ctx, db, sourceKey, "root-encoding-archive")
				})
			}
		} else if errors.Is(err, sql.ErrNoRows) {
			latest, latestErr := metadata.LatestBuildForTarget(ctx, p.db, target.Region, target.Product, target.Locale)
			if latestErr == nil {
				applyLatestBuildState(&input, latest)
			} else if !errors.Is(latestErr, sql.ErrNoRows) {
				return Snapshot{}, latestErr
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Snapshot{}, err
		}

		targets = append(targets, input)
	}

	var metadataDBBytes int64
	if info, err := os.Stat(metadataDBPath); err == nil {
		metadataDBBytes = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, err
	}

	return BuildSnapshot(Input{
		Targets: targets,
		Memory: Memory{
			MemorySoftLimitMB: p.Config.Limits.MemorySoftLimitMB,
			MemoryHardLimitMB: p.Config.Limits.MemoryHardLimitMB,
		},
		Storage: Storage{MetadataDBBytes: metadataDBBytes},
		Artifacts: Artifacts{
			Root:    p.Config.Artifacts.Root,
			BaseURL: p.Config.Server.BaseURL,
		},
		Limits: Limits{
			MaxParallelContextPrepares:       p.Config.Limits.MaxParallelContextPrepares,
			MaxParallelTableMaterializations: p.Config.Limits.MaxParallelTableMaterializations,
			MaxParallelDownloads:             p.Config.Limits.MaxParallelDownloads,
			MaxParallelQueries:               p.Config.Limits.MaxParallelQueries,
		},
	}), nil
}

func metadataStateReady(ctx context.Context, db *sql.DB, read func(context.Context, *sql.DB) (string, error)) bool {
	state, err := read(ctx, db)
	return err == nil && state == metadata.StateValid
}

func applyLatestBuildState(input *TargetInput, build metadata.Build) {
	if build.Key.BuildKey != "" {
		input.CandidateBuild = build.Key.BuildKey
	}
	if build.BuildName != "" {
		input.CandidateBuildName = build.BuildName
	}
	switch build.State {
	case metadata.StateNoBuild:
		input.State = StateNoBuild
		input.Error = build.Error
	case metadata.StateFailed:
		input.State = StateFailed
		input.Error = build.Error
	case metadata.StateStale:
		input.State = StateStale
		input.Error = build.Error
	case metadata.StatePreparing:
		input.State = StatePreparing
		input.Error = build.Error
	}
}

func requiredTableProgress(required []string, tables []metadata.MaterializedTable) (int, int) {
	if len(required) == 0 {
		return 0, 0
	}
	validTables := make(map[string]bool, len(tables))
	for _, table := range tables {
		validTables[table.Key.TableName] = true
	}
	matched := 0
	for _, tableName := range required {
		if validTables[tableName] {
			matched++
		}
	}
	return matched, len(required)
}

func requiredTablesReady(current, total, validTableCount int) bool {
	if total == 0 {
		return validTableCount > 0
	}
	return current == total
}
