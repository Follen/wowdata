package bootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cacheparquet "wowdata/internal/cache/parquet"
	"wowdata/internal/casc"
	"wowdata/internal/db2"
	"wowdata/internal/dbd"
	appruntime "wowdata/internal/runtime"
	"wowdata/internal/server/config"
	"wowdata/internal/server/storage/metadata"
	serverparquet "wowdata/internal/server/storage/parquet"
)

const (
	decoderVersion       = "runtime-db2-loader-v1"
	materializerVersion  = "server-prepare-bootstrap-v1"
	defaultRetryAttempts = 3
	defaultRetryDelay    = 500 * time.Millisecond
)

type Config = config.Config

type DiscoveredBuild struct {
	BuildKey        string
	BuildName       string
	CASCBuildConfig string
	CASCCDNConfig   string
	NoBuild         bool
	Error           string
	FileReader      appruntime.FileDataReader
}

type Discoverer interface {
	DiscoverBuild(context.Context, config.PrepareTarget) (DiscoveredBuild, error)
}

type TableMaterializer interface {
	MaterializeTable(context.Context, config.PrepareTarget, DiscoveredBuild, string) error
}

type Runner struct {
	Config                config.Config
	DB                    *sql.DB
	Discoverer            Discoverer
	Materializer          TableMaterializer
	DiscoverRetryAttempts int
	DiscoverRetryDelay    time.Duration
	RetrySleeper          func(context.Context, time.Duration) error
}

func (r Runner) Prepare(ctx context.Context) error {
	if r.DB == nil {
		return errors.New("metadata db is required")
	}
	discoverer := r.Discoverer
	if discoverer == nil {
		discoverer = NewProductionDiscoverer(r.Config)
	}
	materializer := r.Materializer
	if materializer == nil {
		materializer = NewProductionMaterializer(r.Config, r.DB)
	}

	targets := r.Config.Prepare.Targets
	maxParallel := r.Config.Limits.MaxParallelContextPrepares
	if maxParallel <= 0 {
		maxParallel = 1
	}
	if maxParallel > len(targets) {
		maxParallel = len(targets)
	}

	var firstErr error
	var firstErrMu sync.Mutex
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for _, target := range r.Config.Prepare.Targets {
		sem <- struct{}{}
		wg.Add(1)
		go func(target config.PrepareTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := r.prepareTarget(ctx, target, discoverer, materializer); err != nil {
				firstErrMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				firstErrMu.Unlock()
			}
		}(target)
	}
	wg.Wait()
	return firstErr
}

func (r Runner) prepareTarget(ctx context.Context, target config.PrepareTarget, discoverer Discoverer, materializer TableMaterializer) error {
	if len(r.Config.Prepare.DefaultTables) == 0 {
		return nil
	}
	build, err := r.discoverBuild(ctx, target, discoverer)
	if err != nil {
		recordTargetFailure(ctx, r.DB, target, err.Error())
		return err
	}
	if build.NoBuild || build.BuildKey == "" {
		message := build.Error
		if message == "" {
			message = "no build found"
		}
		recordTargetNoBuild(ctx, r.DB, target, message)
		return nil
	}
	if build.BuildName == "" {
		build.BuildName = build.BuildKey
	}

	buildKey := metadata.BuildKey{
		Region:   target.Region,
		Product:  target.Product,
		Locale:   target.Locale,
		BuildKey: build.BuildKey,
	}
	active, activeErr := metadata.ActiveBuild(ctx, r.DB, target.Region, target.Product, target.Locale)
	if activeErr != nil && !errors.Is(activeErr, sql.ErrNoRows) {
		return activeErr
	}
	sameActiveValid := activeErr == nil && active.Key.BuildKey == build.BuildKey && active.State == metadata.StateValid
	tablesToMaterialize := r.Config.Prepare.DefaultTables
	var restoreTables []metadata.MaterializedTable
	if sameActiveValid {
		tables, err := metadata.ListValidMaterializedTables(ctx, r.DB, metadata.TableCatalogLookup{
			Region:   target.Region,
			Product:  target.Product,
			Locale:   target.Locale,
			BuildKey: build.BuildKey,
		})
		if err != nil {
			return err
		}
		restoreTables = tables
		tablesToMaterialize = missingDefaultTables(r.Config.Prepare.DefaultTables, tables)
		if len(tablesToMaterialize) == 0 {
			return nil
		}
	} else {
		if err := metadata.UpsertDiscoveredBuild(ctx, r.DB, metadata.Build{
			Key:       buildKey,
			BuildName: build.BuildName,
			State:     metadata.StatePreparing,
		}); err != nil {
			return err
		}
	}

	for _, tableName := range tablesToMaterialize {
		if err := materializer.MaterializeTable(ctx, target, build, tableName); err != nil {
			if sameActiveValid {
				_ = restoreValidTables(ctx, r.DB, restoreTables)
				_ = metadata.MarkBuildReady(ctx, r.DB, buildKey)
				return err
			}
			_ = metadata.MarkBuildFailed(ctx, r.DB, buildKey, err.Error())
			return err
		}
	}
	if err := metadata.MarkBuildReady(ctx, r.DB, buildKey); err != nil {
		return err
	}
	return metadata.ActivateBuild(ctx, r.DB, buildKey)
}

func missingDefaultTables(defaultTables []string, validTables []metadata.MaterializedTable) []string {
	validByName := make(map[string]bool, len(validTables))
	for _, table := range validTables {
		validByName[table.Key.TableName] = true
	}
	var missing []string
	for _, tableName := range defaultTables {
		if !validByName[tableName] {
			missing = append(missing, tableName)
		}
	}
	return missing
}

func (r Runner) discoverBuild(ctx context.Context, target config.PrepareTarget, discoverer Discoverer) (DiscoveredBuild, error) {
	attempts := r.DiscoverRetryAttempts
	if attempts <= 0 {
		attempts = defaultRetryAttempts
	}
	delay := r.DiscoverRetryDelay
	if delay <= 0 {
		delay = defaultRetryDelay
	}
	sleeper := r.RetrySleeper
	if sleeper == nil {
		sleeper = sleepContext
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return DiscoveredBuild{}, err
		}
		build, err := discoverer.DiscoverBuild(ctx, target)
		if err == nil {
			return build, nil
		}
		lastErr = err
		if attempt == attempts || !isTransientDiscoverError(err) {
			return DiscoveredBuild{}, err
		}
		if err := sleeper(ctx, delay); err != nil {
			return DiscoveredBuild{}, err
		}
	}
	return DiscoveredBuild{}, lastErr
}

func isTransientDiscoverError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout") || strings.Contains(message, "deadline exceeded")
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func restoreValidTables(ctx context.Context, db *sql.DB, tables []metadata.MaterializedTable) error {
	for _, table := range tables {
		table.State = metadata.StateValid
		table.Error = ""
		if err := metadata.UpsertMaterializedTable(ctx, db, table); err != nil {
			return err
		}
	}
	return nil
}

func recordTargetFailure(ctx context.Context, db *sql.DB, target config.PrepareTarget, message string) {
	if db == nil {
		return
	}
	_ = metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{
		Key: metadata.BuildKey{
			Region: target.Region, Product: target.Product, Locale: target.Locale,
		},
		BuildName: "",
		State:     metadata.StateFailed,
		Error:     message,
	})
}

func recordTargetNoBuild(ctx context.Context, db *sql.DB, target config.PrepareTarget, message string) {
	if db == nil {
		return
	}
	_ = metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{
		Key: metadata.BuildKey{
			Region: target.Region, Product: target.Product, Locale: target.Locale,
		},
		BuildName: "",
		State:     metadata.StateNoBuild,
		Error:     message,
	})
}

func StartBackground(ctx context.Context, cfg config.Config, db *sql.DB) error {
	go func() {
		if err := (Runner{Config: cfg, DB: db}).Prepare(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "wowdata-server prepare bootstrap failed: %v\n", err)
		}
	}()
	return nil
}

type ProductionDiscoverer struct {
	CacheRoot string
}

func NewProductionDiscoverer(cfg config.Config) ProductionDiscoverer {
	cacheRoot := cfg.Cache.RawDir
	if cacheRoot == "" {
		cacheRoot = cfg.Cache.Root
	}
	return ProductionDiscoverer{CacheRoot: cacheRoot}
}

func (d ProductionDiscoverer) DiscoverBuild(_ context.Context, target config.PrepareTarget) (DiscoveredBuild, error) {
	locale, ok := casc.LocaleFlagByNameOK(target.Locale)
	if !ok {
		return DiscoveredBuild{}, fmt.Errorf("unknown locale %q", target.Locale)
	}
	remote := casc.NewCASCRemote(target.Region)
	remote.Locale = locale
	remote.CacheRoot = d.CacheRoot
	if err := remote.Init(); err != nil {
		return DiscoveredBuild{}, err
	}
	buildIndex := -1
	for i, build := range remote.Builds {
		if build.Product == target.Product {
			buildIndex = i
			break
		}
	}
	if buildIndex < 0 {
		return DiscoveredBuild{NoBuild: true, Error: fmt.Sprintf("no build found for %s/%s", target.Region, target.Product)}, nil
	}
	if err := remote.Preload(buildIndex); err != nil {
		return DiscoveredBuild{}, err
	}
	if remote.GetBuildKey() == "" {
		return DiscoveredBuild{NoBuild: true, Error: fmt.Sprintf("no build key found for %s/%s", target.Region, target.Product)}, nil
	}
	return DiscoveredBuild{
		BuildKey:        remote.GetBuildKey(),
		BuildName:       remote.GetBuildName(),
		CASCBuildConfig: remote.Build.BuildConfig,
		CASCCDNConfig:   remote.Build.CDNConfig,
		FileReader:      remote,
	}, nil
}

type manifestSource interface {
	Manifest() (*dbd.Manifest, error)
}

type dbdDefinitionSource interface {
	Definition(tableName string) (string, error)
}

type ProductionMaterializer struct {
	Config         config.Config
	DB             *sql.DB
	ManifestSource manifestSource
	DBDSource      dbdDefinitionSource
}

func NewProductionMaterializer(cfg config.Config, db *sql.DB) ProductionMaterializer {
	dbdCacheDir := filepath.Join(cfg.Cache.Root, "dbd")
	return ProductionMaterializer{
		Config: cfg,
		DB:     db,
		ManifestSource: appruntime.NewHTTPDBDManifestSource(dbdCacheDir, []string{
			"https://raw.githubusercontent.com/wowdev/WoWDBDefs/refs/heads/master/manifest.json",
		}),
		DBDSource: appruntime.NewHTTPDBDSource(dbdCacheDir, []string{
			"https://raw.githubusercontent.com/wowdev/WoWDBDefs/refs/heads/master/definitions/%s.dbd",
			"https://www.kruithne.net/wow.export/data/dbd/?def=%s",
		}),
	}
}

func (m ProductionMaterializer) MaterializeTable(ctx context.Context, target config.PrepareTarget, build DiscoveredBuild, tableName string) error {
	if m.DB == nil {
		return errors.New("metadata db is required")
	}
	if build.FileReader == nil {
		return errors.New("CASC file reader is required")
	}
	if m.ManifestSource == nil {
		return errors.New("DBD manifest source is required")
	}
	if m.DBDSource == nil {
		return errors.New("DBD definition source is required")
	}
	manifest, err := m.ManifestSource.Manifest()
	if err != nil {
		return err
	}
	fileDataID, ok := manifest.GetByTableName(tableName)
	if !ok {
		return fmt.Errorf("table not found in DBD manifest: %s", tableName)
	}
	rawDBD, err := m.DBDSource.Definition(tableName)
	if err != nil {
		return err
	}
	dbdHash := hashDBD(rawDBD)
	schema, err := schemaForTable(rawDBD, build.BuildName)
	if err != nil {
		return err
	}
	spec := serverparquet.TableSpec{
		Key: metadata.TableKey{
			Region:    target.Region,
			Product:   target.Product,
			Locale:    target.Locale,
			BuildKey:  build.BuildKey,
			TableName: tableName,
		},
		DB2FileDataID:       int(fileDataID),
		DBDHash:             dbdHash,
		DecoderVersion:      decoderVersion,
		MaterializerVersion: materializerVersion,
		ParquetPath:         parquetPath(m.Config, target, build, tableName),
		Schema:              schema,
	}
	decoder := runtimeTableDecoder{
		manifest:  manifest,
		dbdSource: m.DBDSource,
		files:     build.FileReader,
		buildName: build.BuildName,
	}
	_, err = serverparquet.NewMaterializer(m.DB, decoder, serverparquetRowWriter{}).Materialize(ctx, spec)
	return err
}

type runtimeTableDecoder struct {
	manifest  *dbd.Manifest
	dbdSource appruntime.DBDDefinitionSource
	files     appruntime.FileDataReader
	buildName string
}

func (d runtimeTableDecoder) DecodeTable(ctx context.Context, spec serverparquet.TableSpec, _ string) (serverparquet.DecodedTable, error) {
	select {
	case <-ctx.Done():
		return serverparquet.DecodedTable{}, ctx.Err()
	default:
	}
	store := appruntime.NewMemoryDB2Store()
	loader := appruntime.NewDB2Loader(d.manifest, d.dbdSource, d.files, d.buildName)
	if err := loader.LoadTable(store, spec.Key.TableName); err != nil {
		return serverparquet.DecodedTable{}, err
	}
	rows, err := store.Rows(spec.Key.TableName, nil, nil, "", 0)
	if err != nil {
		return serverparquet.DecodedTable{}, err
	}
	return serverparquet.DecodedTable{Rows: rows, Release: func() { rows = nil }}, nil
}

type serverparquetRowWriter struct{}

func (serverparquetRowWriter) WriteRows(path string, meta cacheparquet.Metadata, schema []cacheparquet.Field, rows []map[string]interface{}) error {
	return cacheparquet.WriteRowsFile(path, meta, schema, rows)
}

func schemaForTable(rawDBD string, buildName string) ([]cacheparquet.Field, error) {
	parser, err := dbd.Parse(strings.NewReader(rawDBD))
	if err != nil {
		return nil, err
	}
	entry := parser.GetStructure(buildName, "")
	if entry == nil {
		return nil, fmt.Errorf("no DBD structure for build %s", buildName)
	}
	schema, err := db2.SchemaFromDBD(entry)
	if err != nil {
		return nil, err
	}
	out := make([]cacheparquet.Field, 0, len(schema))
	for _, field := range schema {
		out = append(out, cacheparquet.Field{
			Name:     field.Name,
			Type:     field.Type.SchemaDescription(),
			ArrayLen: field.ArrayLen,
		})
	}
	return out, nil
}

func hashDBD(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func parquetPath(cfg config.Config, target config.PrepareTarget, build DiscoveredBuild, tableName string) string {
	root := cfg.Cache.DB2Dir
	if root == "" {
		root = filepath.Join(cfg.Cache.Root, "db2")
	}
	return filepath.Join(root, target.Region, target.Product, build.BuildKey, target.Locale, tableName+".parquet")
}
