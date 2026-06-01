package http

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"wowdata/internal/cache/metadata"
)

func TestBuildWatcherKeepsOldActiveBuildWhenPrepareFails(t *testing.T) {
	oldBuild := BuildMetadata{Region: "cn", Product: "wow", BuildKey: "old", BuildName: "12.0.0.1", Active: true, Ready: true}
	newBuild := BuildMetadata{Region: "cn", Product: "wow", BuildKey: "new", BuildName: "12.0.0.2"}
	store := &fakeBuildWatcherStore{
		active:     oldBuild,
		discovered: []BuildMetadata{newBuild},
	}
	prepareErr := errors.New("prepare failed")
	watcher := NewBuildWatcher(store, func(context.Context, BuildMetadata) error {
		return prepareErr
	})

	err := watcher.CheckOnce(context.Background(), "cn", "wow")

	if !errors.Is(err, prepareErr) {
		t.Fatalf("CheckOnce error = %v, want %v", err, prepareErr)
	}
	if store.active.BuildKey != "old" {
		t.Fatalf("active build = %q, want old", store.active.BuildKey)
	}
	if store.activated != 0 {
		t.Fatalf("ActivateBuild calls = %d, want 0", store.activated)
	}
}

func TestBuildWatcherSwitchesActiveBuildAfterPrepareSucceeds(t *testing.T) {
	oldBuild := BuildMetadata{Region: "cn", Product: "wow", BuildKey: "old", BuildName: "12.0.0.1", Active: true, Ready: true}
	newBuild := BuildMetadata{Region: "cn", Product: "wow", BuildKey: "new", BuildName: "12.0.0.2"}
	store := &fakeBuildWatcherStore{
		active:     oldBuild,
		discovered: []BuildMetadata{newBuild},
	}
	var prepared []string
	watcher := NewBuildWatcher(store, func(_ context.Context, build BuildMetadata) error {
		prepared = append(prepared, build.BuildKey)
		return nil
	})

	if err := watcher.CheckOnce(context.Background(), "cn", "wow"); err != nil {
		t.Fatalf("CheckOnce: %v", err)
	}

	if len(prepared) != 1 || prepared[0] != "new" {
		t.Fatalf("prepared builds = %#v, want [new]", prepared)
	}
	if store.active.BuildKey != "new" {
		t.Fatalf("active build = %q, want new", store.active.BuildKey)
	}
	if store.active.Ready != true {
		t.Fatalf("active ready = false, want true")
	}
}

func TestMetadataBuildStoreSupportsBuildWatcherSwitch(t *testing.T) {
	db, err := metadata.Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := metadata.UpsertBuild(db, metadata.Build{Region: "cn", Product: "wow", BuildKey: "old", BuildName: "12.0.0.1", Active: true, Ready: true}); err != nil {
		t.Fatalf("upsert old: %v", err)
	}
	if err := metadata.UpsertBuild(db, metadata.Build{Region: "cn", Product: "wow", BuildKey: "new", BuildName: "12.0.0.2"}); err != nil {
		t.Fatalf("upsert new: %v", err)
	}
	store := NewMetadataBuildStore(db)
	watcher := NewBuildWatcher(store, func(context.Context, BuildMetadata) error {
		return nil
	})

	if err := watcher.CheckOnce(context.Background(), "cn", "wow"); err != nil {
		t.Fatalf("CheckOnce: %v", err)
	}

	active, err := metadata.ActiveBuild(db, "cn", "wow")
	if err != nil {
		t.Fatalf("active build: %v", err)
	}
	if active.BuildKey != "new" {
		t.Fatalf("active build = %q, want new", active.BuildKey)
	}
}

type fakeBuildWatcherStore struct {
	active     BuildMetadata
	discovered []BuildMetadata
	activated  int
}

func (f *fakeBuildWatcherStore) ActiveBuild(_ context.Context, region, product string) (BuildMetadata, error) {
	if f.active.Region == region && f.active.Product == product {
		return f.active, nil
	}
	return BuildMetadata{}, nil
}

func (f *fakeBuildWatcherStore) DiscoverBuilds(_ context.Context, region, product string) ([]BuildMetadata, error) {
	builds := make([]BuildMetadata, 0, len(f.discovered))
	for _, build := range f.discovered {
		if build.Region == region && build.Product == product {
			builds = append(builds, build)
		}
	}
	return builds, nil
}

func (f *fakeBuildWatcherStore) ActivateBuild(_ context.Context, build BuildMetadata) error {
	f.activated++
	build.Active = true
	build.Ready = true
	f.active = build
	return nil
}
