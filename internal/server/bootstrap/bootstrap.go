package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cacheparquet "wowdata/internal/cache/parquet"
	sharedlistfile "wowdata/internal/listfile"
	"wowdata/internal/server/config"
	"wowdata/internal/server/storage/cascindex"
	serverlistfile "wowdata/internal/server/storage/listfile"
	"wowdata/internal/server/storage/metadata"
	serverparquet "wowdata/internal/server/storage/parquet"
	"wowdata/internal/shared/casc"
	"wowdata/internal/shared/db2"
	"wowdata/internal/shared/dbd"
)

const (
	decoderVersion                = "runtime-db2-loader-v3"
	materializerVersion           = "server-prepare-bootstrap-v2"
	manifestUnreadableErrorPrefix = "manifest unreadable: "
	defaultRetryAttempts          = 3
	defaultRetryDelay             = 500 * time.Millisecond
)

var lastTableMemoryReleaseUnixNano int64
var listfileIndexMu sync.Mutex

type Config = config.Config

type DiscoveredBuild struct {
	BuildKey           string
	BuildName          string
	CASCBuildConfig    string
	CASCCDNConfig      string
	NoBuild            bool
	Error              string
	FileReader         fileDataReader
	PrepareTableReader func(context.Context) (DiscoveredBuild, error)
	PrepareResources   func(context.Context) (DiscoveredBuild, error)
	ResourceIndexes    ResourceIndexes
}

type ResourceIndexes struct {
	ListfileSourceHash string
	ListfileEntries    []serverlistfile.Entry
	CASCSource         cascindex.SourceKey
	CASCSourceVersion  string
	CASCRoots          []cascindex.RootMapping
	CASCEncodings      []cascindex.EncodingMapping
	CASCArchives       []cascindex.ArchiveMapping
}

type Discoverer interface {
	DiscoverBuild(context.Context, config.PrepareTarget) (DiscoveredBuild, error)
}

type TableMaterializer interface {
	MaterializeTable(context.Context, config.PrepareTarget, DiscoveredBuild, string) error
}

type TableCatalogMaterializer interface {
	AvailableTables(context.Context) ([]string, error)
}

type TableVersionMaterializer interface {
	CurrentTableVersions() (string, string)
}

type Runner struct {
	Config                  config.Config
	DB                      *sql.DB
	Discoverer              Discoverer
	Materializer            TableMaterializer
	DiscoverRetryAttempts   int
	DiscoverRetryDelay      time.Duration
	RetrySleeper            func(context.Context, time.Duration) error
	AfterTableMemoryRelease func()
}

type discoveredTarget struct {
	Target config.PrepareTarget
	Build  DiscoveredBuild
	Err    error
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
	discovered := make([]discoveredTarget, len(targets))
	for i, target := range targets {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int, target config.PrepareTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			build, err := r.discoverBuild(ctx, target, discoverer)
			discovered[i] = discoveredTarget{Target: target, Build: build, Err: err}
			if err != nil {
				recordTargetFailure(ctx, r.DB, target, err.Error())
				firstErrMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				firstErrMu.Unlock()
				return
			}
			if build.NoBuild || build.BuildKey == "" {
				message := build.Error
				if message == "" {
					message = "no build found"
				}
				recordTargetNoBuild(ctx, r.DB, target, message)
				return
			}
			if build.BuildName == "" {
				build.BuildName = build.BuildKey
			}
			if err := recordDiscoveredPreparing(ctx, r.DB, target, build); err != nil {
				discovered[i].Err = err
				firstErrMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				firstErrMu.Unlock()
			}
		}(i, target)
	}
	wg.Wait()

	sem = make(chan struct{}, maxParallel)
	tableSem := make(chan struct{}, maxTableMaterializations(r.Config.Limits.MaxParallelTableMaterializations))
	wg = sync.WaitGroup{}
	for _, item := range discovered {
		if item.Err != nil || item.Build.NoBuild || item.Build.BuildKey == "" {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(item discoveredTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			fmt.Fprintf(os.Stderr, "wowdata-server prepare target materialization starting: %s %s/%s/%s build=%s\n", item.Target.Label, item.Target.Region, item.Target.Product, item.Target.Locale, item.Build.BuildKey)
			if err := r.materializeTarget(ctx, item.Target, item.Build, materializer, tableSem); err != nil {
				fmt.Fprintf(os.Stderr, "wowdata-server prepare target materialization failed: %s %s/%s/%s build=%s: %v\n", item.Target.Label, item.Target.Region, item.Target.Product, item.Target.Locale, item.Build.BuildKey, err)
				firstErrMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				firstErrMu.Unlock()
				return
			}
			fmt.Fprintf(os.Stderr, "wowdata-server prepare target materialization finished: %s %s/%s/%s build=%s\n", item.Target.Label, item.Target.Region, item.Target.Product, item.Target.Locale, item.Build.BuildKey)
		}(item)
	}
	wg.Wait()
	return firstErr
}

func maxTableMaterializations(configured int) int {
	if configured <= 0 {
		return 1
	}
	return configured
}

func (r Runner) materializeTarget(ctx context.Context, target config.PrepareTarget, build DiscoveredBuild, materializer TableMaterializer, tableSem chan struct{}) error {
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
	allManifestTables := isAllManifestTables(r.Config.Prepare.DefaultTables)
	requiredTables, err := ResolveRequiredTables(ctx, r.Config.Prepare.DefaultTables, materializer)
	if err != nil {
		return err
	}
	if len(requiredTables) == 0 {
		if sameActiveValid {
			return nil
		}
		if allManifestTables {
			err := errors.New("no readable DB2 tables materialized")
			_ = metadata.MarkBuildFailed(ctx, r.DB, buildKey, err.Error())
			return err
		}
		return nil
	}
	tablesToMaterialize := requiredTables
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
		if allManifestTables {
			knownUnreadable, err := knownManifestUnreadableTables(ctx, r.DB, buildKey)
			if err != nil {
				return err
			}
			tables = append(tables, knownUnreadable...)
		}
		desiredDecoderVersion, desiredMaterializerVersion := currentTableVersions(materializer)
		tablesToMaterialize = missingDefaultTables(requiredTables, tables, desiredDecoderVersion, desiredMaterializerVersion)
		if len(tablesToMaterialize) == 0 {
			ready, err := r.resourceIndexesReady(ctx, buildKey)
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
			if build.PrepareResources == nil {
				return nil
			}
			if _, err := r.prepareBuildResources(ctx, build, buildKey, sameActiveValid, restoreTables); err != nil {
				return err
			}
			ready, err = r.resourceIndexesReady(ctx, buildKey)
			if err != nil {
				return err
			}
			if !ready {
				return errors.New("resource indexes missing after preparation")
			}
			return nil
		}
	}
	preparedBuild, err := r.prepareBuildForTableMaterialization(ctx, build, buildKey, sameActiveValid, restoreTables)
	if err != nil {
		return err
	}
	build = preparedBuild
	if build.PrepareResources != nil {
		build.PrepareResources = nil
	}
	if !sameActiveValid {
		if err := metadata.UpsertDiscoveredBuild(ctx, r.DB, metadata.Build{
			Key:       buildKey,
			BuildName: build.BuildName,
			State:     metadata.StatePreparing,
		}); err != nil {
			return err
		}
	}

	if err := r.materializeTables(ctx, target, build, buildKey, tablesToMaterialize, materializer, !sameActiveValid, tableSem, allManifestTables); err != nil {
		if sameActiveValid {
			_ = restoreValidTables(ctx, r.DB, restoreTables)
			_ = metadata.MarkBuildReady(ctx, r.DB, buildKey)
			return err
		}
		_ = metadata.MarkBuildFailed(ctx, r.DB, buildKey, err.Error())
		return err
	}
	if allManifestTables {
		validTables, err := metadata.ListValidMaterializedTables(ctx, r.DB, metadata.TableCatalogLookup{
			Region:   target.Region,
			Product:  target.Product,
			Locale:   target.Locale,
			BuildKey: build.BuildKey,
		})
		if err != nil {
			return err
		}
		if len(validTables) == 0 {
			err := errors.New("no readable DB2 tables materialized")
			_ = metadata.MarkBuildFailed(ctx, r.DB, buildKey, err.Error())
			return err
		}
	}
	if err := metadata.MarkBuildReady(ctx, r.DB, buildKey); err != nil {
		return err
	}
	return metadata.ActivateBuild(ctx, r.DB, buildKey)
}

func (r Runner) prepareBuildForTableMaterialization(ctx context.Context, build DiscoveredBuild, buildKey metadata.BuildKey, sameActiveValid bool, restoreTables []metadata.MaterializedTable) (DiscoveredBuild, error) {
	if sameActiveValid {
		ready, err := r.resourceIndexesReady(ctx, buildKey)
		if err != nil {
			return build, err
		}
		if ready {
			if build.FileReader != nil {
				return build, nil
			}
			if build.PrepareTableReader != nil {
				prepared, err := build.PrepareTableReader(ctx)
				if err != nil {
					_ = restoreValidTables(ctx, r.DB, restoreTables)
					_ = metadata.MarkBuildReady(ctx, r.DB, buildKey)
					return build, err
				}
				prepared.PrepareResources = nil
				prepared.PrepareTableReader = nil
				if prepared.BuildKey == "" {
					prepared.BuildKey = build.BuildKey
				}
				if prepared.BuildName == "" {
					prepared.BuildName = build.BuildName
				}
				return prepared, nil
			}
		}
	}
	return r.prepareBuildResources(ctx, build, buildKey, sameActiveValid, restoreTables)
}

func (r Runner) resourceIndexesReady(ctx context.Context, buildKey metadata.BuildKey) (bool, error) {
	listfileReady, err := serverlistfile.HasUsableSource(ctx, r.DB)
	if err != nil {
		return false, err
	}
	if !listfileReady {
		return false, nil
	}
	return cascindex.HasUsableIndexForSource(ctx, r.DB, cascindex.SourceKey{
		Region:   buildKey.Region,
		Product:  buildKey.Product,
		Locale:   buildKey.Locale,
		BuildKey: buildKey.BuildKey,
	})
}

func (r Runner) prepareBuildResources(ctx context.Context, build DiscoveredBuild, buildKey metadata.BuildKey, sameActiveValid bool, restoreTables []metadata.MaterializedTable) (DiscoveredBuild, error) {
	if build.PrepareResources == nil {
		return build, nil
	}
	if !sameActiveValid {
		_ = metadata.MarkBuildPreparingMessage(ctx, r.DB, buildKey, "resource preparation started")
	}
	prepared, err := build.PrepareResources(ctx)
	if err != nil {
		if sameActiveValid {
			_ = restoreValidTables(ctx, r.DB, restoreTables)
			_ = metadata.MarkBuildReady(ctx, r.DB, buildKey)
			return build, err
		}
		_ = metadata.MarkBuildFailed(ctx, r.DB, buildKey, err.Error())
		return build, err
	}
	prepared.PrepareResources = nil
	if prepared.BuildKey == "" {
		prepared.BuildKey = build.BuildKey
	}
	if prepared.BuildName == "" {
		prepared.BuildName = build.BuildName
	}
	if err := persistResourceIndexes(ctx, r.DB, prepared.ResourceIndexes); err != nil {
		if sameActiveValid {
			_ = restoreValidTables(ctx, r.DB, restoreTables)
			_ = metadata.MarkBuildReady(ctx, r.DB, buildKey)
			return build, err
		}
		_ = metadata.MarkBuildFailed(ctx, r.DB, buildKey, err.Error())
		return build, err
	}
	return prepared, nil
}

func (r Runner) materializeTables(ctx context.Context, target config.PrepareTarget, build DiscoveredBuild, buildKey metadata.BuildKey, tables []string, materializer TableMaterializer, markBuildProgress bool, tableSem chan struct{}, skipUnavailableBuildStructure bool) error {
	maxParallel := maxTableMaterializations(r.Config.Limits.MaxParallelTableMaterializations)
	if maxParallel > len(tables) {
		maxParallel = len(tables)
	}
	if maxParallel <= 1 {
		for _, tableName := range tables {
			if err := r.materializeOneTable(ctx, target, build, buildKey, tableName, materializer, markBuildProgress, tableSem, skipUnavailableBuildStructure); err != nil {
				return err
			}
		}
		return nil
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan string)
	var wg sync.WaitGroup
	var firstErr error
	var firstErrMu sync.Mutex
	recordErr := func(err error) {
		if err == nil {
			return
		}
		firstErrMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		firstErrMu.Unlock()
	}
	for i := 0; i < maxParallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tableName := range jobs {
				if err := r.materializeOneTable(workCtx, target, build, buildKey, tableName, materializer, markBuildProgress, tableSem, skipUnavailableBuildStructure); err != nil {
					recordErr(err)
				}
			}
		}()
	}
sendJobs:
	for _, tableName := range tables {
		select {
		case <-workCtx.Done():
			break sendJobs
		case jobs <- tableName:
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return workCtx.Err()
}

func (r Runner) materializeOneTable(ctx context.Context, target config.PrepareTarget, build DiscoveredBuild, buildKey metadata.BuildKey, tableName string, materializer TableMaterializer, markBuildProgress bool, tableSem chan struct{}, skipUnavailableBuildStructure bool) error {
	if tableSem != nil {
		select {
		case tableSem <- struct{}{}:
			defer func() { <-tableSem }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if markBuildProgress {
		_ = metadata.MarkBuildPreparingMessage(ctx, r.DB, buildKey, "materializing "+tableName)
	}
	defer r.releaseTableMemory()
	fmt.Fprintf(os.Stderr, "wowdata-server prepare materializing table: %s %s/%s/%s build=%s table=%s\n", target.Label, target.Region, target.Product, target.Locale, build.BuildKey, tableName)
	err := materializer.MaterializeTable(ctx, target, build, tableName)
	if err != nil && skipUnavailableBuildStructure && isManifestUnreadableTableError(err) {
		if markErr := metadata.UpsertMaterializedTable(ctx, r.DB, metadata.MaterializedTable{
			Key: metadata.TableKey{
				Region:    target.Region,
				Product:   target.Product,
				Locale:    target.Locale,
				BuildKey:  build.BuildKey,
				TableName: tableName,
			},
			MaterializerVersion: materializerVersion,
			State:               metadata.StateFailed,
			Error:               manifestUnreadableErrorPrefix + err.Error(),
		}); markErr != nil {
			return markErr
		}
		fmt.Fprintf(os.Stderr, "wowdata-server prepare skipping unreadable table: %s %s/%s/%s build=%s table=%s error=%v\n", target.Label, target.Region, target.Product, target.Locale, build.BuildKey, tableName, err)
		return nil
	}
	return err
}

func (r Runner) releaseTableMemory() {
	release := r.AfterTableMemoryRelease
	if release == nil {
		release = releaseTableMemory
	}
	release()
}

func releaseTableMemory() {
	const minInterval = 2 * time.Second
	now := time.Now()
	last := atomic.LoadInt64(&lastTableMemoryReleaseUnixNano)
	if last != 0 && now.Sub(time.Unix(0, last)) < minInterval {
		return
	}
	if !atomic.CompareAndSwapInt64(&lastTableMemoryReleaseUnixNano, last, now.UnixNano()) {
		return
	}
	runtime.GC()
	debug.FreeOSMemory()
}

func knownManifestUnreadableTables(ctx context.Context, db *sql.DB, buildKey metadata.BuildKey) ([]metadata.MaterializedTable, error) {
	tables, err := metadata.ListFailedMaterializedTablesWithErrorPrefix(ctx, db, metadata.TableCatalogLookup{
		Region:   buildKey.Region,
		Product:  buildKey.Product,
		Locale:   buildKey.Locale,
		BuildKey: buildKey.BuildKey,
	}, manifestUnreadableErrorPrefix)
	if err != nil {
		return nil, err
	}
	var current []metadata.MaterializedTable
	for _, table := range tables {
		if table.MaterializerVersion == materializerVersion {
			current = append(current, table)
		}
	}
	return current, nil
}

func isManifestUnreadableTableError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "no DBD structure for build ") ||
		strings.Contains(message, "Invalid DBD:") ||
		strings.Contains(message, "fileDataID does not exist in root:") ||
		strings.Contains(message, "no root entry found for locale:")
}

func ResolveRequiredTables(ctx context.Context, configured []string, materializer TableMaterializer) ([]string, error) {
	if len(configured) > 0 && !isAllManifestTables(configured) {
		return configured, nil
	}
	catalog, ok := materializer.(TableCatalogMaterializer)
	if !ok {
		return nil, nil
	}
	tables, err := catalog.AvailableTables(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(tables)
	return tables, nil
}

func isAllManifestTables(configured []string) bool {
	return len(configured) == 1 && configured[0] == "*"
}

func missingDefaultTables(defaultTables []string, validTables []metadata.MaterializedTable, desiredDecoderVersion string, desiredMaterializerVersion string) []string {
	validByName := make(map[string]bool, len(validTables))
	for _, table := range validTables {
		if table.State != metadata.StateValid {
			validByName[table.Key.TableName] = true
			continue
		}
		validByName[table.Key.TableName] = table.DecoderVersion == desiredDecoderVersion && table.MaterializerVersion == desiredMaterializerVersion
	}
	var missing []string
	for _, tableName := range defaultTables {
		if !validByName[tableName] {
			missing = append(missing, tableName)
		}
	}
	return missing
}

func currentTableVersions(materializer TableMaterializer) (string, string) {
	if versioned, ok := materializer.(TableVersionMaterializer); ok {
		return versioned.CurrentTableVersions()
	}
	return decoderVersion, materializerVersion
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

func recordDiscoveredPreparing(ctx context.Context, db *sql.DB, target config.PrepareTarget, build DiscoveredBuild) error {
	buildKey := metadata.BuildKey{
		Region:   target.Region,
		Product:  target.Product,
		Locale:   target.Locale,
		BuildKey: build.BuildKey,
	}
	active, err := metadata.ActiveBuild(ctx, db, target.Region, target.Product, target.Locale)
	if err == nil && active.Key.BuildKey == build.BuildKey && active.State == metadata.StateValid {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return metadata.UpsertDiscoveredBuild(ctx, db, metadata.Build{
		Key:       buildKey,
		BuildName: build.BuildName,
		State:     metadata.StatePreparing,
		Error:     "build discovered, waiting for materialization",
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
	CacheRoot       string
	CASCLimitBytes  int64
	CASCTargetBytes int64
}

func NewProductionDiscoverer(cfg config.Config) ProductionDiscoverer {
	cacheRoot := cfg.Cache.RawDir
	if cacheRoot == "" {
		cacheRoot = cfg.Cache.Root
	}
	return ProductionDiscoverer{
		CacheRoot:       cacheRoot,
		CASCLimitBytes:  cfg.Cache.CASCDiskLimitMB * 1024 * 1024,
		CASCTargetBytes: cfg.Cache.CASCDiskTargetMB * 1024 * 1024,
	}
}

func (d ProductionDiscoverer) DiscoverBuild(_ context.Context, target config.PrepareTarget) (DiscoveredBuild, error) {
	locale, ok := casc.LocaleFlagByNameOK(target.Locale)
	if !ok {
		return DiscoveredBuild{}, fmt.Errorf("unknown locale %q", target.Locale)
	}
	remote := casc.NewCASCRemote(target.Region)
	remote.Products = httpServerDiscoveryProducts(target)
	remote.Locale = locale
	remote.CacheRoot = d.CacheRoot
	remote.CacheLimitBytes = d.CASCLimitBytes
	remote.CacheTargetBytes = d.CASCTargetBytes
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
	version := remote.Builds[buildIndex]
	buildKey := version.BuildConfig
	if buildKey == "" {
		buildKey = version.BuildKey
	}
	if buildKey == "" {
		return DiscoveredBuild{NoBuild: true, Error: fmt.Sprintf("no build key found for %s/%s", target.Region, target.Product)}, nil
	}
	prepareTableReader := func(context.Context) (DiscoveredBuild, error) {
		if err := remote.Preload(buildIndex); err != nil {
			return DiscoveredBuild{}, err
		}
		return DiscoveredBuild{
			BuildKey:        remote.GetBuildKey(),
			BuildName:       remote.GetBuildName(),
			CASCBuildConfig: remote.Build.BuildConfig,
			CASCCDNConfig:   remote.Build.CDNConfig,
			FileReader:      remote,
		}, nil
	}
	return DiscoveredBuild{
		BuildKey:           buildKey,
		BuildName:          version.VersionsName,
		CASCBuildConfig:    version.BuildConfig,
		CASCCDNConfig:      version.CDNConfig,
		PrepareTableReader: prepareTableReader,
		PrepareResources: func(ctx context.Context) (DiscoveredBuild, error) {
			prepared, err := prepareTableReader(ctx)
			if err != nil {
				return DiscoveredBuild{}, err
			}
			indexes, err := buildProductionResourceIndexes(d.CacheRoot, target, prepared, remote.CASCSource)
			if err != nil {
				return DiscoveredBuild{}, err
			}
			prepared.ResourceIndexes = indexes
			return prepared, nil
		},
	}, nil
}

func httpServerDiscoveryProducts(target config.PrepareTarget) []string {
	return []string{target.Product}
}

func persistResourceIndexes(ctx context.Context, db *sql.DB, indexes ResourceIndexes) error {
	if db == nil {
		return errors.New("metadata db is required")
	}
	if len(indexes.ListfileEntries) > 0 {
		if err := persistListfileIndex(ctx, db, indexes.ListfileSourceHash, indexes.ListfileEntries); err != nil {
			return err
		}
	}
	if indexes.CASCSourceVersion != "" {
		if err := cascindex.ReplaceIndexForSource(ctx, db, indexes.CASCSource, indexes.CASCSourceVersion, indexes.CASCRoots, indexes.CASCEncodings, indexes.CASCArchives); err != nil {
			return err
		}
	}
	return nil
}

func persistListfileIndex(ctx context.Context, db *sql.DB, sourceHash string, entries []serverlistfile.Entry) error {
	listfileIndexMu.Lock()
	defer listfileIndexMu.Unlock()
	currentHash, err := serverlistfile.SourceHash(ctx, db)
	if err == nil && currentHash == sourceHash {
		return nil
	}
	return serverlistfile.ReplaceSource(ctx, db, sourceHash, entries)
}

func buildProductionResourceIndexes(cacheRoot string, target config.PrepareTarget, build DiscoveredBuild, source *casc.CASCSource) (ResourceIndexes, error) {
	lf, err := loadServerListfile(filepath.Join(cacheRoot, "listfile"))
	if err != nil {
		return ResourceIndexes{}, err
	}
	entries := serverListfileEntries(lf.GetAll())
	locale := casc.LocaleFlagByName(target.Locale)
	roots, encodings, archives := cascResourceMappingsForLocale(source, locale)
	return ResourceIndexes{
		ListfileSourceHash: hashListfileEntries(entries),
		ListfileEntries:    entries,
		CASCSource: cascindex.SourceKey{
			Region:   target.Region,
			Product:  target.Product,
			Locale:   target.Locale,
			BuildKey: build.BuildKey,
		},
		CASCSourceVersion: hashCASCResourceMappings(roots, encodings, archives),
		CASCRoots:         roots,
		CASCEncodings:     encodings,
		CASCArchives:      archives,
	}, nil
}

func loadServerListfile(cacheDir string) (*sharedlistfile.Listfile, error) {
	lf := sharedlistfile.New()
	if err := lf.LoadBinaryDir(cacheDir); err == nil {
		return lf, nil
	}
	cachePath := filepath.Join(cacheDir, "community-listfile.csv")
	if body, err := os.ReadFile(cachePath); err == nil && len(body) > 0 {
		return parseServerListfile(body)
	}
	var lastErr error
	for _, url := range []string{
		"https://github.com/wowdev/wow-listfile/releases/latest/download/community-listfile.csv",
		"https://www.kruithne.net/wow.export/data/listfile/master",
	} {
		body, err := downloadServerListfile(url)
		if err != nil {
			lastErr = err
			continue
		}
		lf, err := parseServerListfile(body)
		if err != nil {
			lastErr = err
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
			return nil, err
		}
		_ = os.WriteFile(cachePath, body, 0644)
		return lf, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no listfile URLs configured")
	}
	return nil, lastErr
}

func parseServerListfile(body []byte) (*sharedlistfile.Listfile, error) {
	lf := sharedlistfile.New()
	if err := lf.Load(bytes.NewReader(body)); err != nil {
		return nil, err
	}
	return lf, nil
}

func downloadServerListfile(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download listfile %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func serverListfileEntries(values map[uint32]string) []serverlistfile.Entry {
	ids := make([]int, 0, len(values))
	for id := range values {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	entries := make([]serverlistfile.Entry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, serverlistfile.Entry{FileDataID: uint32(id), Path: values[uint32(id)]})
	}
	return entries
}

func hashListfileEntries(entries []serverlistfile.Entry) string {
	hash := sha256.New()
	for _, entry := range entries {
		_, _ = fmt.Fprintf(hash, "%d\x00%s\x00", entry.FileDataID, entry.Path)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func hashCASCResourceMappings(roots []cascindex.RootMapping, encodings []cascindex.EncodingMapping, archives []cascindex.ArchiveMapping) string {
	hash := sha256.New()
	for _, mapping := range roots {
		_, _ = fmt.Fprintf(hash, "r:%d:%s\x00", mapping.FileDataID, mapping.ContentKey)
	}
	for _, mapping := range encodings {
		_, _ = fmt.Fprintf(hash, "e:%s:%s:%d\x00", mapping.ContentKey, mapping.EncodingKey, mapping.Size)
	}
	for _, mapping := range archives {
		_, _ = fmt.Fprintf(hash, "a:%s:%s:%d:%d\x00", mapping.EncodingKey, mapping.ArchiveKey, mapping.Offset, mapping.Size)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cascResourceMappingsForLocale(source *casc.CASCSource, locale casc.LocaleFlag) ([]cascindex.RootMapping, []cascindex.EncodingMapping, []cascindex.ArchiveMapping) {
	roots := cascRootMappingsForLocale(source, locale)
	encodings := cascEncodingMappingsForRoots(source, roots)
	archives := cascArchiveMappingsForEncodings(source, encodings)
	encodings = cascEncodingMappingsForArchives(encodings, archives)
	roots = cascRootMappingsForEncodings(roots, encodings)
	return roots, encodings, archives
}

func cascRootMappings(source *casc.CASCSource) []cascindex.RootMapping {
	if source == nil {
		return nil
	}
	return cascRootMappingsForLocale(source, source.Locale)
}

func cascRootMappingsForLocale(source *casc.CASCSource, locale casc.LocaleFlag) []cascindex.RootMapping {
	if source == nil {
		return nil
	}
	ids := make([]int, 0, len(source.RootEntries))
	for id := range source.RootEntries {
		ids = append(ids, int(id))
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	mappings := make([]cascindex.RootMapping, 0, len(ids))
	for _, id := range ids {
		contentKey := rootContentKeyForLocale(source, uint32(id), locale)
		if contentKey == "" {
			continue
		}
		mappings = append(mappings, cascindex.RootMapping{FileDataID: uint32(id), ContentKey: contentKey})
	}
	return mappings
}

func rootContentKeyForLocale(source *casc.CASCSource, fileDataID uint32, locale casc.LocaleFlag) string {
	rootEntries, ok := source.RootEntries[fileDataID]
	if !ok {
		return ""
	}
	for _, entry := range rootEntries {
		if entry.TypeIndex >= len(source.RootTypes) {
			continue
		}
		rootType := source.RootTypes[entry.TypeIndex]
		if rootType.LocaleFlags&locale != 0 && rootType.ContentFlags&casc.ContentLowViolence == 0 {
			return entry.ContentKey
		}
	}
	return ""
}

func cascEncodingMappings(source *casc.CASCSource) []cascindex.EncodingMapping {
	if source == nil {
		return nil
	}
	keys := make([]string, 0, len(source.EncodingEntries))
	for contentKey := range source.EncodingEntries {
		keys = append(keys, contentKey)
	}
	sort.Strings(keys)
	mappings := make([]cascindex.EncodingMapping, 0, len(keys))
	for _, contentKey := range keys {
		entry := source.EncodingEntries[contentKey]
		mappings = append(mappings, cascindex.EncodingMapping{
			ContentKey:  contentKey,
			EncodingKey: entry.Key,
			Size:        entry.Size,
		})
	}
	return mappings
}

func cascEncodingMappingsForRoots(source *casc.CASCSource, roots []cascindex.RootMapping) []cascindex.EncodingMapping {
	if source == nil {
		return nil
	}
	keys := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if _, ok := seen[root.ContentKey]; ok {
			continue
		}
		if _, ok := source.EncodingEntries[root.ContentKey]; !ok {
			continue
		}
		seen[root.ContentKey] = struct{}{}
		keys = append(keys, root.ContentKey)
	}
	sort.Strings(keys)
	mappings := make([]cascindex.EncodingMapping, 0, len(keys))
	for _, contentKey := range keys {
		entry := source.EncodingEntries[contentKey]
		mappings = append(mappings, cascindex.EncodingMapping{
			ContentKey:  contentKey,
			EncodingKey: entry.Key,
			Size:        entry.Size,
		})
	}
	return mappings
}

func cascEncodingMappingsForArchives(encodings []cascindex.EncodingMapping, archives []cascindex.ArchiveMapping) []cascindex.EncodingMapping {
	archiveKeys := make(map[string]struct{}, len(archives))
	for _, archive := range archives {
		archiveKeys[archive.EncodingKey] = struct{}{}
	}
	filtered := make([]cascindex.EncodingMapping, 0, len(encodings))
	for _, encoding := range encodings {
		if _, ok := archiveKeys[encoding.EncodingKey]; ok {
			filtered = append(filtered, encoding)
		}
	}
	return filtered
}

func cascRootMappingsForEncodings(roots []cascindex.RootMapping, encodings []cascindex.EncodingMapping) []cascindex.RootMapping {
	contentKeys := make(map[string]struct{}, len(encodings))
	for _, encoding := range encodings {
		contentKeys[encoding.ContentKey] = struct{}{}
	}
	filtered := make([]cascindex.RootMapping, 0, len(roots))
	for _, root := range roots {
		if _, ok := contentKeys[root.ContentKey]; ok {
			filtered = append(filtered, root)
		}
	}
	return filtered
}

func cascArchiveMappings(source *casc.CASCSource) []cascindex.ArchiveMapping {
	if source == nil {
		return nil
	}
	keys := make([]string, 0, len(source.Archives))
	for encodingKey := range source.Archives {
		keys = append(keys, encodingKey)
	}
	sort.Strings(keys)
	mappings := make([]cascindex.ArchiveMapping, 0, len(keys))
	for _, encodingKey := range keys {
		archive := source.Archives[encodingKey]
		mappings = append(mappings, cascindex.ArchiveMapping{
			EncodingKey: encodingKey,
			ArchiveKey:  archive.Key,
			Offset:      int64(archive.Offset),
			Size:        int64(archive.Size),
		})
	}
	return mappings
}

func cascArchiveMappingsForEncodings(source *casc.CASCSource, encodings []cascindex.EncodingMapping) []cascindex.ArchiveMapping {
	if source == nil {
		return nil
	}
	keys := make([]string, 0, len(encodings))
	seen := make(map[string]struct{}, len(encodings))
	for _, encoding := range encodings {
		if _, ok := seen[encoding.EncodingKey]; ok {
			continue
		}
		if _, ok := source.Archives[encoding.EncodingKey]; !ok {
			continue
		}
		seen[encoding.EncodingKey] = struct{}{}
		keys = append(keys, encoding.EncodingKey)
	}
	sort.Strings(keys)
	mappings := make([]cascindex.ArchiveMapping, 0, len(keys))
	for _, encodingKey := range keys {
		archive := source.Archives[encodingKey]
		mappings = append(mappings, cascindex.ArchiveMapping{
			EncodingKey: encodingKey,
			ArchiveKey:  archive.Key,
			Offset:      int64(archive.Offset),
			Size:        int64(archive.Size),
		})
	}
	return mappings
}

type manifestSource interface {
	Manifest() (*dbd.Manifest, error)
}

type dbdDefinitionSource interface {
	Definition(tableName string) (string, error)
}

type fileDataReader interface {
	ReadFileData(fileDataID uint32) ([]byte, error)
}

type partialFileDataReader interface {
	ReadFileDataPartial(fileDataID uint32) ([]byte, error)
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
		ManifestSource: newHTTPDBDManifestSource(dbdCacheDir, []string{
			"https://raw.githubusercontent.com/wowdev/WoWDBDefs/refs/heads/master/manifest.json",
		}),
		DBDSource: newHTTPDBDSource(dbdCacheDir, []string{
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
	data, err := readDB2FileData(build.FileReader, fileDataID)
	if err != nil {
		return err
	}
	layoutHash, err := db2LayoutHash(data)
	if err != nil {
		return err
	}
	dbdHash := hashDBD(rawDBD)
	schema, err := schemaForTable(rawDBD, build.BuildName, layoutHash)
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
		manifest:   manifest,
		dbdSource:  m.DBDSource,
		files:      fixedFileDataReader{data: data},
		buildName:  build.BuildName,
		layoutHash: layoutHash,
	}
	_, err = serverparquet.NewMaterializer(m.DB, decoder, serverparquetRowWriter{}).Materialize(ctx, spec)
	return err
}

func (m ProductionMaterializer) CurrentTableVersions() (string, string) {
	return decoderVersion, materializerVersion
}

func (m ProductionMaterializer) AvailableTables(ctx context.Context) ([]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if m.ManifestSource == nil {
		return nil, errors.New("DBD manifest source is required")
	}
	manifest, err := m.ManifestSource.Manifest()
	if err != nil {
		return nil, err
	}
	return manifest.TableNames(), nil
}

type runtimeTableDecoder struct {
	manifest   *dbd.Manifest
	dbdSource  dbdDefinitionSource
	files      fileDataReader
	buildName  string
	layoutHash string
}

func (d runtimeTableDecoder) DecodeTable(ctx context.Context, spec serverparquet.TableSpec, _ string) (serverparquet.DecodedTable, error) {
	select {
	case <-ctx.Done():
		return serverparquet.DecodedTable{}, ctx.Err()
	default:
	}
	fileDataID, ok := d.manifest.GetByTableName(spec.Key.TableName)
	if !ok {
		return serverparquet.DecodedTable{}, fmt.Errorf("table not found in DBD manifest: %s", spec.Key.TableName)
	}
	var data []byte
	var err error
	if partialReader, ok := d.files.(partialFileDataReader); ok {
		data, err = partialReader.ReadFileDataPartial(fileDataID)
	} else {
		data, err = d.files.ReadFileData(fileDataID)
	}
	if err != nil {
		return serverparquet.DecodedTable{}, err
	}
	rawDBD, err := d.dbdSource.Definition(spec.Key.TableName)
	if err != nil {
		return serverparquet.DecodedTable{}, err
	}
	layoutHash := d.layoutHash
	if layoutHash == "" {
		layoutHash, err = db2LayoutHash(data)
		if err != nil {
			return serverparquet.DecodedTable{}, err
		}
	}
	schema, err := db2SchemaForRuntime(rawDBD, d.buildName, layoutHash)
	if err != nil {
		return serverparquet.DecodedTable{}, err
	}
	reader, err := db2.NewWDCReaderFromBytes(spec.Key.TableName, data, schema)
	if err != nil {
		return serverparquet.DecodedTable{}, err
	}
	rowSource, err := newWDCRowSource(reader)
	if err != nil {
		return serverparquet.DecodedTable{}, err
	}
	return serverparquet.DecodedTable{RowSource: rowSource, Release: func() { _ = rowSource.Close() }}, nil
}

type serverparquetRowWriter struct{}

func (serverparquetRowWriter) WriteRows(path string, meta cacheparquet.Metadata, schema []cacheparquet.Field, rows []map[string]interface{}) error {
	return cacheparquet.WriteRowsFile(path, meta, schema, rows)
}

func (serverparquetRowWriter) WriteRowSource(path string, meta cacheparquet.Metadata, schema []cacheparquet.Field, rows serverparquet.RowSource) (int, error) {
	return cacheparquet.WriteRowSourceFile(path, meta, schema, rows)
}

type wdcRowSource struct {
	rows chan wdcRowResult
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

type wdcRowResult struct {
	row map[string]interface{}
	err error
}

func newWDCRowSource(reader *db2.WDCReader) (*wdcRowSource, error) {
	source := &wdcRowSource{
		rows: make(chan wdcRowResult, 1),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go source.streamRows(reader)
	return source, nil
}

var errWDCRowSourceClosed = errors.New("WDC row source closed")

func (s *wdcRowSource) streamRows(reader *db2.WDCReader) {
	defer close(s.done)
	defer close(s.rows)
	err := reader.ForEachRow(func(recordID uint32, row map[string]interface{}) error {
		row = rowWithSyntheticID(row, recordID)
		select {
		case <-s.stop:
			return errWDCRowSourceClosed
		case s.rows <- wdcRowResult{row: row}:
			return nil
		}
	})
	if err == nil || errors.Is(err, errWDCRowSourceClosed) {
		return
	}
	select {
	case <-s.stop:
	case s.rows <- wdcRowResult{err: err}:
	}
}

func (s *wdcRowSource) NextRow() (map[string]interface{}, bool, error) {
	result, ok := <-s.rows
	if !ok {
		return nil, false, nil
	}
	if result.err != nil {
		return nil, false, result.err
	}
	return result.row, true, nil
}

func (s *wdcRowSource) Close() error {
	s.once.Do(func() {
		close(s.stop)
		<-s.done
	})
	return nil
}

func schemaForTable(rawDBD string, buildName string, layoutHash ...string) ([]cacheparquet.Field, error) {
	schema, err := db2SchemaForRuntime(rawDBD, buildName, optionalLayoutHash(layoutHash))
	if err != nil {
		return nil, err
	}
	schema = db2.SchemaWithSyntheticID(schema)
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

func rowWithSyntheticID(row map[string]interface{}, recordID uint32) map[string]interface{} {
	if row == nil {
		return nil
	}
	if _, ok := row["ID"]; ok {
		return row
	}
	out := make(map[string]interface{}, len(row)+1)
	out["ID"] = recordID
	for key, value := range row {
		out[key] = value
	}
	return out
}

func db2SchemaForRuntime(rawDBD string, buildName string, layoutHash string) ([]db2.SchemaField, error) {
	parser, err := dbd.Parse(strings.NewReader(rawDBD))
	if err != nil {
		return nil, err
	}
	entry := parser.GetStructure(buildName, layoutHash)
	if entry == nil {
		return nil, fmt.Errorf("no DBD structure for build %s", buildName)
	}
	schema, err := db2.SchemaFromDBD(entry)
	if err != nil {
		return nil, err
	}
	return schema, nil
}

func optionalLayoutHash(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

type fixedFileDataReader struct {
	data []byte
}

func (r fixedFileDataReader) ReadFileData(uint32) ([]byte, error) {
	return r.data, nil
}

func readDB2FileData(files fileDataReader, fileDataID uint32) ([]byte, error) {
	if partialReader, ok := files.(partialFileDataReader); ok {
		return partialReader.ReadFileDataPartial(fileDataID)
	}
	return files.ReadFileData(fileDataID)
}

func db2LayoutHash(data []byte) (string, error) {
	if len(data) < 24 {
		return "", fmt.Errorf("DB2 data too short for layout hash: %d bytes", len(data))
	}
	pos := 4
	magic := binary.LittleEndian.Uint32(data[:4])
	if magic == 0x35434457 {
		if len(data) < 156 {
			return "", fmt.Errorf("WDC5 data too short for layout hash: %d bytes", len(data))
		}
		pos += 4 + 128
	}
	pos += 4 // recordCount
	pos += 4 // fieldCount
	pos += 4 // recordSize
	pos += 4 // stringTableSize
	pos += 4 // tableHash
	if pos+4 > len(data) {
		return "", fmt.Errorf("DB2 data too short for layout hash: %d bytes", len(data))
	}
	return strings.ToUpper(fmt.Sprintf("%02x%02x%02x%02x", data[pos+3], data[pos+2], data[pos+1], data[pos])), nil
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

type httpDBDManifestSource struct {
	cacheDir string
	urls     []string
	client   *http.Client
}

func newHTTPDBDManifestSource(cacheDir string, urls []string) *httpDBDManifestSource {
	return &httpDBDManifestSource{cacheDir: cacheDir, urls: urls, client: http.DefaultClient}
}

func (s *httpDBDManifestSource) Manifest() (*dbd.Manifest, error) {
	cachePath := filepath.Join(s.cacheDir, "dbd-manifest.json")
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		return dbd.ParseManifest(strings.NewReader(string(data)))
	}
	var lastErr error
	for _, url := range s.urls {
		resp, err := s.client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			continue
		}
		manifest, err := dbd.ParseManifest(strings.NewReader(string(body)))
		if err != nil {
			lastErr = err
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
			return nil, err
		}
		_ = os.WriteFile(cachePath, body, 0644)
		return manifest, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DBD manifest URLs configured")
	}
	return nil, lastErr
}

type httpDBDSource struct {
	cacheDir string
	urls     []string
	client   *http.Client
}

func newHTTPDBDSource(cacheDir string, urls []string) *httpDBDSource {
	return &httpDBDSource{cacheDir: cacheDir, urls: urls, client: http.DefaultClient}
}

func (s *httpDBDSource) Definition(tableName string) (string, error) {
	cachePath := filepath.Join(s.cacheDir, tableName+".dbd")
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		return string(data), nil
	}
	var lastErr error
	for _, tmpl := range s.urls {
		url := fmt.Sprintf(tmpl, tableName)
		resp, err := s.client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
			return "", err
		}
		_ = os.WriteFile(cachePath, body, 0644)
		return string(body), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DBD URLs configured")
	}
	return "", lastErr
}
