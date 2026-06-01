package http

import (
	"context"
	"database/sql"
	"fmt"

	"wowdata/internal/cache/metadata"
)

type BuildMetadata struct {
	Region    string
	Product   string
	BuildKey  string
	BuildName string
	Active    bool
	Ready     bool
	Error     string
}

type BuildWatcherStore interface {
	ActiveBuild(ctx context.Context, region, product string) (BuildMetadata, error)
	DiscoverBuilds(ctx context.Context, region, product string) ([]BuildMetadata, error)
	ActivateBuild(ctx context.Context, build BuildMetadata) error
}

type BuildPrepareFunc func(context.Context, BuildMetadata) error

type BuildWatcher struct {
	store   BuildWatcherStore
	prepare BuildPrepareFunc
}

func NewBuildWatcher(store BuildWatcherStore, prepare BuildPrepareFunc) *BuildWatcher {
	return &BuildWatcher{store: store, prepare: prepare}
}

type MetadataBuildStore struct {
	db *sql.DB
}

func NewMetadataBuildStore(db *sql.DB) *MetadataBuildStore {
	return &MetadataBuildStore{db: db}
}

func (s *MetadataBuildStore) ActiveBuild(_ context.Context, region, product string) (BuildMetadata, error) {
	build, err := metadata.ActiveBuild(s.db, region, product)
	if err != nil {
		return BuildMetadata{}, err
	}
	return buildMetadataFromRecord(build), nil
}

func (s *MetadataBuildStore) DiscoverBuilds(_ context.Context, region, product string) ([]BuildMetadata, error) {
	records, err := metadata.ListBuilds(s.db, region, product)
	if err != nil {
		return nil, err
	}
	builds := make([]BuildMetadata, 0, len(records))
	for _, record := range records {
		builds = append(builds, buildMetadataFromRecord(record))
	}
	return builds, nil
}

func (s *MetadataBuildStore) ActivateBuild(_ context.Context, build BuildMetadata) error {
	return metadata.ActivateBuild(s.db, build.Region, build.Product, build.BuildKey)
}

func (w *BuildWatcher) CheckOnce(ctx context.Context, region, product string) error {
	if w == nil || w.store == nil {
		return fmt.Errorf("build watcher store is required")
	}
	if w.prepare == nil {
		return fmt.Errorf("build prepare function is required")
	}
	active, err := w.store.ActiveBuild(ctx, region, product)
	if err != nil {
		return err
	}
	builds, err := w.store.DiscoverBuilds(ctx, region, product)
	if err != nil {
		return err
	}
	for _, build := range builds {
		if build.Region != region || build.Product != product {
			continue
		}
		if build.BuildKey == "" || build.BuildKey == active.BuildKey {
			continue
		}
		if err := w.prepare(ctx, build); err != nil {
			return err
		}
		build.Ready = true
		return w.store.ActivateBuild(ctx, build)
	}
	return nil
}

func buildMetadataFromRecord(build metadata.Build) BuildMetadata {
	return BuildMetadata{
		Region:    build.Region,
		Product:   build.Product,
		BuildKey:  build.BuildKey,
		BuildName: build.BuildName,
		Active:    build.Active,
		Ready:     build.Ready,
		Error:     build.Error,
	}
}
