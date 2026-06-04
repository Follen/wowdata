package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cacheparquet "wowdata/internal/cache/parquet"
	"wowdata/internal/server/config"
	"wowdata/internal/server/health"
	"wowdata/internal/server/storage/cascindex"
	serverlistfile "wowdata/internal/server/storage/listfile"
	"wowdata/internal/server/storage/metadata"
	"wowdata/internal/shared/casc"
	"wowdata/internal/shared/db2"
)

func TestManifestUnreadableTableErrorsIncludeMissingRootFileDataID(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("fileDataID does not exist in root: 1720145"),
		fmt.Errorf("no root entry found for locale: 2"),
	} {
		if !isManifestUnreadableTableError(err) {
			t.Fatalf("isManifestUnreadableTableError(%v) = false, want true", err)
		}
	}
}

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

func TestPrepareEmptyDefaultTablesMaterializesManifestTableSet(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"*"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-2", BuildName: "Build 2"},
	}}
	materializer := &fakeMaterializer{
		db:              db,
		availableTables: []string{"Achievement", "Item", "Spell"},
	}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := strings.Join(materializer.tables, ","); got != "Achievement,Item,Spell" {
		t.Fatalf("materialized tables = %q, want manifest table set", got)
	}
	snapshot := healthSnapshot(t, ctx, db, cfg)
	if snapshot.Contexts[0].PrepareCurrent != 3 || snapshot.Contexts[0].PrepareTotal != 3 {
		t.Fatalf("prepare progress = %d/%d, want 3/3 manifest tables", snapshot.Contexts[0].PrepareCurrent, snapshot.Contexts[0].PrepareTotal)
	}
}

func TestPrepareManifestDefaultTablesSkipsTablesWithoutBuildStructure(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"*"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-2", BuildName: "12.0.5.67823"},
	}}
	materializer := &fakeMaterializer{
		db:              db,
		availableTables: []string{"Achievement", "ModelSound", "Spell"},
		failTable:       "ModelSound",
		err:             errors.New("no DBD structure for build 12.0.5.67823"),
	}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := strings.Join(materializer.tables, ","); got != "Achievement,ModelSound,Spell" {
		t.Fatalf("attempted tables = %q, want manifest order including unreadable table", got)
	}
	active := activeBuild(t, ctx, db, "us", "wow", "enUS")
	if active.Key.BuildKey != "build-2" || active.State != metadata.StateValid {
		t.Fatalf("active build = %#v, want build-2 valid after readable tables materialize", active)
	}
	tables, err := metadata.ListValidMaterializedTables(ctx, db, metadata.TableCatalogLookup{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2",
	})
	if err != nil {
		t.Fatalf("list materialized tables: %v", err)
	}
	if len(tables) != 2 || tables[0].Key.TableName != "Achievement" || tables[1].Key.TableName != "Spell" {
		t.Fatalf("valid tables = %#v, want only readable Achievement and Spell", tables)
	}
	snapshot := healthSnapshot(t, ctx, db, cfg)
	if !snapshot.Readiness.OK || snapshot.Contexts[0].PrepareCurrent != 2 || snapshot.Contexts[0].PrepareTotal != 2 {
		t.Fatalf("health after prepare = %#v, want ready with readable table denominator 2/2", snapshot)
	}
}

func TestPrepareManifestDefaultTablesDoesNotRetryKnownUnreadableTablesForReadyActiveBuild(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"*"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-2", BuildName: "12.0.5.67823"},
	}}
	firstMaterializer := &fakeMaterializer{
		db:              db,
		availableTables: []string{"Achievement", "ModelSound", "Spell"},
		failTable:       "ModelSound",
		err:             errors.New("no DBD structure for build 12.0.5.67823"),
	}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: firstMaterializer}).Prepare(ctx); err != nil {
		t.Fatalf("first Prepare: %v", err)
	}

	secondMaterializer := &fakeMaterializer{
		db:              db,
		availableTables: []string{"Achievement", "ModelSound", "Spell"},
		failTable:       "ModelSound",
		err:             errors.New("no DBD structure for build 12.0.5.67823"),
	}
	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: secondMaterializer}).Prepare(ctx); err != nil {
		t.Fatalf("second Prepare: %v", err)
	}

	if secondMaterializer.calls != 0 {
		t.Fatalf("second prepare materialized tables = %q (%d calls), want reuse with no unreadable retry", strings.Join(secondMaterializer.tables, ","), secondMaterializer.calls)
	}
}

func TestPrepareConfiguredTableFailsEvenAfterWildcardMarkedTableUnreadable(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	wildcardCfg := testConfig(metadataPath, []string{"*"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-2", BuildName: "12.0.5.67823"},
	}}
	firstMaterializer := &fakeMaterializer{
		db:              db,
		availableTables: []string{"Achievement", "ModelSound", "Spell"},
		failTable:       "ModelSound",
		err:             errors.New("no DBD structure for build 12.0.5.67823"),
	}
	if err := (Runner{Config: wildcardCfg, DB: db, Discoverer: discoverer, Materializer: firstMaterializer}).Prepare(ctx); err != nil {
		t.Fatalf("wildcard Prepare: %v", err)
	}

	explicitCfg := testConfig(metadataPath, []string{"ModelSound"})
	secondMaterializer := &fakeMaterializer{
		db:        db,
		failTable: "ModelSound",
		err:       errors.New("no DBD structure for build 12.0.5.67823"),
	}
	err := (Runner{Config: explicitCfg, DB: db, Discoverer: discoverer, Materializer: secondMaterializer}).Prepare(ctx)
	if err == nil || !strings.Contains(err.Error(), "no DBD structure for build 12.0.5.67823") {
		t.Fatalf("explicit Prepare error = %v, want unreadable table failure", err)
	}
	if secondMaterializer.calls != 1 || strings.Join(secondMaterializer.tables, ",") != "ModelSound" {
		t.Fatalf("explicit materialized tables = %q (%d calls), want ModelSound attempted", strings.Join(secondMaterializer.tables, ","), secondMaterializer.calls)
	}
}

func TestPrepareReleasesMemoryAfterEachTableAttempt(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"*"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-2", BuildName: "12.0.5.67823"},
	}}
	materializer := &fakeMaterializer{
		db:              db,
		availableTables: []string{"Achievement", "ModelSound", "Spell"},
		failTable:       "ModelSound",
		err:             errors.New("no DBD structure for build 12.0.5.67823"),
	}
	var releases int32

	if err := (Runner{
		Config:                  cfg,
		DB:                      db,
		Discoverer:              discoverer,
		Materializer:            materializer,
		AfterTableMemoryRelease: func() { atomic.AddInt32(&releases, 1) },
	}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if got := atomic.LoadInt32(&releases); got != 3 {
		t.Fatalf("memory releases = %d, want one per table attempt", got)
	}
}

func TestPreparePersistsResourceIndexesForAssets(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {
			BuildKey:  "build-2",
			BuildName: "12.0.5.67823",
			PrepareResources: func(context.Context) (DiscoveredBuild, error) {
				return DiscoveredBuild{
					BuildKey:  "build-2",
					BuildName: "12.0.5.67823",
					ResourceIndexes: ResourceIndexes{
						ListfileSourceHash: "listfile-hash",
						ListfileEntries: []serverlistfile.Entry{
							{FileDataID: 321, Path: "Interface/Icons/Server_Test.blp"},
						},
						CASCSource:        cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2"},
						CASCSourceVersion: "casc-version",
						CASCRoots: []cascindex.RootMapping{
							{FileDataID: 321, ContentKey: "content-key"},
						},
						CASCEncodings: []cascindex.EncodingMapping{
							{ContentKey: "content-key", EncodingKey: "encoding-key", Size: 12},
						},
						CASCArchives: []cascindex.ArchiveMapping{
							{EncodingKey: "encoding-key", ArchiveKey: "archive-key", Offset: 34, Size: 12},
						},
					},
				}, nil
			},
		},
	}}
	materializer := &fakeMaterializer{
		db:              db,
		availableTables: []string{"Item"},
	}

	if err := (Runner{
		Config:       cfg,
		DB:           db,
		Discoverer:   discoverer,
		Materializer: materializer,
	}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	entry, err := serverlistfile.LookupByFileDataID(ctx, db, 321)
	if err != nil {
		t.Fatalf("listfile lookup: %v", err)
	}
	if entry.Path != "interface/icons/server_test.blp" {
		t.Fatalf("listfile path = %q, want normalized asset path", entry.Path)
	}
	span, err := cascindex.ResolveFileDataIDForSource(ctx, db, cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2"}, 321)
	if err != nil {
		t.Fatalf("casc resolve: %v", err)
	}
	if span.EncodingKey != "encoding-key" || span.ArchiveKey != "archive-key" {
		t.Fatalf("casc span = %#v, want persisted encoding/archive mapping", span)
	}
}

func TestPrepareClearsOldCASCIndexWhenNewResourceIndexIsEmpty(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	source := cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2"}
	if err := cascindex.ReplaceIndexForSource(ctx, db, source, "old-casc-version",
		[]cascindex.RootMapping{{FileDataID: 555, ContentKey: "old-content-key"}},
		[]cascindex.EncodingMapping{{ContentKey: "old-content-key", EncodingKey: "old-encoding-key", Size: 12}},
		[]cascindex.ArchiveMapping{{EncodingKey: "old-encoding-key", ArchiveKey: "old-archive-key", Offset: 34, Size: 12}},
	); err != nil {
		t.Fatalf("seed old casc index: %v", err)
	}
	cfg := testConfig(metadataPath, []string{"Item"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {
			BuildKey:  "build-2",
			BuildName: "12.0.5.67823",
			PrepareResources: func(context.Context) (DiscoveredBuild, error) {
				return DiscoveredBuild{
					BuildKey:  "build-2",
					BuildName: "12.0.5.67823",
					ResourceIndexes: ResourceIndexes{
						CASCSource:        source,
						CASCSourceVersion: "empty-casc-version",
					},
				}, nil
			},
		},
	}}

	if err := (Runner{
		Config:       cfg,
		DB:           db,
		Discoverer:   discoverer,
		Materializer: &fakeMaterializer{db: db, availableTables: []string{"Item"}},
	}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	ready, err := cascindex.HasUsableIndexForSource(ctx, db, source)
	if err != nil {
		t.Fatalf("usable casc index: %v", err)
	}
	if ready {
		t.Fatal("usable casc index = true, want false after empty replacement")
	}
	if _, err := cascindex.ResolveFileDataIDForSource(ctx, db, source, 555); !errors.Is(err, cascindex.ErrNotFound) {
		t.Fatalf("old casc resolve error = %v, want ErrNotFound", err)
	}
}

func TestPrepareSkipsUnchangedListfileIndex(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	if err := serverlistfile.ReplaceSource(ctx, db, "same-listfile-hash", []serverlistfile.Entry{
		{FileDataID: 321, Path: "Interface/Icons/Existing.blp"},
	}); err != nil {
		t.Fatalf("seed listfile: %v", err)
	}
	cfg := testConfig(metadataPath, []string{"Item"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {
			BuildKey:  "build-2",
			BuildName: "12.0.5.67823",
			PrepareResources: func(context.Context) (DiscoveredBuild, error) {
				return DiscoveredBuild{
					BuildKey:  "build-2",
					BuildName: "12.0.5.67823",
					ResourceIndexes: ResourceIndexes{
						ListfileSourceHash: "same-listfile-hash",
						ListfileEntries: []serverlistfile.Entry{
							{FileDataID: 999, Path: "Interface/Icons/Should_Not_Rewrite.blp"},
						},
					},
				}, nil
			},
		},
	}}

	if err := (Runner{
		Config:       cfg,
		DB:           db,
		Discoverer:   discoverer,
		Materializer: &fakeMaterializer{db: db, availableTables: []string{"Item"}},
	}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	entry, err := serverlistfile.LookupByFileDataID(ctx, db, 321)
	if err != nil {
		t.Fatalf("existing listfile lookup: %v", err)
	}
	if entry.Path != "interface/icons/existing.blp" {
		t.Fatalf("existing listfile path = %q, want unchanged entry", entry.Path)
	}
	if _, err := serverlistfile.LookupByFileDataID(ctx, db, 999); err == nil {
		t.Fatal("unchanged listfile hash rewrote entries")
	}
}

func TestPreparePersistsResourceIndexesWhenDB2TablesAlreadyValid(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	seedActiveBuildWithTables(t, ctx, db, metadata.BuildKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2",
	}, []string{"Item"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {
			BuildKey:  "build-2",
			BuildName: "12.0.5.67823",
			PrepareResources: func(context.Context) (DiscoveredBuild, error) {
				return DiscoveredBuild{
					BuildKey:  "build-2",
					BuildName: "12.0.5.67823",
					ResourceIndexes: ResourceIndexes{
						ListfileSourceHash: "listfile-hash",
						ListfileEntries: []serverlistfile.Entry{
							{FileDataID: 555, Path: "Interface/Icons/Ready.blp"},
						},
						CASCSource:        cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2"},
						CASCSourceVersion: "casc-version",
						CASCRoots: []cascindex.RootMapping{
							{FileDataID: 555, ContentKey: "content-key"},
						},
						CASCEncodings: []cascindex.EncodingMapping{
							{ContentKey: "content-key", EncodingKey: "encoding-key", Size: 12},
						},
						CASCArchives: []cascindex.ArchiveMapping{
							{EncodingKey: "encoding-key", ArchiveKey: "archive-key", Offset: 34, Size: 12},
						},
					},
				}, nil
			},
		},
	}}

	if err := (Runner{
		Config:       cfg,
		DB:           db,
		Discoverer:   discoverer,
		Materializer: &fakeMaterializer{db: db, availableTables: []string{"Item"}},
	}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	span, err := cascindex.ResolveFileDataIDForSource(ctx, db, cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2"}, 555)
	if err != nil {
		t.Fatalf("casc resolve after DB2 reuse: %v", err)
	}
	if span.EncodingKey != "encoding-key" {
		t.Fatalf("span = %#v, want persisted resource index without DB2 rematerialization", span)
	}
}

func TestPrepareSkipsResourcePreparationWhenDB2AndAssetIndexesAlreadyValid(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2"}
	seedActiveBuildWithTables(t, ctx, db, key, []string{"Item"})
	if err := serverlistfile.ReplaceSource(ctx, db, "listfile-hash", []serverlistfile.Entry{
		{FileDataID: 555, Path: "Interface/Icons/Ready.blp"},
	}); err != nil {
		t.Fatalf("seed listfile: %v", err)
	}
	source := cascindex.SourceKey{Region: key.Region, Product: key.Product, Locale: key.Locale, BuildKey: key.BuildKey}
	if err := cascindex.ReplaceIndexForSource(ctx, db, source, "casc-version",
		[]cascindex.RootMapping{{FileDataID: 555, ContentKey: "content-key"}},
		[]cascindex.EncodingMapping{{ContentKey: "content-key", EncodingKey: "encoding-key", Size: 12}},
		[]cascindex.ArchiveMapping{{EncodingKey: "encoding-key", ArchiveKey: "archive-key", Offset: 34, Size: 12}},
	); err != nil {
		t.Fatalf("seed casc index: %v", err)
	}
	var prepareResourcesCalled bool
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {
			BuildKey:  "build-2",
			BuildName: "12.0.5.67823",
			PrepareResources: func(context.Context) (DiscoveredBuild, error) {
				prepareResourcesCalled = true
				return DiscoveredBuild{BuildKey: "build-2", BuildName: "12.0.5.67823"}, nil
			},
		},
	}}

	if err := (Runner{
		Config:       cfg,
		DB:           db,
		Discoverer:   discoverer,
		Materializer: &fakeMaterializer{db: db, availableTables: []string{"Item"}},
	}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if prepareResourcesCalled {
		t.Fatal("PrepareResources was called even though DB2 and asset indexes were already valid")
	}
}

func TestPrepareMissingTableForReadyActiveBuildUsesTableReaderWhenAssetIndexesReady(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item", "Spell"})
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2"}
	seedActiveBuildWithTables(t, ctx, db, key, []string{"Item"})
	if err := serverlistfile.ReplaceSource(ctx, db, "listfile-hash", []serverlistfile.Entry{
		{FileDataID: 555, Path: "Interface/Icons/Ready.blp"},
	}); err != nil {
		t.Fatalf("seed listfile: %v", err)
	}
	source := cascindex.SourceKey{Region: key.Region, Product: key.Product, Locale: key.Locale, BuildKey: key.BuildKey}
	if err := cascindex.ReplaceIndexForSource(ctx, db, source, "casc-version",
		[]cascindex.RootMapping{{FileDataID: 555, ContentKey: "content-key"}},
		[]cascindex.EncodingMapping{{ContentKey: "content-key", EncodingKey: "encoding-key", Size: 12}},
		[]cascindex.ArchiveMapping{{EncodingKey: "encoding-key", ArchiveKey: "archive-key", Offset: 34, Size: 12}},
	); err != nil {
		t.Fatalf("seed casc index: %v", err)
	}
	var prepareTableReaderCalled bool
	var prepareResourcesCalled bool
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {
			BuildKey:  "build-2",
			BuildName: "12.0.5.67823",
			PrepareTableReader: func(context.Context) (DiscoveredBuild, error) {
				prepareTableReaderCalled = true
				return DiscoveredBuild{BuildKey: "build-2", BuildName: "12.0.5.67823"}, nil
			},
			PrepareResources: func(context.Context) (DiscoveredBuild, error) {
				prepareResourcesCalled = true
				return DiscoveredBuild{BuildKey: "build-2", BuildName: "12.0.5.67823"}, nil
			},
		},
	}}
	materializer := &fakeMaterializer{db: db}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if !prepareTableReaderCalled {
		t.Fatal("PrepareTableReader was not called for missing DB2 table on ready active build")
	}
	if prepareResourcesCalled {
		t.Fatal("PrepareResources was called even though asset indexes were already valid")
	}
	if materializer.calls != 1 || strings.Join(materializer.tables, ",") != "Spell" {
		t.Fatalf("materialized tables = %q (%d calls), want only missing Spell", strings.Join(materializer.tables, ","), materializer.calls)
	}
}

func TestPrepareFailsWhenDB2ReadyResourceBackfillLeavesAssetIndexesMissing(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2"}
	seedActiveBuildWithTables(t, ctx, db, key, []string{"Item"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {
			BuildKey:  "build-2",
			BuildName: "12.0.5.67823",
			PrepareResources: func(context.Context) (DiscoveredBuild, error) {
				return DiscoveredBuild{BuildKey: "build-2", BuildName: "12.0.5.67823"}, nil
			},
		},
	}}

	err := (Runner{
		Config:       cfg,
		DB:           db,
		Discoverer:   discoverer,
		Materializer: &fakeMaterializer{db: db, availableTables: []string{"Item"}},
	}).Prepare(ctx)
	if err == nil {
		t.Fatal("Prepare succeeded after resource backfill left asset indexes missing")
	}
	if !strings.Contains(err.Error(), "resource indexes missing after preparation") {
		t.Fatalf("Prepare error = %v, want missing resource index postcondition", err)
	}
}

func TestPrepareManifestDefaultTablesSkipsInvalidDBDDefinitions(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"*"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-2", BuildName: "12.0.5.67823"},
	}}
	materializer := &fakeMaterializer{
		db:              db,
		availableTables: []string{"GarrMission", "GarrMissionReward", "GarrMechanic"},
		failTable:       "GarrMissionReward",
		err:             errors.New("Invalid DBD: Missing column definitions."),
	}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	tables, err := metadata.ListValidMaterializedTables(ctx, db, metadata.TableCatalogLookup{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2",
	})
	if err != nil {
		t.Fatalf("list materialized tables: %v", err)
	}
	if len(tables) != 2 || tables[0].Key.TableName != "GarrMechanic" || tables[1].Key.TableName != "GarrMission" {
		t.Fatalf("valid tables = %#v, want only readable GarrMechanic and GarrMission", tables)
	}
}

func TestPrepareConfiguredTableFailsOnInvalidDBDDefinition(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"GarrMissionReward"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-2", BuildName: "12.0.5.67823"},
	}}
	materializer := &fakeMaterializer{
		db:        db,
		failTable: "GarrMissionReward",
		err:       errors.New("Invalid DBD: Missing column definitions."),
	}

	err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx)
	if err == nil || !strings.Contains(err.Error(), "Invalid DBD") {
		t.Fatalf("Prepare error = %v, want explicit table invalid DBD failure", err)
	}
}

func TestHTTPDBDSourceRefreshesStaleCachedDefinition(t *testing.T) {
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cacheDir, "SpellOverrideName.dbd"), []byte("stale definition"), 0644); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SpellOverrideName.dbd" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("fresh definition"))
	}))
	t.Cleanup(server.Close)

	source := newHTTPDBDSource(cacheDir, []string{server.URL + "/%s.dbd"})
	got, err := source.Definition("SpellOverrideName")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if got != "fresh definition" {
		t.Fatalf("Definition = %q, want fresh definition", got)
	}
	cached, err := os.ReadFile(filepath.Join(cacheDir, "SpellOverrideName.dbd"))
	if err != nil {
		t.Fatalf("read refreshed cache: %v", err)
	}
	if string(cached) != "fresh definition" {
		t.Fatalf("cache = %q, want fresh definition", string(cached))
	}
}

func TestHTTPDBDSourceFallsBackToCachedDefinitionWhenRemoteFails(t *testing.T) {
	cacheDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cacheDir, "SpellOverrideName.dbd"), []byte("cached definition"), 0644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	source := newHTTPDBDSource(cacheDir, []string{server.URL + "/%s.dbd"})
	got, err := source.Definition("SpellOverrideName")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if got != "cached definition" {
		t.Fatalf("Definition = %q, want cached definition", got)
	}
}

func TestPrepareManifestDefaultTablesFailsWhenNoReadableTablesMaterialize(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"*"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-empty", BuildName: "12.0.5.67823"},
	}}
	materializer := &fakeMaterializer{db: db}

	err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx)
	if err == nil || !strings.Contains(err.Error(), "no readable DB2 tables materialized") {
		t.Fatalf("Prepare error = %v, want no readable tables failure", err)
	}
	candidate := buildByKey(t, ctx, db, metadata.BuildKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-empty",
	})
	if candidate.State != metadata.StateFailed || !strings.Contains(candidate.Error, "no readable DB2 tables materialized") {
		t.Fatalf("candidate = %#v, want failed with no readable tables message", candidate)
	}
}

func TestPrepareManifestDefaultTablesKeepsSameActiveBuildReadyWhenCatalogIsEmpty(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"*"})
	seedActiveBuildWithTables(t, ctx, db, metadata.BuildKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "ready-build",
	}, []string{"Item"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "ready-build", BuildName: "Ready Build"},
	}}
	materializer := &fakeMaterializer{db: db}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	active := activeBuild(t, ctx, db, "us", "wow", "enUS")
	if active.Key.BuildKey != "ready-build" || active.State != metadata.StateValid {
		t.Fatalf("active build = %#v, want ready-build preserved as valid", active)
	}
	if materializer.calls != 0 {
		t.Fatalf("materialize calls = %d, want 0 when empty catalog cannot identify missing tables", materializer.calls)
	}
}

func TestPrepareUsesConfiguredParallelTableMaterializations(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"A", "B", "C", "D"})
	cfg.Limits.MaxParallelContextPrepares = 1
	cfg.Limits.MaxParallelTableMaterializations = 2
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "build-2", BuildName: "Build 2"},
	}}
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	var active int32
	var maxActive int32
	materializer := &fakeMaterializer{
		db: db,
		before: func(config.PrepareTarget, string) {
			now := atomic.AddInt32(&active, 1)
			for {
				observed := atomic.LoadInt32(&maxActive)
				if now <= observed || atomic.CompareAndSwapInt32(&maxActive, observed, now) {
					break
				}
			}
			entered <- struct{}{}
			<-release
			atomic.AddInt32(&active, -1)
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx)
	}()

	if !eventually(1*time.Second, func() bool {
		return atomic.LoadInt32(&maxActive) == 2
	}) {
		close(release)
		t.Fatalf("max parallel table materializations = %d, want 2", atomic.LoadInt32(&maxActive))
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := len(entered); got != 4 {
		t.Fatalf("materialized table entries = %d, want 4", got)
	}
}

func TestPrepareLimitsParallelTableMaterializationsAcrossTargets(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"A", "B"})
	cfg.Prepare.Targets = append(cfg.Prepare.Targets, config.PrepareTarget{
		Label: "US PTR", Region: "us", Product: "wowt", Locale: "enUS",
	})
	cfg.Limits.MaxParallelContextPrepares = 2
	cfg.Limits.MaxParallelTableMaterializations = 2
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS":  {BuildKey: "build-2", BuildName: "Build 2"},
		"us/wowt/enUS": {BuildKey: "ptr-build-2", BuildName: "PTR Build 2"},
	}}
	release := make(chan struct{})
	var active int32
	var maxActive int32
	materializer := &fakeMaterializer{
		db: db,
		before: func(config.PrepareTarget, string) {
			now := atomic.AddInt32(&active, 1)
			for {
				observed := atomic.LoadInt32(&maxActive)
				if now <= observed || atomic.CompareAndSwapInt32(&maxActive, observed, now) {
					break
				}
			}
			<-release
			atomic.AddInt32(&active, -1)
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx)
	}()

	if eventually(1*time.Second, func() bool {
		return atomic.LoadInt32(&maxActive) > 2
	}) {
		close(release)
		_ = <-done
		t.Fatalf("max parallel table materializations across targets = %d, want <= 2", atomic.LoadInt32(&maxActive))
	}
	if atomic.LoadInt32(&maxActive) != 2 {
		close(release)
		_ = <-done
		t.Fatalf("max parallel table materializations across targets = %d, want 2", atomic.LoadInt32(&maxActive))
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Prepare: %v", err)
	}
}

func TestWDCRowSourceStreamsRowsAndEOF(t *testing.T) {
	reader, err := db2.NewWDCReaderFromBytes("TestTable", db2.BuildMinimalWDC2ForTest(), []db2.SchemaField{
		{Name: "ID", Type: db2.FieldUInt32},
		{Name: "Value", Type: db2.FieldUInt32},
	})
	if err != nil {
		t.Fatalf("NewWDCReaderFromBytes: %v", err)
	}
	source, err := newWDCRowSource(reader)
	if err != nil {
		t.Fatalf("newWDCRowSource: %v", err)
	}
	defer source.Close()

	first, ok, err := source.NextRow()
	if err != nil || !ok || first["ID"] != uint32(1) || first["Value"] != uint32(100) {
		t.Fatalf("first row = %#v ok=%v err=%v, want ID 1 and Value 100", first, ok, err)
	}
	second, ok, err := source.NextRow()
	if err != nil || !ok || second["ID"] != uint32(2) || second["Value"] != uint32(200) {
		t.Fatalf("second row = %#v ok=%v err=%v, want ID 2 and Value 200", second, ok, err)
	}
	_, ok, err = source.NextRow()
	if err != nil || ok {
		t.Fatalf("after rows ok=%v err=%v, want EOF", ok, err)
	}
}

func TestSchemaForTablePreservesDBDStringArrays(t *testing.T) {
	rawDBD := strings.Join([]string{
		"COLUMNS",
		"string ParamLabel",
		"int ParamTypeEnum",
		"",
		"BUILD 12.0.5.67823",
		"ParamLabel[6]",
		"ParamTypeEnum<u8>[6]",
		"",
	}, "\n")

	schema, err := schemaForTable(rawDBD, "12.0.5.67823")
	if err != nil {
		t.Fatalf("schemaForTable: %v", err)
	}
	if len(schema) != 3 {
		t.Fatalf("schema fields = %#v, want ID, ParamLabel and ParamTypeEnum", schema)
	}
	if schema[0] != (cacheparquet.Field{Name: "ID", Type: "dbFieldUInt32"}) {
		t.Fatalf("ID schema = %#v, want synthetic uint32 ID", schema[0])
	}
	if schema[1] != (cacheparquet.Field{Name: "ParamLabel", Type: "dbFieldString", ArrayLen: 6}) {
		t.Fatalf("ParamLabel schema = %#v, want dbFieldString array", schema[1])
	}
	if schema[2] != (cacheparquet.Field{Name: "ParamTypeEnum", Type: "dbFieldUInt8", ArrayLen: 6}) {
		t.Fatalf("ParamTypeEnum schema = %#v, want numeric array preserved", schema[2])
	}
}

func TestSchemaForTableUsesLayoutHashWhenBuildNameDoesNotMatch(t *testing.T) {
	rawDBD := strings.Join([]string{
		"COLUMNS",
		"int ID",
		"int Field_10_2_5_52206_000",
		"int Field_10_2_5_52206_001",
		"",
		"BUILD 10.2.5.52206",
		"LAYOUT 4E2D58C1",
		"$id,noninline$ID<u32>",
		"Field_10_2_5_52206_000<32>",
		"Field_10_2_5_52206_001<32>",
		"",
	}, "\n")

	schema, err := schemaForTable(rawDBD, "12.0.5.67823", "4E2D58C1")
	if err != nil {
		t.Fatalf("schemaForTable: %v", err)
	}
	if len(schema) != 3 {
		t.Fatalf("schema fields = %#v, want layout-hash entry fields", schema)
	}
	if schema[0] != (cacheparquet.Field{Name: "ID", Type: "dbFieldNonInlineID"}) {
		t.Fatalf("ID schema = %#v, want non-inline ID", schema[0])
	}
	if schema[1].Name != "Field_10_2_5_52206_000" || schema[2].Name != "Field_10_2_5_52206_001" {
		t.Fatalf("schema fields = %#v, want 10.2.5 layout fallback fields", schema)
	}
}

func TestDB2LayoutHashReadsReversedHeaderHash(t *testing.T) {
	data := make([]byte, 32)
	data[0] = 'W'
	data[1] = 'D'
	data[2] = 'C'
	data[3] = '2'
	copy(data[24:28], []byte{0xC1, 0x58, 0x2D, 0x4E})

	got, err := db2LayoutHash(data)
	if err != nil {
		t.Fatalf("db2LayoutHash: %v", err)
	}
	if got != "4E2D58C1" {
		t.Fatalf("layout hash = %q, want 4E2D58C1", got)
	}
}

func TestPrepareBootstrapsNewConfiguredTargetMissingFromMetadata(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	cfg.Prepare.Targets = append(cfg.Prepare.Targets, config.PrepareTarget{
		Label: "US Beta", Region: "us", Product: "wowxptr", Locale: "enUS",
	})
	seedActiveBuildWithTables(t, ctx, db, metadata.BuildKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "old-ready-build",
	}, []string{"Item"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS":     {BuildKey: "old-ready-build", BuildName: "Old Ready Build"},
		"us/wowxptr/enUS": {BuildKey: "new-beta-build", BuildName: "New Beta Build"},
	}}
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	betaStarted := make(chan struct{})
	materializer := &fakeMaterializer{
		db: db,
		before: func(target config.PrepareTarget, _ string) {
			if target.Product == "wowxptr" {
				select {
				case <-betaStarted:
				default:
					close(betaStarted)
				}
			}
			if target.Product != "wow" {
				return
			}
			select {
			case <-oldStarted:
			default:
				close(oldStarted)
			}
			<-releaseOld
		},
	}
	cfg.Limits.MaxParallelContextPrepares = 2

	done := make(chan error, 1)
	go func() {
		done <- (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx)
	}()
	defer func() {
		close(releaseOld)
		if err := <-done; err != nil {
			t.Fatalf("Prepare after releasing old target: %v", err)
		}
	}()

	var beta metadata.Build
	if !eventually(time.Second, func() bool {
		var err error
		beta, err = metadata.ActiveBuild(ctx, db, "us", "wowxptr", "enUS")
		return err == nil
	}) {
		t.Fatal("new configured beta target was not prepared while old ready target was blocked")
	}
	select {
	case <-betaStarted:
	default:
		t.Fatal("new configured beta target did not materialize through prepare")
	}
	if beta.Key.BuildKey != "new-beta-build" || beta.State != metadata.StateValid {
		t.Fatalf("beta active build = %#v, want new-beta-build valid", beta)
	}
	tables, err := metadata.ListValidMaterializedTables(ctx, db, metadata.TableCatalogLookup{
		Region: "us", Product: "wowxptr", Locale: "enUS", BuildKey: "new-beta-build",
	})
	if err != nil {
		t.Fatalf("list beta tables: %v", err)
	}
	if len(tables) != 1 || tables[0].Key.TableName != "Item" {
		t.Fatalf("beta tables = %#v, want Item materialized", tables)
	}
}

func TestPrepareDiscoversEveryConfiguredTargetBeforeMaterializing(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	cfg.Prepare.Targets = []config.PrepareTarget{
		{Label: "US Retail", Region: "us", Product: "wow", Locale: "enUS"},
		{Label: "US Beta", Region: "us", Product: "wowxptr", Locale: "enUS"},
	}
	cfg.Limits.MaxParallelContextPrepares = 1
	discoverer := &recordingDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS":     {BuildKey: "retail-build", BuildName: "Retail Build"},
		"us/wowxptr/enUS": {BuildKey: "beta-build", BuildName: "Beta Build"},
	}}
	materializer := &fakeMaterializer{
		db: db,
		before: func(_ config.PrepareTarget, _ string) {
			if got := discoverer.DiscoveredCount(); got != len(cfg.Prepare.Targets) {
				t.Fatalf("materialization started after %d/%d discoveries; want all targets discovered first", got, len(cfg.Prepare.Targets))
			}
		},
	}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if materializer.calls != 2 {
		t.Fatalf("materialize calls = %d, want 2", materializer.calls)
	}
}

func TestPrepareRecordsDiscoveredBuildBeforeMaterializing(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "discovered-build", BuildName: "Discovered Build"},
	}}
	materializer := &fakeMaterializer{
		db: db,
		before: func(target config.PrepareTarget, _ string) {
			latest, err := metadata.LatestBuildForTarget(ctx, db, target.Region, target.Product, target.Locale)
			if err != nil {
				t.Fatalf("latest discovered build before materialization: %v", err)
			}
			if latest.Key.BuildKey != "discovered-build" || latest.State != metadata.StatePreparing {
				t.Fatalf("latest discovered build before materialization = %#v, want discovered-build preparing", latest)
			}
		},
	}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
}

func TestRecordDiscoveredPreparingReportsWaitingForMaterialization(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	target := config.PrepareTarget{Label: "US Beta", Region: "us", Product: "wowxptr", Locale: "enUS"}
	build := DiscoveredBuild{BuildKey: "beta-build", BuildName: "Beta Build"}

	if err := recordDiscoveredPreparing(ctx, db, target, build); err != nil {
		t.Fatalf("record discovered preparing: %v", err)
	}

	latest, err := metadata.LatestBuildForTarget(ctx, db, "us", "wowxptr", "enUS")
	if err != nil {
		t.Fatalf("latest build: %v", err)
	}
	if latest.Error != "build discovered, waiting for materialization" {
		t.Fatalf("latest discovered build message = %q, want discovery progress", latest.Error)
	}
}

func TestPrepareRecordsDiscoveredBuildBeforeResourcePreparationFailure(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	resourceErr := errors.New("cdn reset during CASC preload")
	discoverer := &resourcePreparingDiscoverer{
		builds: map[string]DiscoveredBuild{
			"us/wow/enUS": {BuildKey: "discovered-build", BuildName: "Discovered Build"},
		},
		err: resourceErr,
	}
	materializer := &fakeMaterializer{db: db}

	err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx)
	if !errors.Is(err, resourceErr) {
		t.Fatalf("Prepare error = %v, want %v", err, resourceErr)
	}
	latest, err := metadata.LatestBuildForTarget(ctx, db, "us", "wow", "enUS")
	if err != nil {
		t.Fatalf("latest discovered build: %v", err)
	}
	if latest.Key.BuildKey != "discovered-build" || latest.State != metadata.StateFailed || !strings.Contains(latest.Error, "cdn reset") {
		t.Fatalf("latest discovered build = %#v, want discovered-build failed with resource error", latest)
	}
	if materializer.calls != 0 {
		t.Fatalf("materialize calls = %d, want 0 when resource preparation fails", materializer.calls)
	}
}

func TestPrepareReportsResourcePreparationStage(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	discoverer := &resourcePreparingDiscoverer{
		builds: map[string]DiscoveredBuild{
			"us/wow/enUS": {BuildKey: "discovered-build", BuildName: "Discovered Build"},
		},
		onPrepare: func() {
			latest, err := metadata.LatestBuildForTarget(ctx, db, "us", "wow", "enUS")
			if err != nil {
				t.Fatalf("latest during resource preparation: %v", err)
			}
			if latest.Error != "resource preparation started" {
				t.Fatalf("preparing message during resource preparation = %q, want resource preparation started", latest.Error)
			}
		},
	}
	materializer := &fakeMaterializer{db: db}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
}

func TestPrepareReportsCurrentMaterializingTable(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item", "Spell"})
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "discovered-build", BuildName: "Discovered Build"},
	}}
	materializer := &fakeMaterializer{
		db: db,
		before: func(_ config.PrepareTarget, tableName string) {
			latest, err := metadata.LatestBuildForTarget(ctx, db, "us", "wow", "enUS")
			if err != nil {
				t.Fatalf("latest during table materialization: %v", err)
			}
			want := "materializing " + tableName
			if latest.Error != want {
				t.Fatalf("preparing message = %q, want %q", latest.Error, want)
			}
		},
	}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
}

func TestPrepareSkipsMaterializationWhenActiveBuildHasAllDefaultTables(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item", "Spell"})
	seedActiveBuildWithTables(t, ctx, db, metadata.BuildKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "ready-build",
	}, cfg.Prepare.DefaultTables)
	materializer := &fakeMaterializer{db: db}
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: "ready-build", BuildName: "Ready Build"},
	}}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if materializer.calls != 0 {
		t.Fatalf("materialize calls = %d, want 0 for ready active build with all default tables", materializer.calls)
	}
	snapshot := healthSnapshot(t, ctx, db, cfg)
	if !snapshot.Readiness.OK || snapshot.Contexts[0].State != health.StateReady {
		t.Fatalf("health after prepare = %#v, want ready", snapshot)
	}
}

func TestPrepareRematerializesOutdatedTableVersionsForReadyActiveBuild(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item", "Spell"})
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "ready-build"}
	seedActiveBuildWithTables(t, ctx, db, key, []string{"Item"})
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key: metadata.TableKey{
			Region: key.Region, Product: key.Product, Locale: key.Locale, BuildKey: key.BuildKey, TableName: "Spell",
		},
		DB2FileDataID:       2,
		DBDHash:             "old-dbd-Spell",
		DecoderVersion:      "old-decoder",
		MaterializerVersion: "old-materializer",
		ParquetPath:         filepath.ToSlash(filepath.Join("cache", "db2", "Spell.parquet")),
		RowCount:            1,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("upsert outdated Spell: %v", err)
	}
	materializer := &fakeMaterializer{db: db}
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: key.BuildKey, BuildName: "Ready Build"},
	}}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if materializer.calls != 1 || strings.Join(materializer.tables, ",") != "Spell" {
		t.Fatalf("materialized tables = %q (%d calls), want only outdated Spell", strings.Join(materializer.tables, ","), materializer.calls)
	}
	record, err := metadata.LatestValidMaterializedTable(ctx, db, metadata.TableLookup{
		Region: key.Region, Product: key.Product, Locale: key.Locale, TableName: "Spell",
	})
	if err != nil {
		t.Fatalf("latest Spell: %v", err)
	}
	if record.DecoderVersion != "fake-decoder" || record.MaterializerVersion != "fake-materializer" {
		t.Fatalf("Spell versions = %q/%q, want fake-decoder/fake-materializer", record.DecoderVersion, record.MaterializerVersion)
	}
}

func TestPrepareRematerializesChangedDBDHashForReadyActiveBuild(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item", "Spell"})
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "ready-build"}
	seedActiveBuildWithTables(t, ctx, db, key, cfg.Prepare.DefaultTables)
	materializer := &fakeFingerprintMaterializer{
		fakeMaterializer: &fakeMaterializer{db: db},
		fingerprints: map[string]TableFingerprint{
			"Item":  {TableName: "Item", DB2FileDataID: 1, DBDHash: "old-dbd-Item"},
			"Spell": {TableName: "Spell", DB2FileDataID: 2, DBDHash: "new-dbd-Spell"},
		},
	}
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: key.BuildKey, BuildName: "Ready Build"},
	}}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if materializer.calls != 1 || strings.Join(materializer.tables, ",") != "Spell" {
		t.Fatalf("materialized tables = %q (%d calls), want only changed DBD hash Spell", strings.Join(materializer.tables, ","), materializer.calls)
	}
}

func TestPrepareRematerializesStaleTableForReadyActiveBuild(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"*"})
	key := metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "ready-build"}
	seedActiveBuildWithTables(t, ctx, db, key, []string{"Item", "Spell"})
	if err := metadata.MarkMaterializedTableState(ctx, db, metadata.TableKey{
		Region: key.Region, Product: key.Product, Locale: key.Locale, BuildKey: key.BuildKey, TableName: "Spell",
	}, metadata.StateStale, "test stale"); err != nil {
		t.Fatalf("mark Spell stale: %v", err)
	}
	materializer := &fakeMaterializer{db: db, availableTables: []string{"Item", "Spell"}}
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {BuildKey: key.BuildKey, BuildName: "Ready Build"},
	}}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if materializer.calls != 1 || strings.Join(materializer.tables, ",") != "Spell" {
		t.Fatalf("materialized tables = %q (%d calls), want only stale Spell", strings.Join(materializer.tables, ","), materializer.calls)
	}
}

func TestPrepareReusesDB2TablesWhenActiveBuildHasAllDefaultTables(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item", "Spell"})
	seedActiveBuildWithTables(t, ctx, db, metadata.BuildKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "ready-build",
	}, cfg.Prepare.DefaultTables)
	var prepareResourcesCalled bool
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {
			BuildKey:  "ready-build",
			BuildName: "Ready Build",
			PrepareResources: func(context.Context) (DiscoveredBuild, error) {
				prepareResourcesCalled = true
				return DiscoveredBuild{
					BuildKey:  "ready-build",
					BuildName: "Ready Build",
					ResourceIndexes: ResourceIndexes{
						ListfileSourceHash: "listfile-hash",
						ListfileEntries: []serverlistfile.Entry{
							{FileDataID: 555, Path: "Interface/Icons/Ready.blp"},
						},
						CASCSource:        cascindex.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "ready-build"},
						CASCSourceVersion: "casc-version",
						CASCRoots: []cascindex.RootMapping{
							{FileDataID: 555, ContentKey: "content-key"},
						},
						CASCEncodings: []cascindex.EncodingMapping{
							{ContentKey: "content-key", EncodingKey: "encoding-key", Size: 12},
						},
						CASCArchives: []cascindex.ArchiveMapping{
							{EncodingKey: "encoding-key", ArchiveKey: "archive-key", Offset: 34, Size: 12},
						},
					},
				}, nil
			},
		},
	}}
	materializer := &fakeMaterializer{db: db}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if !prepareResourcesCalled {
		t.Fatal("PrepareResources was not called to refresh asset indexes for a ready active build")
	}
	if materializer.calls != 0 {
		t.Fatalf("materialize calls = %d, want 0 for ready active build with all default tables", materializer.calls)
	}
}

func TestCASCResourceMappingsKeepOnlyTargetLocaleReachableFiles(t *testing.T) {
	source := casc.NewCASCSource()
	source.RootTypes = []casc.RootType{
		{LocaleFlags: casc.LocaleEnUS},
		{LocaleFlags: casc.LocaleZhCN},
		{LocaleFlags: casc.LocaleEnUS, ContentFlags: casc.ContentLowViolence},
	}
	source.RootEntries = map[uint32][]casc.RootEntry{
		100: {{TypeIndex: 0, ContentKey: "enus-content"}},
		200: {{TypeIndex: 1, ContentKey: "zhcn-content"}},
		300: {{TypeIndex: 2, ContentKey: "low-violence-content"}},
		400: {{TypeIndex: 99, ContentKey: "bad-type-content"}},
		500: {{TypeIndex: 0, ContentKey: "missing-encoding-content"}},
		600: {{TypeIndex: 0, ContentKey: "missing-archive-content"}},
	}
	source.EncodingEntries = map[string]casc.EncodingEntry{
		"enus-content":            {Key: "enus-encoding", Size: 11},
		"zhcn-content":            {Key: "zhcn-encoding", Size: 22},
		"low-violence-content":    {Key: "low-violence-encoding", Size: 33},
		"missing-archive-content": {Key: "missing-archive-encoding", Size: 55},
		"unreferenced-content":    {Key: "unreferenced-encoding", Size: 44},
	}
	source.Archives = map[string]casc.ArchiveEntry{
		"enus-encoding":         {Key: "enus-archive", Offset: 10, Size: 11},
		"zhcn-encoding":         {Key: "zhcn-archive", Offset: 20, Size: 22},
		"low-violence-encoding": {Key: "low-violence-archive", Offset: 30, Size: 33},
		"unreferenced-encoding": {Key: "unreferenced-archive", Offset: 40, Size: 44},
	}

	roots, encodings, archives := cascResourceMappingsForLocale(source, casc.LocaleEnUS)

	if len(roots) != 1 || roots[0] != (cascindex.RootMapping{FileDataID: 100, ContentKey: "enus-content"}) {
		t.Fatalf("roots = %#v, want only enUS non-low-violence root", roots)
	}
	if len(encodings) != 1 || encodings[0] != (cascindex.EncodingMapping{ContentKey: "enus-content", EncodingKey: "enus-encoding", Size: 11}) {
		t.Fatalf("encodings = %#v, want only encoding reachable from enUS root", encodings)
	}
	if len(archives) != 1 || archives[0] != (cascindex.ArchiveMapping{EncodingKey: "enus-encoding", ArchiveKey: "enus-archive", Offset: 10, Size: 11}) {
		t.Fatalf("archives = %#v, want only archive reachable from enUS encoding", archives)
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

func TestPrepareFailureForSameActiveBuildMissingTableKeepsExistingBuildValid(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item", "Spell"})
	seedActiveBuildWithTables(t, ctx, db, metadata.BuildKey{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1",
	}, []string{"Item"})
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
	if materializer.calls != 1 || strings.Join(materializer.tables, ",") != "Spell" {
		t.Fatalf("materialized tables = %q (%d calls), want only missing Spell", strings.Join(materializer.tables, ","), materializer.calls)
	}
	snapshot := healthSnapshot(t, ctx, db, cfg)
	if snapshot.Readiness.OK || snapshot.Contexts[0].State == health.StateReady || snapshot.Contexts[0].PrepareCurrent != 1 {
		t.Fatalf("health = %#v, want old active build valid with 1/2 tables after missing-table failure", snapshot)
	}
}

func TestPrepareNoBuildRecordsNoBuildAndDoesNotBlockReadiness(t *testing.T) {
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
	latest, err := metadata.LatestBuildForTarget(ctx, db, "us", "wow", "enUS")
	if err != nil {
		t.Fatalf("latest no-build row: %v", err)
	}
	if latest.State != metadata.StateNoBuild {
		t.Fatalf("latest state = %q, want no_build", latest.State)
	}
	snapshot := healthSnapshot(t, ctx, db, cfg)
	if !snapshot.Readiness.OK || snapshot.Contexts[0].State != health.StateNoBuild {
		t.Fatalf("health = %#v, want no_build readiness ok for non-strict target", snapshot)
	}
	if snapshot.Contexts[0].Error != "product is not published in region" {
		t.Fatalf("health error = %q, want no-build error", snapshot.Contexts[0].Error)
	}
}

func TestPrepareNoBuildForStrictTargetBlocksReadiness(t *testing.T) {
	ctx := context.Background()
	metadataPath := filepath.Join(t.TempDir(), "metadata.sqlite")
	db := openMetadataDBAt(t, metadataPath)
	cfg := testConfig(metadataPath, []string{"Item"})
	cfg.Prepare.Targets[0].Strict = true
	discoverer := fakeDiscoverer{builds: map[string]DiscoveredBuild{
		"us/wow/enUS": {NoBuild: true, Error: "strict target has no build"},
	}}
	materializer := &fakeMaterializer{db: db}

	if err := (Runner{Config: cfg, DB: db, Discoverer: discoverer, Materializer: materializer}).Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	snapshot := healthSnapshot(t, ctx, db, cfg)
	if snapshot.Readiness.OK || snapshot.Contexts[0].State != health.StateNoBuild {
		t.Fatalf("health = %#v, want no_build strict target to block readiness", snapshot)
	}
	if snapshot.Readiness.RequiredTargetsTotal != 1 || snapshot.Readiness.RequiredTargetsReady != 0 {
		t.Fatalf("readiness counts = %#v, want 0/1 strict no-build", snapshot.Readiness)
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

func TestHTTPServerDiscoveryProductsOnlyIncludesRequestedTargetProduct(t *testing.T) {
	products := httpServerDiscoveryProducts(config.PrepareTarget{Product: "wow_classic_ptr"})
	if len(products) != 1 || products[0] != "wow_classic_ptr" {
		t.Fatalf("products = %#v, want only requested target product", products)
	}
	for _, product := range products {
		if product == "wowxptr" || product == "wow_classic_era" {
			t.Fatalf("products include excluded default product %q", product)
		}
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

type recordingDiscoverer struct {
	mu     sync.Mutex
	builds map[string]DiscoveredBuild
	seen   map[string]bool
}

func (d *recordingDiscoverer) DiscoverBuild(_ context.Context, target config.PrepareTarget) (DiscoveredBuild, error) {
	key := target.Region + "/" + target.Product + "/" + target.Locale
	build, ok := d.builds[key]
	if !ok {
		return DiscoveredBuild{}, errors.New("unexpected discover target " + key)
	}
	d.mu.Lock()
	if d.seen == nil {
		d.seen = map[string]bool{}
	}
	d.seen[key] = true
	d.mu.Unlock()
	return build, nil
}

func (d *recordingDiscoverer) DiscoveredCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.seen)
}

type resourcePreparingDiscoverer struct {
	builds    map[string]DiscoveredBuild
	err       error
	onPrepare func()
}

func (d *resourcePreparingDiscoverer) DiscoverBuild(_ context.Context, target config.PrepareTarget) (DiscoveredBuild, error) {
	key := target.Region + "/" + target.Product + "/" + target.Locale
	build, ok := d.builds[key]
	if !ok {
		return DiscoveredBuild{}, errors.New("unexpected discover target " + key)
	}
	build.PrepareResources = func(context.Context) (DiscoveredBuild, error) {
		if d.onPrepare != nil {
			d.onPrepare()
		}
		return build, d.err
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
	db              *sql.DB
	failTable       string
	err             error
	availableTables []string
	mu              sync.Mutex
	tables          []string
	calls           int
	during          func(string)
	before          func(config.PrepareTarget, string)
}

func (m *fakeMaterializer) AvailableTables(context.Context) ([]string, error) {
	return append([]string{}, m.availableTables...), nil
}

func (m *fakeMaterializer) MaterializeTable(ctx context.Context, target config.PrepareTarget, build DiscoveredBuild, tableName string) error {
	if m.before != nil {
		m.before(target, tableName)
	}
	m.mu.Lock()
	m.calls++
	call := m.calls
	m.tables = append(m.tables, tableName)
	m.mu.Unlock()
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
		DB2FileDataID:       100 + call,
		DBDHash:             "dbd-" + tableName,
		DecoderVersion:      "fake-decoder",
		MaterializerVersion: "fake-materializer",
		ParquetPath:         filepath.ToSlash(filepath.Join("cache", "db2", tableName+".parquet")),
		RowCount:            1,
		State:               metadata.StateValid,
	})
}

func (m *fakeMaterializer) CurrentTableVersions() (string, string) {
	return "fake-decoder", "fake-materializer"
}

type fakeFingerprintMaterializer struct {
	*fakeMaterializer
	fingerprints map[string]TableFingerprint
}

func (m *fakeFingerprintMaterializer) TableFingerprint(_ context.Context, _ config.PrepareTarget, _ DiscoveredBuild, tableName string) (TableFingerprint, error) {
	fingerprint, ok := m.fingerprints[tableName]
	if !ok {
		return TableFingerprint{}, fmt.Errorf("missing fingerprint for %s", tableName)
	}
	return fingerprint, nil
}

func eventually(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return condition()
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
