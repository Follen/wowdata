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
	}), nil
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
