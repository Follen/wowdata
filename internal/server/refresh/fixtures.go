package refresh

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"wowdata/internal/server/storage/metadata"
)

type FixtureResult struct {
	Check  string `json:"check"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

func RunUpdateFixture(ctx context.Context, check string) (FixtureResult, error) {
	if check == "" {
		return FixtureResult{}, fmt.Errorf("update fixture check is required")
	}
	result := FixtureResult{Check: check}
	err := runUpdateFixture(ctx, check)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.Passed = true
	return result, nil
}

func runUpdateFixture(ctx context.Context, check string) error {
	switch check {
	case "UpdateNoNewBuild":
		return fixtureNoNewBuild(ctx)
	case "UpdateCandidateSuccess":
		return fixtureCandidateSuccess(ctx)
	case "UpdateCandidateFailure":
		return fixtureCandidateFailure(ctx)
	case "UpdateListfileSourceHashChange":
		return fixtureListfileSourceHashChange(ctx)
	case "UpdateCASCIndexVersionChange":
		return fixtureCASCIndexVersionChange(ctx)
	case "UpdateDB2FingerprintChange":
		return fixtureDB2FingerprintChange(ctx)
	case "UpdateUnaffectedTableStillValid":
		return fixtureUnaffectedTableStillValid(ctx)
	default:
		return fmt.Errorf("unknown update fixture check %q", check)
	}
}

func fixtureNoNewBuild(ctx context.Context) error {
	db, cleanup, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cleanup()
	seedFixtureActiveBuild(ctx, db, "old")
	result, err := Workflow{DB: db, Discoverer: fixtureLatest{}}.RefreshTarget(ctx, fixtureTarget())
	if err != nil {
		return err
	}
	if result.Activated {
		return errors.New("no-new-build activated a candidate")
	}
	return requireFixtureActiveBuild(ctx, db, "old")
}

func fixtureCandidateSuccess(ctx context.Context) error {
	db, cleanup, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cleanup()
	seedFixtureActiveBuild(ctx, db, "old")
	result, err := Workflow{DB: db, Discoverer: fixtureLatest{candidate: BuildCandidate{BuildKey: "new"}}}.RefreshTarget(ctx, fixtureTarget())
	if err != nil {
		return err
	}
	if !result.Activated {
		return errors.New("candidate success did not activate")
	}
	return requireFixtureActiveBuild(ctx, db, "new")
}

func fixtureCandidateFailure(ctx context.Context) error {
	db, cleanup, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cleanup()
	seedFixtureActiveBuild(ctx, db, "old")
	prepareErr := errors.New("fixture prepare failed")
	_, err = Workflow{
		DB:         db,
		Discoverer: fixtureLatest{candidate: BuildCandidate{BuildKey: "new"}},
		Preparer:   fixturePrepare{err: prepareErr},
	}.RefreshTarget(ctx, fixtureTarget())
	if !errors.Is(err, prepareErr) {
		return fmt.Errorf("candidate failure error = %v, want %v", err, prepareErr)
	}
	if err := requireFixtureActiveBuild(ctx, db, "old"); err != nil {
		return err
	}
	return requireFixtureBuildState(db, "new", metadata.StateFailed)
}

func fixtureListfileSourceHashChange(ctx context.Context) error {
	db, cleanup, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cleanup()
	seedFixtureActiveBuild(ctx, db, "build-1")
	key := seedFixtureListfile(ctx, db, "build-1", "hash-a")
	_, err = Workflow{
		DB:         db,
		Discoverer: fixtureLatest{candidate: BuildCandidate{BuildKey: "build-1", ListfileSourceHash: "hash-b"}},
	}.RefreshTarget(ctx, fixtureTarget())
	if err != nil {
		return err
	}
	return requireFixtureListfileState(ctx, db, key, "main", metadata.StateStale)
}

func fixtureCASCIndexVersionChange(ctx context.Context) error {
	db, cleanup, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cleanup()
	seedFixtureActiveBuild(ctx, db, "build-1")
	key := seedFixtureCASC(ctx, db, "build-1", "build-config-a", "cdn-config-a")
	_, err = Workflow{
		DB:         db,
		Discoverer: fixtureLatest{candidate: BuildCandidate{BuildKey: "build-1", CASCBuildConfig: "build-config-b", CASCCDNConfig: "cdn-config-a"}},
	}.RefreshTarget(ctx, fixtureTarget())
	if err != nil {
		return err
	}
	return requireFixtureCASCState(ctx, db, key, "root-encoding-archive", metadata.StateStale)
}

func fixtureDB2FingerprintChange(ctx context.Context) error {
	db, cleanup, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cleanup()
	seedFixtureActiveBuild(ctx, db, "build-1")
	seedFixtureTable(ctx, db, "build-1", "Spell", "dbd-a")
	seedFixtureTable(ctx, db, "build-1", "Item", "dbd-a")
	result, err := Workflow{
		DB: db,
		Discoverer: fixtureLatest{candidate: BuildCandidate{BuildKey: "build-1", Tables: []TableFingerprint{
			{TableName: "Spell", DB2FileDataID: 123, DBDHash: "dbd-b", DecoderVersion: "decoder-1", MaterializerVersion: "materializer-1"},
			{TableName: "Item", DB2FileDataID: 456, DBDHash: "dbd-a", DecoderVersion: "decoder-1", MaterializerVersion: "materializer-1"},
		}}},
	}.RefreshTarget(ctx, fixtureTarget())
	if err != nil {
		return err
	}
	if len(result.StaleTables) != 1 || result.StaleTables[0] != "Spell" {
		return fmt.Errorf("stale tables = %#v, want Spell", result.StaleTables)
	}
	if err := requireFixtureTableState(db, "build-1", "Spell", metadata.StateStale); err != nil {
		return err
	}
	return requireFixtureTableState(db, "build-1", "Item", metadata.StateValid)
}

func fixtureUnaffectedTableStillValid(ctx context.Context) error {
	db, cleanup, err := openFixtureDB()
	if err != nil {
		return err
	}
	defer cleanup()
	seedFixtureActiveBuild(ctx, db, "build-1")
	seedFixtureTable(ctx, db, "build-1", "Spell", "dbd-a")
	result, err := Workflow{
		DB: db,
		Discoverer: fixtureLatest{candidate: BuildCandidate{BuildKey: "build-1", Tables: []TableFingerprint{
			{TableName: "Spell", DB2FileDataID: 123, DBDHash: "dbd-a", DecoderVersion: "decoder-1", MaterializerVersion: "materializer-1"},
		}}},
	}.RefreshTarget(ctx, fixtureTarget())
	if err != nil {
		return err
	}
	if len(result.StaleTables) != 0 {
		return fmt.Errorf("stale tables = %#v, want none", result.StaleTables)
	}
	return requireFixtureTableState(db, "build-1", "Spell", metadata.StateValid)
}

type fixtureLatest struct {
	candidate BuildCandidate
}

func (d fixtureLatest) LatestBuild(context.Context, Target) (BuildCandidate, error) {
	return d.candidate, nil
}

type fixturePrepare struct {
	err error
}

func (p fixturePrepare) PrepareCandidate(context.Context, BuildCandidate) error {
	return p.err
}

func openFixtureDB() (*sql.DB, func(), error) {
	dir, err := os.MkdirTemp("", "wowdata-update-fixture-*")
	if err != nil {
		return nil, nil, err
	}
	db, err := metadata.Open(filepath.Join(dir, "metadata.sqlite"))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, err
	}
	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(dir)
	}
	return db, cleanup, nil
}

func fixtureTarget() Target {
	return Target{Region: "us", Product: "wow", Locale: "enUS"}
}

func fixtureBuildKey(buildKey string) metadata.BuildKey {
	return metadata.BuildKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: buildKey}
}

func seedFixtureActiveBuild(ctx context.Context, db *sql.DB, buildKey string) {
	key := fixtureBuildKey(buildKey)
	_ = metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{Key: key, BuildName: buildKey, State: metadata.StateValid})
	_ = metadata.MarkBuildReady(ctx, db, key)
	_ = metadata.ActivateBuild(ctx, db, key)
}

func seedFixtureTable(ctx context.Context, db *sql.DB, buildKey, tableName, dbdHash string) {
	_ = metadata.UpsertMaterializedTable(ctx, db, metadata.MaterializedTable{
		Key: metadata.TableKey{
			Region: "us", Product: "wow", Locale: "enUS", BuildKey: buildKey, TableName: tableName,
		},
		DB2FileDataID:       map[string]int{"Spell": 123, "Item": 456}[tableName],
		DBDHash:             dbdHash,
		DecoderVersion:      "decoder-1",
		MaterializerVersion: "materializer-1",
		ParquetPath:         "cache/" + tableName + ".parquet",
		RowCount:            1,
		State:               metadata.StateValid,
	})
}

func seedFixtureListfile(ctx context.Context, db *sql.DB, buildKey, sourceHash string) metadata.SourceKey {
	key := metadata.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: buildKey}
	_, _ = metadata.UpsertListfileSource(ctx, db, metadata.ListfileSource{Region: key.Region, Product: key.Product, Locale: key.Locale, BuildKey: key.BuildKey, SourceHash: sourceHash, State: metadata.StateValid})
	_ = metadata.UpsertListfileIndexState(ctx, db, key, "main", metadata.StateValid, "")
	return key
}

func seedFixtureCASC(ctx context.Context, db *sql.DB, buildKey, buildConfig, cdnConfig string) metadata.SourceKey {
	key := metadata.SourceKey{Region: "us", Product: "wow", Locale: "enUS", BuildKey: buildKey}
	_, _ = metadata.UpsertCASCSource(ctx, db, metadata.CASCSource{Region: key.Region, Product: key.Product, Locale: key.Locale, BuildKey: key.BuildKey, BuildConfig: buildConfig, CDNConfig: cdnConfig, State: metadata.StateValid})
	_ = metadata.UpsertCASCIndexState(ctx, db, key, "root-encoding-archive", metadata.StateValid, "")
	return key
}

func requireFixtureActiveBuild(ctx context.Context, db *sql.DB, want string) error {
	active, err := metadata.ActiveBuild(ctx, db, "us", "wow", "enUS")
	if err != nil {
		return err
	}
	if active.Key.BuildKey != want {
		return fmt.Errorf("active build = %q, want %q", active.Key.BuildKey, want)
	}
	return nil
}

func requireFixtureBuildState(db *sql.DB, buildKey, want string) error {
	var state string
	if err := db.QueryRow(`SELECT state FROM server_builds WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND build_key = ?`, buildKey).Scan(&state); err != nil {
		return err
	}
	if state != want {
		return fmt.Errorf("build %s state = %q, want %q", buildKey, state, want)
	}
	return nil
}

func requireFixtureListfileState(ctx context.Context, db *sql.DB, key metadata.SourceKey, indexName, want string) error {
	state, err := metadata.ListfileIndexState(ctx, db, key, indexName)
	if err != nil {
		return err
	}
	if state != want {
		return fmt.Errorf("listfile index state = %q, want %q", state, want)
	}
	return nil
}

func requireFixtureCASCState(ctx context.Context, db *sql.DB, key metadata.SourceKey, indexName, want string) error {
	state, err := metadata.CASCIndexState(ctx, db, key, indexName)
	if err != nil {
		return err
	}
	if state != want {
		return fmt.Errorf("casc index state = %q, want %q", state, want)
	}
	return nil
}

func requireFixtureTableState(db *sql.DB, buildKey, tableName, want string) error {
	var state string
	if err := db.QueryRow(`SELECT state FROM server_materialized_tables WHERE region = 'us' AND product = 'wow' AND locale = 'enUS' AND build_key = ? AND table_name = ?`, buildKey, tableName).Scan(&state); err != nil {
		return err
	}
	if state != want {
		return fmt.Errorf("table %s/%s state = %q, want %q", buildKey, tableName, state, want)
	}
	return nil
}
