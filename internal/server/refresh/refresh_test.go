package refresh

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"wowdata/internal/server/storage/metadata"
)

func TestRefreshNoNewBuildLeavesActiveBuildUnchanged(t *testing.T) {
	ctx := context.Background()
	db := openRefreshTestDB(t)
	active := seedActiveBuild(t, ctx, db, "old")

	workflow := Workflow{
		DB:         db,
		Discoverer: fixtureDiscoverer{},
		Preparer:   fixturePreparer{},
	}
	result, err := workflow.RefreshTarget(ctx, Target{Region: active.Key.Region, Product: active.Key.Product, Locale: active.Key.Locale})
	if err != nil {
		t.Fatalf("refresh target: %v", err)
	}
	if result.Activated {
		t.Fatalf("activated = true, want false")
	}
	assertActiveBuild(t, ctx, db, "old")
}

func TestRefreshCandidateSuccessActivatesNewBuild(t *testing.T) {
	ctx := context.Background()
	db := openRefreshTestDB(t)
	seedActiveBuild(t, ctx, db, "old")

	workflow := Workflow{
		DB: db,
		Discoverer: fixtureDiscoverer{candidate: BuildCandidate{
			Region: "us", Product: "wow", Locale: "enUS", BuildKey: "new", BuildName: "new build",
		}},
		Preparer: fixturePreparer{},
	}
	result, err := workflow.RefreshTarget(ctx, Target{Region: "us", Product: "wow", Locale: "enUS"})
	if err != nil {
		t.Fatalf("refresh target: %v", err)
	}
	if !result.Activated {
		t.Fatalf("activated = false, want true")
	}
	assertActiveBuild(t, ctx, db, "new")
}

func TestRefreshCandidateFailurePreservesOldActiveBuild(t *testing.T) {
	ctx := context.Background()
	db := openRefreshTestDB(t)
	seedActiveBuild(t, ctx, db, "build-1")
	seedValidTable(t, ctx, db, "Spell", "dbd-a", "decoder-1", "materializer-1")
	sourceKey := metadata.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"}
	if changed, err := metadata.UpsertListfileSource(ctx, db, metadata.ListfileSource{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", SourceHash: "hash-a", State: metadata.StateValid,
	}); err != nil || changed {
		t.Fatalf("seed listfile source changed=%v err=%v, want false nil", changed, err)
	}
	if err := metadata.UpsertListfileIndexState(ctx, db, sourceKey, "main", metadata.StateValid, ""); err != nil {
		t.Fatalf("seed listfile index: %v", err)
	}
	if changed, err := metadata.UpsertCASCSource(ctx, db, metadata.CASCSource{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1",
		BuildConfig: "build-config-a", CDNConfig: "cdn-config-a", State: metadata.StateValid,
	}); err != nil || changed {
		t.Fatalf("seed casc source changed=%v err=%v, want false nil", changed, err)
	}
	if err := metadata.UpsertCASCIndexState(ctx, db, sourceKey, "root-encoding-archive", metadata.StateValid, ""); err != nil {
		t.Fatalf("seed casc index: %v", err)
	}

	prepareErr := errors.New("fixture prepare failed")
	workflow := Workflow{
		DB: db,
		Discoverer: fixtureDiscoverer{candidate: BuildCandidate{
			Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-2", BuildName: "new build",
			ListfileSourceHash: "hash-b",
			CASCBuildConfig:    "build-config-b",
			CASCCDNConfig:      "cdn-config-a",
			Tables: []TableFingerprint{
				{TableName: "Spell", DB2FileDataID: 123, DBDHash: "dbd-b", DecoderVersion: "decoder-1", MaterializerVersion: "materializer-1"},
			},
		}},
		Preparer: fixturePreparer{err: prepareErr},
	}
	result, err := workflow.RefreshTarget(ctx, Target{Region: "us", Product: "wow", Locale: "enUS"})
	if !errors.Is(err, prepareErr) {
		t.Fatalf("refresh error = %v, want %v", err, prepareErr)
	}
	if result.Activated {
		t.Fatalf("activated = true, want false")
	}
	assertActiveBuild(t, ctx, db, "build-1")
	assertTableState(t, db, "Spell", metadata.StateValid)
	assertListfileIndexState(t, ctx, db, sourceKey, "main", metadata.StateValid)
	assertCASCIndexState(t, ctx, db, sourceKey, "root-encoding-archive", metadata.StateValid)
	assertBuildState(t, db, "build-2", metadata.StateFailed)
}

func TestRefreshListfileSourceHashChangeMarksListfileStale(t *testing.T) {
	ctx := context.Background()
	db := openRefreshTestDB(t)
	seedActiveBuild(t, ctx, db, "build-1")
	key := metadata.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"}
	if changed, err := metadata.UpsertListfileSource(ctx, db, metadata.ListfileSource{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", SourceHash: "hash-a", State: metadata.StateValid,
	}); err != nil || changed {
		t.Fatalf("seed listfile source changed=%v err=%v, want false nil", changed, err)
	}
	if err := metadata.UpsertListfileIndexState(ctx, db, key, "main", metadata.StateValid, ""); err != nil {
		t.Fatalf("seed listfile index: %v", err)
	}

	workflow := Workflow{
		DB: db,
		Discoverer: fixtureDiscoverer{candidate: BuildCandidate{
			Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", BuildName: "build one", ListfileSourceHash: "hash-b",
		}},
		Preparer: fixturePreparer{},
	}
	if _, err := workflow.RefreshTarget(ctx, Target{Region: "us", Product: "wow", Locale: "enUS"}); err != nil {
		t.Fatalf("refresh target: %v", err)
	}
	assertListfileIndexState(t, ctx, db, key, "main", metadata.StateStale)
	assertActiveBuild(t, ctx, db, "build-1")
}

func TestRefreshCASCIndexVersionChangeMarksCASCStale(t *testing.T) {
	ctx := context.Background()
	db := openRefreshTestDB(t)
	seedActiveBuild(t, ctx, db, "build-1")
	key := metadata.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1"}
	if changed, err := metadata.UpsertCASCSource(ctx, db, metadata.CASCSource{
		Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1",
		BuildConfig: "build-config-a", CDNConfig: "cdn-config-a", State: metadata.StateValid,
	}); err != nil || changed {
		t.Fatalf("seed casc source changed=%v err=%v, want false nil", changed, err)
	}
	if err := metadata.UpsertCASCIndexState(ctx, db, key, "root-encoding-archive", metadata.StateValid, ""); err != nil {
		t.Fatalf("seed casc index: %v", err)
	}

	workflow := Workflow{
		DB: db,
		Discoverer: fixtureDiscoverer{candidate: BuildCandidate{
			Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", BuildName: "build one",
			CASCBuildConfig: "build-config-b", CASCCDNConfig: "cdn-config-a",
		}},
		Preparer: fixturePreparer{},
	}
	if _, err := workflow.RefreshTarget(ctx, Target{Region: "us", Product: "wow", Locale: "enUS"}); err != nil {
		t.Fatalf("refresh target: %v", err)
	}
	assertCASCIndexState(t, ctx, db, key, "root-encoding-archive", metadata.StateStale)
	assertActiveBuild(t, ctx, db, "build-1")
}

func TestRefreshDB2FingerprintChangeMarksOnlyAffectedTableStale(t *testing.T) {
	ctx := context.Background()
	db := openRefreshTestDB(t)
	seedActiveBuild(t, ctx, db, "build-1")
	seedValidTable(t, ctx, db, "Spell", "dbd-a", "decoder-1", "materializer-1")
	seedValidTable(t, ctx, db, "Item", "dbd-a", "decoder-1", "materializer-1")

	workflow := Workflow{
		DB: db,
		Discoverer: fixtureDiscoverer{candidate: BuildCandidate{
			Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", BuildName: "build one",
			Tables: []TableFingerprint{
				{TableName: "Spell", DB2FileDataID: 123, DBDHash: "dbd-b", DecoderVersion: "decoder-1", MaterializerVersion: "materializer-1"},
				{TableName: "Item", DB2FileDataID: 456, DBDHash: "dbd-a", DecoderVersion: "decoder-1", MaterializerVersion: "materializer-1"},
			},
		}},
		Preparer: fixturePreparer{},
	}
	result, err := workflow.RefreshTarget(ctx, Target{Region: "us", Product: "wow", Locale: "enUS"})
	if err != nil {
		t.Fatalf("refresh target: %v", err)
	}
	if len(result.StaleTables) != 1 || result.StaleTables[0] != "Spell" {
		t.Fatalf("stale tables = %#v, want Spell only", result.StaleTables)
	}
	assertTableState(t, db, "Spell", metadata.StateStale)
	assertTableState(t, db, "Item", metadata.StateValid)
}

func TestRefreshUnaffectedTableRemainsValid(t *testing.T) {
	ctx := context.Background()
	db := openRefreshTestDB(t)
	seedActiveBuild(t, ctx, db, "build-1")
	seedValidTable(t, ctx, db, "Spell", "dbd-a", "decoder-1", "materializer-1")

	workflow := Workflow{
		DB: db,
		Discoverer: fixtureDiscoverer{candidate: BuildCandidate{
			Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", BuildName: "build one",
			Tables: []TableFingerprint{
				{TableName: "Spell", DB2FileDataID: 123, DBDHash: "dbd-a", DecoderVersion: "decoder-1", MaterializerVersion: "materializer-1"},
			},
		}},
		Preparer: fixturePreparer{},
	}
	result, err := workflow.RefreshTarget(ctx, Target{Region: "us", Product: "wow", Locale: "enUS"})
	if err != nil {
		t.Fatalf("refresh target: %v", err)
	}
	if len(result.StaleTables) != 0 {
		t.Fatalf("stale tables = %#v, want none", result.StaleTables)
	}
	assertTableState(t, db, "Spell", metadata.StateValid)
}

type fixtureDiscoverer struct {
	candidate BuildCandidate
	err       error
}

func (d fixtureDiscoverer) LatestBuild(context.Context, Target) (BuildCandidate, error) {
	return d.candidate, d.err
}

type fixturePreparer struct {
	err error
}

func (p fixturePreparer) PrepareCandidate(context.Context, BuildCandidate) error {
	return p.err
}

func openRefreshTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedActiveBuild(t *testing.T, ctx context.Context, db *sql.DB, buildKey string) metadata.Build {
	t.Helper()
	build := metadata.Build{
		Key:       metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: buildKey},
		BuildName: buildKey,
		State:     metadata.StateValid,
	}
	if err := metadata.UpsertDiscoveredBuild(ctx, db, build); err != nil {
		t.Fatalf("upsert active seed: %v", err)
	}
	if err := metadata.MarkBuildReady(ctx, db, build.Key); err != nil {
		t.Fatalf("mark active seed ready: %v", err)
	}
	if err := metadata.ActivateBuild(ctx, db, build.Key); err != nil {
		t.Fatalf("activate seed build: %v", err)
	}
	return build
}

func seedValidTable(t *testing.T, ctx context.Context, db *sql.DB, tableName, dbdHash, decoderVersion, materializerVersion string) {
	t.Helper()
	if err := metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key: metadata.TableKey{
			Region: "us", Product: "wow", Locale: "enUS", BuildKey: "build-1", TableName: tableName,
		},
		DB2FileDataID:       map[string]int{"Spell": 123, "Item": 456}[tableName],
		DBDHash:             dbdHash,
		DecoderVersion:      decoderVersion,
		MaterializerVersion: materializerVersion,
		ParquetPath:         "cache/" + tableName + ".parquet",
		RowCount:            10,
		State:               metadata.StateValid,
	}); err != nil {
		t.Fatalf("seed table %s: %v", tableName, err)
	}
}

func assertActiveBuild(t *testing.T, ctx context.Context, db *sql.DB, want string) {
	t.Helper()
	active, err := metadata.ActiveBuild(ctx, db, "us", "wow", "enUS")
	if err != nil {
		t.Fatalf("active build: %v", err)
	}
	if active.Key.BuildKey != want {
		t.Fatalf("active build = %q, want %q", active.Key.BuildKey, want)
	}
}

func assertBuildState(t *testing.T, db *sql.DB, buildKey, want string) {
	t.Helper()
	var state string
	if err := db.QueryRow(`SELECT state FROM server_builds WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND build_key = ?`, buildKey).Scan(&state); err != nil {
		t.Fatalf("query build state: %v", err)
	}
	if state != want {
		t.Fatalf("build %s state = %q, want %q", buildKey, state, want)
	}
}

func assertListfileIndexState(t *testing.T, ctx context.Context, db *sql.DB, key metadata.SourceKey, indexName, want string) {
	t.Helper()
	state, err := metadata.ListfileIndexState(ctx, db, key, indexName)
	if err != nil {
		t.Fatalf("query listfile index state: %v", err)
	}
	if state != want {
		t.Fatalf("listfile index state = %q, want %q", state, want)
	}
}

func assertCASCIndexState(t *testing.T, ctx context.Context, db *sql.DB, key metadata.SourceKey, indexName, want string) {
	t.Helper()
	state, err := metadata.CASCIndexState(ctx, db, key, indexName)
	if err != nil {
		t.Fatalf("query casc index state: %v", err)
	}
	if state != want {
		t.Fatalf("casc index state = %q, want %q", state, want)
	}
}

func assertTableState(t *testing.T, db *sql.DB, tableName, want string) {
	t.Helper()
	var state string
	if err := db.QueryRow(`SELECT state FROM server_materialized_tables WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND build_key = 'build-1' AND table_name = ?`, tableName).Scan(&state); err != nil {
		t.Fatalf("query table state: %v", err)
	}
	if state != want {
		t.Fatalf("table %s state = %q, want %q", tableName, state, want)
	}
}
