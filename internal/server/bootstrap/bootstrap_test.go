package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wowdata/internal/server/config"
	"wowdata/internal/server/health"
	"wowdata/internal/server/storage/metadata"
)

func TestPrepareActivatesBuildOnlyAfterEveryRequiredTableMaterializes(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item", "Spell"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-2", BuildName: "Build 2"},
	}}
	materializer := &fakeMaterializer{
		db: db,
		during: func(table string) {
			snapshot := healthSnapshot(t, ctx, db, cfg)
			if snapshot.Contexts[0].State != health.StatePreparing {
				t.Fatalf("health during %s = %s, want preparing before activation", table, snapshot.Contexts[0].State)
			}
		},
	}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	active := activeBuild(t, ctx, db, "us", "wow", "enUS")
	if active.Key.BuildKey != "build-2" || active.State != metadata.StateValid {
		t.Fatalf("active build = %#v, want build-2 valid", active)
	}
	snapshot := healthSnapshot(t, ctx, db, cfg)
	if !snapshot.Readiness.OK || snapshot.Contexts[0].State != health.StateReady {
		t.Fatalf("health after prepare = %#v, want ready", snapshot)
	}
	if snapshot.Contexts[0].PrepareCurrent != 2 || snapshot.Contexts[0].PrepareTotal != 2 {
		t.Fatalf("prepare progress = %d/%d, want 2/2", snapshot.Contexts[0].PrepareCurrent, snapshot.Contexts[0].PrepareTotal)
	}
	if got := strings.Join(materializer.tables, ","); got != "Item,Spell" {
		t.Fatalf("materialized tables = %q, want Item,Spell", got)
	}
}

func TestPrepareFailureMarksCandidateFailedAndPreservesOldActiveBuild(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item", "Spell"})
	seedActiveBuildWithTables(t, ctx, db, metadata.BuildKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "old-build",
	}, []string{"Item", "Spell"})
	fail := errors.New("Spell decode failed")
	materializer := &fakeMaterializer{db: db, failTable: "Spell", err: fail}
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "new-build", BuildName: "New Build"},
	}}

	err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx)
	if !errors.Is(err, fail) {
		t.Fatalf("Prepare error = %v, want %v", err, fail)
	}

	active := activeBuild(t, ctx, db, "us", "wow", "enUS")
	if active.Key.BuildKey != "old-build" || active.State != metadata.StateValid {
		t.Fatalf("active build = %#v, want old-build preserved", active)
	}
	candidate := buildByKey(t, ctx, db, metadata.BuildKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "new-build",
	})
	if candidate.State != metadata.StateFailed || !strings.Contains(candidate.Error, "Spell decode failed") {
		t.Fatalf("candidate = %#v, want failed with decode error", candidate)
	}
	snapshot := healthSnapshot(t, ctx, db, cfg)
	if !snapshot.Readiness.OK || snapshot.Contexts[0].ActiveBuild != "old-build" || snapshot.Contexts[0].State != health.StateReady {
		t.Fatalf("health = %#v, want old active still ready", snapshot)
	}
}

func TestPrepareFailureForSameActiveBuildKeepsExistingBuildValidAndReady(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item", "Spell"})
	seedActiveBuildWithTables(t, ctx, db, metadata.BuildKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1",
	}, []string{"Item", "Spell"})
	fail := errors.New("Spell transient decode failed")
	materializer := &fakeMaterializer{db: db, failTable: "Spell", err: fail}
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-1", BuildName: "Build 1"},
	}}

	err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx)
	if !errors.Is(err, fail) {
		t.Fatalf("Prepare error = %v, want %v", err, fail)
	}

	active := activeBuild(t, ctx, db, "us", "wow", "enUS")
	if active.Key.BuildKey != "build-1" || active.State != metadata.StateValid {
		t.Fatalf("active build = %#v, want build-1 still valid", active)
	}
	snapshot := healthSnapshot(t, ctx, db, cfg)
	if !snapshot.Readiness.OK || snapshot.Contexts[0].State != health.StateReady || snapshot.Contexts[0].PrepareCurrent != 2 {
		t.Fatalf("health = %#v, want old active build still ready with 2/2 tables", snapshot)
	}
}

func TestPrepareNoBuildDoesNotMarkTargetReady(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {NoBuild: true, Error: "product is not published in region"},
	}}
	materializer := &fakeMaterializer{db: db}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if materializer.calls != 0 {
		t.Fatalf("materialize calls = %d, want 0 for no-build", materializer.calls)
	}
	if _, err := metadata.ActiveBuild(ctx, db, "us", "wow", "enUS"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("active build error = %v, want sql.ErrNoRows", err)
	}
	snapshot := healthSnapshot(t, ctx, db, cfg)
	if snapshot.Readiness.OK || snapshot.Contexts[0].State != health.StateFailed {
		t.Fatalf("health = %#v, want failed without active build after no-build", snapshot)
	}
	if snapshot.Contexts[0].Error != "product is not published in region" {
		t.Fatalf("health error = %q, want no-build error", snapshot.Contexts[0].Error)
	}
}

func TestPrepareRetriesTransientDiscoverTimeoutAndActivatesBuild(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	timeout := context.DeadlineExceeded
	discoverer := &scriptedDiscoverer{results: []discoverResult{
		{err: timeout},
		{err: timeout},
		{build: DiscoveredBuild{BuildKey: "build-3", BuildName: "Build 3"}},
	}}
	sleeper := &fakeRetrySleeper{}
	materializer := &fakeMaterializer{db: db}

	err := (Runner{
		Config:       cfg,
		DB:           db,
		Discoverer:   discoverer,
		Materializer: materializer,
		RetrySleeper: sleeper.Sleep,
	}).Prepare(ctx)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if discoverer.calls != 3 {
		t.Fatalf("discover calls = %d, want 3", discoverer.calls)
	}
	if len(sleeper.delays) != 2 {
		t.Fatalf("retry sleeps = %d, want 2", len(sleeper.delays))
	}
	active := activeBuild(t, ctx, db, "us", "wow", "enUS")
	if active.Key.BuildKey != "build-3" || active.State != metadata.StateValid {
		t.Fatalf("active build = %#v, want build-3 valid", active)
	}
	if materializer.calls != 1 {
		t.Fatalf("materialize calls = %d, want 1", materializer.calls)
	}
}

func TestPrepareDoesNotRetryNoBuildResult(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	discoverer := &scriptedDiscoverer{results: []discoverResult{
		{build: DiscoveredBuild{NoBuild: true, Error: "no build published"}},
	}}
	sleeper := &fakeRetrySleeper{}
	materializer := &fakeMaterializer{db: db}

	err := (Runner{
		Config:       cfg,
		DB:           db,
		Discoverer:   discoverer,
		Materializer: materializer,
		RetrySleeper: sleeper.Sleep,
	}).Prepare(ctx)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if discoverer.calls != 1 {
		t.Fatalf("discover calls = %d, want 1", discoverer.calls)
	}
	if len(sleeper.delays) != 0 {
		t.Fatalf("retry sleeps = %d, want 0", len(sleeper.delays))
	}
	if materializer.calls != 0 {
		t.Fatalf("materialize calls = %d, want 0 for no-build", materializer.calls)
	}
}

func TestPrepareStopsDiscoverRetryWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	timeout := context.DeadlineExceeded
	discoverer := &scriptedDiscoverer{results: []discoverResult{
		{err: timeout},
		{err: timeout},
		{build: DiscoveredBuild{BuildKey: "build-after-cancel", BuildName: "Build After Cancel"}},
	}}
	sleeper := &fakeRetrySleeper{onSleep: cancel}
	materializer := &fakeMaterializer{db: db}

	err := (Runner{
		Config:       cfg,
		DB:           db,
		Discoverer:   discoverer,
		Materializer: materializer,
		RetrySleeper: sleeper.Sleep,
	}).Prepare(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Prepare error = %v, want context.Canceled", err)
	}
	if discoverer.calls != 1 {
		t.Fatalf("discover calls = %d, want 1 after cancellation", discoverer.calls)
	}
	if len(sleeper.delays) != 1 {
		t.Fatalf("retry sleeps = %d, want 1", len(sleeper.delays))
	}
	if materializer.calls != 0 {
		t.Fatalf("materialize calls = %d, want 0 after cancellation", materializer.calls)
	}
}

type fakeDiscoverer struct {
	builds map[string]DiscoveredBuild
}

func (d fakeDiscoverer) DiscoverBuild(_ context.Context, target config.PrepareTarget) (DiscoveredBuild, error) {
	key := target.Region + "/" + target.Product + "/" + target.Locale
	build, ok := d.builds[key]
	if !ok {
		return DiscoveredBuild{}, errors.New("unexpected discover target " + key)
	}
	return build, nil
}

type discoverResult struct {
	build DiscoveredBuild
	err   error
}

type scriptedDiscoverer struct {
	results []discoverResult
	calls   int
}

func (d *scriptedDiscoverer) DiscoverBuild(_ context.Context, _ config.PrepareTarget) (DiscoveredBuild, error) {
	if d.calls >= len(d.results) {
		return DiscoveredBuild{}, errors.New("unexpected extra discover call")
	}
	result := d.results[d.calls]
	d.calls++
	return result.build, result.err
}

type fakeRetrySleeper struct {
	delays  []time.Duration
	onSleep func()
}

func (s *fakeRetrySleeper) Sleep(ctx context.Context, delay time.Duration) error {
	s.delays = append(s.delays, delay)
	if s.onSleep != nil {
		s.onSleep()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

type fakeMaterializer struct {
	db        *sql.DB
	failTable string
	err       error
	tables    []string
	calls     int
	during    func(string)
}

func (m *fakeMaterializer) MaterializeTable(ctx context.Context, target config.PrepareTarget, build DiscoveredBuild, tableName string) error {
	m.calls++
	m.tables = append(m.tables, tableName)
	if m.during != nil {
		m.during(tableName)
	}
	if tableName == m.failTable {
		return m.err
	}
	return metadata.UpsertMaterializedTable(ctx, m.db, metadata.MaterializedTable{
		Key: metadata.TableKey{
			Region:    target.Region,
			Product:   target.Product,
			Locale:    target.Locale,
			BuildKey:  build.BuildKey,
			TableName: tableName,
		},
		DB2FileDataID:       100 + m.calls,
		DBDHash:             "dbd-" + tableName,
		DecoderVersion:      "fake-decoder",
		MaterializerVersion: "fake-materializer",
		ParquetPath:         filepath.ToSlash(filepath.Join("cache", "db2", tableName+".parquet")),
		RowCount:            1,
		State:               metadata.StateValid,
	})
}

func testConfig(metadataPath string, defaultTables []string) config.Config {
	return config.Config{
		Cache: config.CacheConfig{MetadataDB: metadataPath},
		Prepare: config.PrepareConfig{
			Targets: []config.PrepareTarget{{
				Label: "US Retail", Region: "us", Product: "wow", Locale: "enUS",
			}},
			DefaultTables: defaultTables,
		},
	}
}

func healthSnapshot(t *testing.T, ctx context.Context, db *sql.DB, cfg config.Config) health.Snapshot {
	t.Helper()
	provider := health.NewMetadataProviderWithDB(cfg, cfg.Cache.MetadataDB, db)
	snapshot, err := provider.HealthSnapshot(ctx)
	if err != nil {
		t.Fatalf("HealthSnapshot: %v", err)
	}
	return snapshot
}

func openMetadataDBAt(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := metadata.Open(path)
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedActiveBuildWithTables(t *testing.T, ctx context.Context, db *sql.DB, key metadata.BuildKey, tables []string) {
	t.Helper()
	if err := metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{Key: key, BuildName: key.BuildKey, State: metadata.StateValid}); err != nil {
		t.Fatalf("upsert old build: %v", err)
	}
	if err := metadata.ActivateBuild(ctx, db, key); err != nil {
		t.Fatalf("activate old build: %v", err)
	}
	for index, tableName := range tables {
		if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
			Key: metadata.TableKey{
				Region: key.Region, Product: key.Product, Locale: key.Locale, BuildKey: key.BuildKey, TableName: tableName,
			},
			DB2FileDataID:       index + 1,
			DBDHash:             "old-dbd-" + tableName,
			DecoderVersion:      "fake-decoder",
			MaterializerVersion: "fake-materializer",
			ParquetPath:         filepath.ToSlash(filepath.Join("cache", "db2", tableName+".parquet")),
			RowCount:            1,
			State:               metadata.StateValid,
		}); err != nil {
			t.Fatalf("upsert old table %s: %v", tableName, err)
		}
	}
}

func activeBuild(t *testing.T, ctx context.Context, db *sql.DB, region, product, locale string) metadata.Build {
	t.Helper()
	build, err := metadata.ActiveBuild(ctx, db, region, product, locale)
	if err != nil {
		t.Fatalf("active build: %v", err)
	}
	return build
}

func buildByKey(t *testing.T, ctx context.Context, db *sql.DB, key metadata.BuildKey) metadata.Build {
	t.Helper()
	var build metadata.Build
	var active int
	err := db.QueryRowContext(ctx, `
SELECT region, product, locale, build_key, build_name, state, active, error
FROM server_builds
WHERE region = ? AND product = ? AND locale = ? AND build_key = ?`,
		key.Region, key.Product, key.Locale, key.BuildKey,
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
	if err != nil {
		t.Fatalf("query build: %v", err)
	}
	build.Active = active != 0
	return build
}
