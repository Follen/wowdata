package health

import (
	"context"
	"database/sql"
	"errors"
	"os"

	"wowdata/internal/server/config"
	"wowdata/internal/server/storage/metadata"
)

type MetadataProvider struct {
	Config         config.Config
	MetadataDBPath string
}

func (p MetadataProvider) HealthSnapshot(ctx context.Context) (Snapshot, error) {
	metadataDBPath := p.MetadataDBPath
	if metadataDBPath == "" {
		metadataDBPath = p.Config.Cache.MetadataDB
	}

	db, err := metadata.Open(metadataDBPath)
	if err != nil {
		return Snapshot{}, err
	}
	defer db.Close()

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

		active, err := metadata.ActiveBuild(ctx, db, target.Region, target.Product, target.Locale)
		if err == nil {
			input.ActiveBuild = active.Key.BuildKey
			if active.State == metadata.StateValid {
				tables, err := metadata.ListValidMaterializedTables(ctx, db, metadata.TableCatalogLookup{
					Region:   target.Region,
					Product:  target.Product,
					Locale:   target.Locale,
					BuildKey: active.Key.BuildKey,
				})
				if err != nil {
					return Snapshot{}, err
				}
				if len(tables) > 0 {
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
