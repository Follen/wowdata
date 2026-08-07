package casc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"wowdata/internal/blte"
	"wowdata/internal/resource"
	"wowdata/internal/storage"
)

const (
	cdnHostTemplate       = "http://%s.patch.battle.net:1119/"
	cdnHostTemplateChina  = "https://cn.version.battlenet.com.cn/"
	rangeChunkSize        = 1 << 20
	defaultRangeChunkSize = 4 << 20
	rangeWorkerCount      = 4
	httpDownloadAttempts  = 3
)

var downloadProgressWriter io.Writer = os.Stderr

func SetDownloadProgressWriter(w io.Writer) {
	downloadProgressWriter = w
}

func SetDownloadProgressWriterForTest(w io.Writer) {
	SetDownloadProgressWriter(w)
}

func DownloadProgressWriterForTest() io.Writer {
	return downloadProgressWriter
}

func ReportDownloadProgress(url, method string, bytes, workers int, duration time.Duration) {
	if downloadProgressWriter == nil {
		return
	}
	if workers > 0 {
		fmt.Fprintf(downloadProgressWriter, "download method=%s url=%s bytes=%d workers=%d duration=%s\n", method, url, bytes, workers, duration.Round(time.Millisecond))
		return
	}
	fmt.Fprintf(downloadProgressWriter, "download method=%s url=%s bytes=%d duration=%s\n", method, url, bytes, duration.Round(time.Millisecond))
}

type CASCRemote struct {
	*CASCSource
	Region            string
	Host              string
	PatchHost         string
	Builds            []VersionEntry
	Build             *VersionEntry
	Server            *VersionEntry
	Cache             *DataCache
	CacheRoot         string
	CacheMaxBytes     int64
	Workers           int
	MetadataWorkers   int
	LargeRangeWorkers int
	RangeChunkSize    int64
	ArchiveTailProbe  int
	ResourceClass     resource.Class
	ManifestTTL       time.Duration
	cacheBudget       *CacheBudget
	fetchFull         func(cdnFile string) ([]byte, error)
	prefetchMu        sync.Mutex
	prefetched        map[string][]byte
	schedulerMu       sync.Mutex
	scheduler         *resource.Scheduler
	schedulerPlan     resource.Plan

	fetchPartial func(cdnFile string, offset, length int) ([]byte, error)
}

func NewCASCRemote(region string) *CASCRemote {
	r := &CASCRemote{
		CASCSource:       NewCASCSource(),
		Region:           region,
		ArchiveTailProbe: 64 << 10,
		RangeChunkSize:   defaultRangeChunkSize,
		ResourceClass:    resource.PointQuery,
	}
	if region == "cn" {
		r.PatchHost = cdnHostTemplateChina
	} else {
		r.PatchHost = fmt.Sprintf(cdnHostTemplate, region)
	}
	r.Host = r.PatchHost
	return r
}

func (r *CASCRemote) ResourcePlan() resource.Plan {
	return resource.NewPlanWithPoolOverrides(r.ResourceClass, r.Workers, r.MetadataWorkers, r.LargeRangeWorkers)
}

func (r *CASCRemote) ResourceScheduler() *resource.Scheduler {
	plan := r.ResourcePlan()
	r.schedulerMu.Lock()
	defer r.schedulerMu.Unlock()
	if r.scheduler == nil || !sameSchedulerPlan(r.schedulerPlan, plan) {
		r.scheduler = resource.NewScheduler(plan)
		r.schedulerPlan = plan
	}
	return r.scheduler
}

func sameSchedulerPlan(left, right resource.Plan) bool {
	// Available memory can fluctuate while a command runs. Freeze the first
	// scheduler's memory budget so every pool continues to share one limiter.
	return left.Class == right.Class &&
		left.GOMAXPROCS == right.GOMAXPROCS &&
		left.MetadataWorkers == right.MetadataWorkers &&
		left.LargeRangeWorkers == right.LargeRangeWorkers &&
		left.DB2Workers == right.DB2Workers &&
		left.BLTEWorkers == right.BLTEWorkers &&
		left.ImageWorkers == right.ImageWorkers &&
		left.ConnectionBudget == right.ConnectionBudget &&
		left.FileHandleBudget == right.FileHandleBudget &&
		left.ExplicitWorkerLimit == right.ExplicitWorkerLimit &&
		left.MetadataOverride == right.MetadataOverride &&
		left.LargeRangeOverride == right.LargeRangeOverride
}

func (r *CASCRemote) GetBuildName() string {
	if r.Build != nil {
		return r.Build.VersionsName
	}
	return ""
}

func (r *CASCRemote) GetBuildKey() string {
	if r.Build != nil {
		return r.Build.BuildConfig
	}
	return ""
}

func (r *CASCRemote) Init() error {
	return r.initProductsWithStage(KnownProducts, 0)
}

func (r *CASCRemote) InitProduct(product string) error {
	return r.InitProductWithStage(product, 0)
}

func (r *CASCRemote) InitProductWithStage(product string, stage resource.StageID) error {
	if strings.TrimSpace(product) == "" {
		return fmt.Errorf("product is required")
	}
	return r.initProductsWithStage([]string{product}, stage)
}

func (r *CASCRemote) initProducts(products []string) error {
	return r.initProductsWithStage(products, 0)
}

func (r *CASCRemote) initProductsWithStage(products []string, stage resource.StageID) error {
	builds := make([]VersionEntry, len(products))
	type versionResult struct {
		index int
		entry VersionEntry
		err   error
	}
	workers := r.ResourcePlan().MetadataWorkers
	if workers > len(products) {
		workers = len(products)
	}
	jobs := make(chan int)
	results := make(chan versionResult, len(products))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for productIndex := range jobs {
				product := products[productIndex]
				config, err := r.getVersionConfigWithStage(product, stage)
				if err != nil {
					results <- versionResult{index: productIndex, err: fmt.Errorf("%s: %w", product, err)}
					continue
				}
				var entry VersionEntry
				for i := range config {
					if config[i].Region == r.Region {
						config[i].Product = product
						entry = config[i]
						break
					}
				}
				results <- versionResult{index: productIndex, entry: entry}
			}
		}()
	}
	go func() {
		for i := range products {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var failures []string
	for result := range results {
		if result.err != nil {
			failures = append(failures, result.err.Error())
			continue
		}
		builds[result.index] = result.entry
	}
	r.Builds = builds
	loaded := 0
	for _, build := range builds {
		if build.Product != "" {
			loaded++
		}
	}
	if loaded == 0 {
		if len(failures) > 0 {
			return fmt.Errorf("no remote builds loaded for region %s: %s", r.Region, strings.Join(failures, "; "))
		}
		return fmt.Errorf("no remote builds loaded for region %s", r.Region)
	}
	return nil
}

var KnownProducts = []string{
	"wow", "wowt", "wowxptr", "wow_beta",
	"wow_classic", "wow_classic_ptr", "wow_classic_beta",
	"wow_classic_era", "wow_classic_era_ptr", "wow_anniversary", "wow_classic_titan",
}

func (r *CASCRemote) getVersionConfig(product string) ([]VersionEntry, error) {
	return r.getVersionConfigWithStage(product, 0)
}

func (r *CASCRemote) getVersionConfigWithStage(product string, stage resource.StageID) ([]VersionEntry, error) {
	url := r.PatchHost + product + "/versions"
	cachePath := r.manifestPath(product + "-versions")
	if body, ok := readFreshManifest(cachePath, r.ManifestTTL); ok {
		return ParseVersionConfig(string(body)), nil
	}
	resp, err := httpGetWithStage(url, stage)
	if err == nil {
		defer resp.Body.Close()
		body, readErr := io.ReadAll(resp.Body)
		if readErr == nil {
			_ = storage.AtomicWriteFile(cachePath, body, 0o644)
			return ParseVersionConfig(string(body)), nil
		}
		err = readErr
	}
	if body, cacheErr := os.ReadFile(cachePath); cacheErr == nil && len(body) > 0 {
		fmt.Fprintf(downloadProgressWriter, "cache source=offline-manifest path=%s\n", filepath.ToSlash(cachePath))
		return ParseVersionConfig(string(body)), nil
	}
	return nil, err
}

func (r *CASCRemote) manifestPath(name string) string {
	root := r.CacheRoot
	if root == "" {
		root = "cache"
	}
	return filepath.Join(root, "manifests", r.Region, name)
}

func (r *CASCRemote) GetConfig(url string) (map[string]string, error) {
	resp, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParseCDNConfig(string(body))
}

func (r *CASCRemote) GetDataFile(cdnFile string) ([]byte, error) {
	url := r.Host + "data/" + cdnFile
	if r.Cache != nil {
		partPath, statePath := ResumeObjectPaths(r.Cache.ResumeRoot(), url)
		if data, err := DownloadHTTPConcurrentResumableWithOptions(url, partPath, statePath, ResumeOptions{
			Workers: r.ResourcePlan().LargeRangeWorkers, CacheRoot: r.CacheRoot, MaxBytes: r.CacheMaxBytes, TTL: DefaultResumeTTL, Budget: r.cacheBudget,
			Scheduler: r.ResourceScheduler(),
			ChunkSize: r.RangeChunkSize,
		}); err == nil {
			return data, nil
		} else if IsCacheQuotaError(err) {
			return nil, err
		}
	}
	if data, err := downloadHTTPConcurrentWithScheduler(url, r.ResourcePlan().LargeRangeWorkers, r.ResourceScheduler()); err == nil {
		return data, nil
	}
	resp, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (r *CASCRemote) GetDataFilePartial(cdnFile string, offset, length int) ([]byte, error) {
	url := r.Host + "data/" + cdnFile
	data, err := httpRangeWithScheduler(url, offset, offset+length-1, r.ResourcePlan().LargeRangeWorkers, r.ResourceScheduler())
	if err != nil {
		return nil, err
	}
	return data, nil
}

func DecodeCASCData(data []byte) ([]byte, error) {
	reader, err := blte.NewReader(data)
	if err != nil {
		return nil, err
	}
	return reader.ReadAll()
}

func DecodeCASCDataPartial(data []byte) ([]byte, error) {
	reader, err := blte.NewPartialReader(data)
	if err != nil {
		return nil, err
	}
	return reader.ReadAll()
}

func (r *CASCRemote) ReadFileData(fileDataID uint32) ([]byte, error) {
	_, encodingKey, err := r.ResolveFileKeys(fileDataID)
	if err != nil {
		return nil, err
	}
	return r.ReadEncodingData(encodingKey)
}

func (r *CASCRemote) ReadFileDataPartial(fileDataID uint32) ([]byte, error) {
	return r.ReadFileDataPartialWithStage(fileDataID, 0)
}

func (r *CASCRemote) ReadFileDataPartialWithStage(fileDataID uint32, stage resource.StageID) ([]byte, error) {
	_, encodingKey, err := r.ResolveFileKeys(fileDataID)
	if err != nil {
		return nil, err
	}
	return r.readEncodingDataWithStage(encodingKey, true, stage)
}

func (r *CASCRemote) ReadEncodingData(encodingKey string) ([]byte, error) {
	return r.readEncodingData(encodingKey, false)
}

func (r *CASCRemote) ReadEncodingDataPartial(encodingKey string) ([]byte, error) {
	return r.readEncodingData(encodingKey, true)
}

func (r *CASCRemote) readEncodingData(encodingKey string, partial bool) ([]byte, error) {
	return r.readEncodingDataWithStage(encodingKey, partial, 0)
}

func (r *CASCRemote) readEncodingDataWithStage(encodingKey string, partial bool, stage resource.StageID) ([]byte, error) {
	decode := func(data []byte) ([]byte, error) {
		if partial {
			return DecodeCASCDataPartial(data)
		}
		return DecodeCASCData(data)
	}
	r.prefetchMu.Lock()
	prefetched := r.prefetched[encodingKey]
	r.prefetchMu.Unlock()
	if len(prefetched) > 0 {
		return decode(prefetched)
	}
	if r.Cache != nil {
		if data, err := r.Cache.GetBLTEObject(encodingKey); err == nil && len(data) > 0 {
			return decode(data)
		}
		lock, err := r.Cache.AcquireObjectLock(encodingKey, 5*time.Minute)
		if err != nil {
			return nil, err
		}
		defer lock.Release()
		if data, err := r.Cache.GetBLTEObject(encodingKey); err == nil && len(data) > 0 {
			return decode(data)
		}
	}

	var raw []byte
	var err error
	if archive, ok := r.Archives[encodingKey]; ok {
		raw, err = r.getDataFilePartialWithStage(FormatCDNKey(archive.Key), int(archive.Offset), int(archive.Size), stage)
	} else {
		raw, err = r.getDataFile(FormatCDNKey(encodingKey))
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty CASC data for encoding key: %s", encodingKey)
	}
	decoded, err := decode(raw)
	if err != nil {
		return nil, err
	}
	if r.Cache != nil {
		if err := r.Cache.StoreObject(encodingKey, raw); err != nil {
			return nil, err
		}
	}
	return decoded, nil
}

func (r *CASCRemote) getDataFile(cdnFile string) ([]byte, error) {
	if r.fetchFull != nil {
		return r.fetchFull(cdnFile)
	}
	return r.GetDataFile(cdnFile)
}

func (r *CASCRemote) getDataFilePartial(cdnFile string, offset, length int) ([]byte, error) {
	return r.getDataFilePartialWithStage(cdnFile, offset, length, 0)
}

func (r *CASCRemote) getDataFilePartialWithStage(cdnFile string, offset, length int, stage resource.StageID) ([]byte, error) {
	if r.fetchPartial != nil {
		return r.fetchPartial(cdnFile, offset, length)
	}
	url := r.Host + "data/" + cdnFile
	return httpRangeWithSchedulerStage(url, offset, offset+length-1, r.ResourcePlan().LargeRangeWorkers, r.ResourceScheduler(), stage)
}

func (r *CASCRemote) Preload(buildIndex int) error {
	return r.preload(buildIndex, nil, false, 0, resource.StageWaveInitial)
}

func (r *CASCRemote) PreloadWithStage(buildIndex int, parent resource.StageID) error {
	return r.preload(buildIndex, nil, false, parent, resource.StageWaveInitial)
}

// PreloadMetadataWithStage prepares the selected Build and its server, CDN,
// and Build configs without loading archive indexes, encoding, or root data.
func (r *CASCRemote) PreloadMetadataWithStage(buildIndex int, parent resource.StageID) error {
	return r.preload(buildIndex, nil, true, parent, resource.StageWaveInitial)
}

// PreloadFiles prepares only the supplied FileDataIDs. Network payloads remain
// fully verified, but root, encoding, and archive catalogs are projected in
// memory to the query's dependency set.
func (r *CASCRemote) PreloadFiles(buildIndex int, fileDataIDs []uint32) error {
	return r.PreloadFilesWithStage(buildIndex, fileDataIDs, 0)
}

func (r *CASCRemote) PreloadFilesWithStage(buildIndex int, fileDataIDs []uint32, parent resource.StageID) error {
	if len(fileDataIDs) == 0 {
		return r.preload(buildIndex, nil, false, parent, resource.StageWaveInitial)
	}
	return r.preload(buildIndex, fileDataIDs, false, parent, resource.StageWaveInitial)
}

func (r *CASCRemote) EnsureFiles(fileDataIDs []uint32) error {
	return r.EnsureFilesWithStage(fileDataIDs, 0)
}

func (r *CASCRemote) EnsureFilesWithStage(fileDataIDs []uint32, parent resource.StageID) error {
	batch := resource.StartStage("ensure-files-batch", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveResourceSecondWave, DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
	var resultErr error
	defer func() { resource.FinishStage(batch, resultErr) }()
	if r.Build == nil || r.Cache == nil || len(fileDataIDs) == 0 {
		return nil
	}
	check := resource.StartStage("ensure-missing-check", resource.StageOptions{ParentID: batch, Wave: resource.StageWaveResourceSecondWave, DependsOn: []resource.StageDependency{resource.Dependency(batch, resource.StageRelationHard)}})
	missing := make([]uint32, 0, len(fileDataIDs))
	for _, id := range fileDataIDs {
		if !r.FileExists(id) {
			missing = append(missing, id)
		}
	}
	resource.FinishStage(check, nil)
	if len(missing) == 0 {
		return nil
	}
	lastHTTP := SnapshotHTTPMetrics()
	report := func(_ string, stageID resource.StageID) {
		current := SnapshotHTTPMetrics()
		uniqueBytes := current.UniquePayloadBytes - lastHTTP.UniquePayloadBytes
		transferredBytes := current.ResponseBytes - lastHTTP.ResponseBytes
		uniqueBytes -= current.StageUniqueBytes - lastHTTP.StageUniqueBytes
		transferredBytes -= current.StageTransferBytes - lastHTTP.StageTransferBytes
		if uniqueBytes > 0 || transferredBytes > 0 {
			resource.RecordStageNetwork(stageID, uint64(max(uniqueBytes, 0)), uint64(max(transferredBytes, 0)))
		}
		lastHTTP = current
	}
	resultErr = r.loadSelectedFiles(missing, report, batch, resource.StageWaveResourceSecondWave)
	return resultErr
}

func (r *CASCRemote) preload(buildIndex int, fileDataIDs []uint32, metadataOnly bool, parent resource.StageID, wave string) error {
	preloadStarted := time.Now()
	preloadLast := preloadStarted
	preloadHTTP := SnapshotHTTPMetrics()
	reportPreload := func(stage string, stageID resource.StageID) {
		now := time.Now()
		currentHTTP := SnapshotHTTPMetrics()
		uniqueBytes := currentHTTP.UniquePayloadBytes - preloadHTTP.UniquePayloadBytes
		transferredBytes := currentHTTP.ResponseBytes - preloadHTTP.ResponseBytes
		uniqueBytes -= currentHTTP.StageUniqueBytes - preloadHTTP.StageUniqueBytes
		transferredBytes -= currentHTTP.StageTransferBytes - preloadHTTP.StageTransferBytes
		if uniqueBytes > 0 || transferredBytes > 0 {
			resource.RecordStageNetwork(stageID, uint64(max(uniqueBytes, 0)), uint64(max(transferredBytes, 0)))
		}
		if os.Getenv("WOWDATA_TIMING") == "1" {
			fmt.Fprintf(downloadProgressWriter, "timing stage=casc-%s duration=%s total=%s requests=%d uniqueBytes=%d duplicateBytes=%d canceled=%d\n",
				stage, now.Sub(preloadLast).Round(time.Millisecond), now.Sub(preloadStarted).Round(time.Millisecond),
				currentHTTP.Requests-preloadHTTP.Requests,
				uniqueBytes,
				currentHTTP.DuplicateBytes-preloadHTTP.DuplicateBytes,
				currentHTTP.CanceledRequests-preloadHTTP.CanceledRequests,
			)
		}
		preloadLast = now
		preloadHTTP = currentHTTP
	}
	if buildIndex < 0 || buildIndex >= len(r.Builds) {
		return fmt.Errorf("build index %d out of range", buildIndex)
	}
	r.Build = &r.Builds[buildIndex]

	// Initialize cache
	cacheRoot := r.CacheRoot
	if cacheRoot == "" {
		cacheRoot = "cache"
	}
	r.Cache = NewDataCache(cacheRoot, r.Build.BuildConfig)
	r.cacheBudget = NewCacheBudget(cacheRoot, r.CacheMaxBytes)

	serverStage := resource.StartStage("casc-server-config", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
	if err := r.loadServerConfig(); err != nil {
		resource.FinishStage(serverStage, err)
		return fmt.Errorf("server config: %w", err)
	}
	if r.Server != nil {
		r.Host = r.bestCDNHost(r.Server)
	}
	resource.FinishStage(serverStage, nil)
	reportPreload("server-config", serverStage)

	// Load configs
	cdnStage := resource.StartStage("casc-cdn-config", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(serverStage, resource.StageRelationHard)}})
	cdnCfg, err := r.getConfigWithCache("cdn_config", r.Host+"config/"+FormatCDNKey(r.Build.CDNConfig))
	if err != nil {
		resource.FinishStage(cdnStage, err)
		return fmt.Errorf("cdn config: %w", err)
	}
	r.SetCDNConfig(cdnCfg)
	resource.FinishStage(cdnStage, nil)
	reportPreload("cdn-config", cdnStage)

	buildStage := resource.StartStage("casc-build-config", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(serverStage, resource.StageRelationHard)}})
	buildCfg, err := r.getConfigWithCache("build_config", r.Host+"config/"+FormatCDNKey(r.Build.BuildConfig))
	if err != nil {
		resource.FinishStage(buildStage, err)
		return fmt.Errorf("build config: %w", err)
	}
	r.SetBuildConfig(buildCfg)
	resource.FinishStage(buildStage, nil)
	reportPreload("build-config", buildStage)
	if metadataOnly {
		return nil
	}

	if len(fileDataIDs) == 0 {
		archivesStage := resource.StartStage("casc-archives-full", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(cdnStage, resource.StageRelationHard)}})
		if err := r.loadArchives(); err != nil {
			resource.FinishStage(archivesStage, err)
			return fmt.Errorf("archives: %w", err)
		}
		resource.FinishStage(archivesStage, nil)
		reportPreload("archives", archivesStage)
		encodingStage := resource.StartStage("casc-encoding-full", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(buildStage, resource.StageRelationHard)}})
		if err := r.loadEncoding(); err != nil {
			resource.FinishStage(encodingStage, err)
			return fmt.Errorf("encoding: %w", err)
		}
		resource.FinishStage(encodingStage, nil)
		reportPreload("encoding", encodingStage)
		rootStage := resource.StartStage("casc-root-full", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(encodingStage, resource.StageRelationHard)}})
		if err := r.loadRoot(); err != nil {
			resource.FinishStage(rootStage, err)
			return fmt.Errorf("root: %w", err)
		}
		resource.FinishStage(rootStage, nil)
		reportPreload("root", rootStage)
		return nil
	}

	if err := r.loadSelectedFiles(fileDataIDs, reportPreload, parent, wave); err != nil {
		return err
	}

	return nil
}

func (r *CASCRemote) loadSelectedFiles(fileDataIDs []uint32, report func(string, resource.StageID), parent resource.StageID, wave string) error {
	encKeys := strings.Fields(r.BuildConfig["encoding"])
	if len(encKeys) < 2 {
		return fmt.Errorf("encoding key not found in build config")
	}
	type cachedRootResult struct {
		data []byte
		err  error
	}
	rootCacheCh := make(chan cachedRootResult, 1)
	rootCacheStage := resource.StartStage("casc-root-cache-read", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
	go func() {
		data, err := r.Cache.GetBLTEFile("build_root", "")
		resource.FinishStage(rootCacheStage, err)
		rootCacheCh <- cachedRootResult{data: data, err: err}
	}()
	encodingReaderStage := resource.StartStage("casc-encoding-reader", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
	encodingReader, err := r.getConfigRangeReader("build_encoding", encKeys[1])
	if err != nil {
		resource.FinishStage(encodingReaderStage, err)
		return fmt.Errorf("encoding: %w", err)
	}
	resource.FinishStage(encodingReaderStage, nil)
	cachedRoot := <-rootCacheCh
	workers := r.ResourcePlan().BLTEWorkers
	encodingWorkers := workers
	rootWorkers := workers
	if cachedRoot.err == nil && len(cachedRoot.data) > 0 && workers > 1 {
		rootWorkers = selectedRootWorkerBudget(cachedRoot.data, fileDataIDs, workers)
		encodingWorkers = workers - rootWorkers
	}
	rootParseCh := make(chan error, 1)
	var rootParseStage resource.StageID
	if cachedRoot.err == nil && len(cachedRoot.data) > 0 {
		rootParseStage = resource.StartStage("casc-root-cache-parse", resource.StageOptions{ParentID: parent, Wave: wave, Condition: "cache-hit", DependsOn: []resource.StageDependency{resource.Dependency(rootCacheStage, resource.StageRelationHard)}})
		go func() {
			release, acquireErr := r.ResourceScheduler().Acquire(context.Background(), resource.BLTEDecodePool)
			if acquireErr != nil {
				resource.FinishStage(rootParseStage, acquireErr)
				rootParseCh <- acquireErr
				return
			}
			defer release()
			_, parseErr := r.parseRootFileSelectedAdaptiveForLocale(cachedRoot.data, fileDataIDs, rootWorkers)
			resource.FinishStage(rootParseStage, parseErr)
			rootParseCh <- parseErr
		}()
	}
	rootLookupStage := resource.StartStage("casc-root-key-lookup", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(encodingReaderStage, resource.StageRelationHard)}})
	rootKey, decodeErr := hex.DecodeString(r.BuildConfig["root"])
	if decodeErr != nil {
		resource.FinishStage(rootLookupStage, decodeErr)
		return fmt.Errorf("invalid root content key: %w", decodeErr)
	}
	if err := r.withResource(resource.BLTEDecodePool, func() error {
		return r.parseEncodingReaderSelected(encodingReader, map[string]struct{}{string(rootKey): {}}, encodingWorkers)
	}); err != nil {
		resource.FinishStage(rootLookupStage, err)
		return fmt.Errorf("encoding root lookup: %w", err)
	}
	resource.FinishStage(rootLookupStage, nil)
	rootSelectedDeps := []resource.StageDependency{resource.Dependency(rootLookupStage, resource.StageRelationJoin)}
	if cachedRoot.err == nil && len(cachedRoot.data) > 0 {
		rootSelectedDeps = append(rootSelectedDeps, resource.Dependency(rootParseStage, resource.StageRelationJoin))
		if err := <-rootParseCh; err != nil {
			return fmt.Errorf("root: %w", err)
		}
	} else {
		rootFetchStage := resource.StartStage("casc-root-fetch-parse", resource.StageOptions{ParentID: parent, Wave: wave, Condition: "cache-miss", DependsOn: []resource.StageDependency{resource.Dependency(rootLookupStage, resource.StageRelationHard)}})
		rootEntry, ok := r.EncodingEntries[r.BuildConfig["root"]]
		if !ok {
			resource.FinishStage(rootFetchStage, fmt.Errorf("missing root encoding entry"))
			return fmt.Errorf("no encoding entry found for root key")
		}
		rootReader, loadErr := r.getConfigRangeReader("build_root", rootEntry.Key)
		if loadErr != nil {
			resource.FinishStage(rootFetchStage, loadErr)
			return fmt.Errorf("root: %w", loadErr)
		}
		if err := r.withResource(resource.BLTEDecodePool, func() error {
			_, parseErr := r.parseRootReaderSelectedForLocale(rootReader, fileDataIDs, workers)
			return parseErr
		}); err != nil {
			resource.FinishStage(rootFetchStage, err)
			return fmt.Errorf("root: %w", err)
		}
		resource.FinishStage(rootFetchStage, nil)
		rootSelectedDeps = []resource.StageDependency{resource.Dependency(rootFetchStage, resource.StageRelationHard)}
	}
	rootSelectedStage := resource.StartStage("casc-root-selected", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: rootSelectedDeps})
	resource.FinishStage(rootSelectedStage, nil)
	report("root-selected", rootSelectedStage)

	contentKeys := make([]string, 0, len(fileDataIDs))
	for _, id := range fileDataIDs {
		for _, entry := range r.RootEntries[id] {
			contentKeys = append(contentKeys, entry.ContentKey)
		}
	}
	wantedContentKeys := make(map[string]struct{}, len(contentKeys))
	for _, key := range contentKeys {
		rawKey, decodeErr := hex.DecodeString(key)
		if decodeErr != nil {
			return fmt.Errorf("invalid content key %q: %w", key, decodeErr)
		}
		wantedContentKeys[string(rawKey)] = struct{}{}
	}
	encodingSelectedStage := resource.StartStage("casc-encoding-selected", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(rootSelectedStage, resource.StageRelationHard)}})
	if err := r.withResource(resource.BLTEDecodePool, func() error {
		return r.parseEncodingReaderSelected(encodingReader, wantedContentKeys, workers)
	}); err != nil {
		resource.FinishStage(encodingSelectedStage, err)
		return fmt.Errorf("encoding: %w", err)
	}
	resource.FinishStage(encodingSelectedStage, nil)
	report("encoding-selected", encodingSelectedStage)

	missing := make(map[string]struct{}, len(contentKeys))
	cacheCheckStage := resource.StartStage("casc-selected-cache-check", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(encodingSelectedStage, resource.StageRelationHard)}})
	r.prefetchMu.Lock()
	if r.prefetched == nil {
		r.prefetched = make(map[string][]byte)
	}
	for _, contentKey := range contentKeys {
		entry, exists := r.EncodingEntries[contentKey]
		if !exists {
			continue
		}
		if data, cacheErr := r.Cache.GetBLTEFile(entry.Key, filepath.Join(r.Cache.Root(), "data")); cacheErr == nil && len(data) > 0 {
			r.prefetched[entry.Key] = data
		} else {
			missing[entry.Key] = struct{}{}
		}
	}
	r.prefetchMu.Unlock()
	resource.FinishStage(cacheCheckStage, nil)
	var archiveDependency resource.StageID = cacheCheckStage
	if len(missing) > 0 {
		var selectedErr error
		archiveDependency, selectedErr = r.loadArchivesSelectedTraced(missing, cacheCheckStage, wave)
		if selectedErr != nil {
			return fmt.Errorf("archives: %w", selectedErr)
		}
	}
	archivesSelectedStage := resource.StartStage("casc-archives-selected", resource.StageOptions{ParentID: parent, Wave: wave, DependsOn: []resource.StageDependency{resource.Dependency(archiveDependency, resource.StageRelationJoin)}})
	resource.FinishStage(archivesSelectedStage, nil)
	report("archives-selected", archivesSelectedStage)
	return nil
}

func (r *CASCRemote) loadArchivesSelected(encodingKeys map[string]struct{}) error {
	_, err := r.loadArchivesSelectedTraced(encodingKeys, 0, resource.StageWaveInitial)
	return err
}

func (r *CASCRemote) loadArchivesSelectedTraced(encodingKeys map[string]struct{}, parent resource.StageID, wave string) (resource.StageID, error) {
	archiveKeys := strings.Fields(r.CDNConfig["archives"])
	if len(archiveKeys) == 0 || len(encodingKeys) == 0 {
		return parent, nil
	}
	remaining := make(map[string]struct{}, len(encodingKeys))
	for key := range encodingKeys {
		remaining[key] = struct{}{}
	}
	dependency := parent
	fileIndexes := []struct{ key, sizeKey string }{{"fileIndex", "fileIndexSize"}}
	for _, index := range fileIndexes {
		fileIndex := strings.TrimSpace(r.CDNConfig[index.key])
		if fileIndex == "" || len(remaining) == 0 {
			continue
		}
		fileStage := resource.StartStage("casc-file-index", resource.StageOptions{ParentID: parent, Wave: wave, Condition: "file-index-present", DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
		fileIndexSize, _ := strconv.Atoi(strings.TrimSpace(r.CDNConfig[index.sizeKey]))
		loose, err := (&cdnFileLocator{remote: r, key: fileIndex, size: fileIndexSize, stage: fileStage, wave: wave}).LocateFiles(remaining)
		resource.FinishStage(fileStage, err)
		dependency = fileStage
		if err == nil {
			for key := range loose {
				delete(remaining, key)
			}
		}
	}
	if len(remaining) == 0 {
		return dependency, nil
	}
	groups := []struct {
		key      string
		archives []string
	}{
		{strings.TrimSpace(r.CDNConfig["archiveGroup"]), archiveKeys},
	}
	for _, group := range groups {
		if group.key == "" || len(group.archives) == 0 || len(remaining) == 0 {
			continue
		}
		groupStage := resource.StartStage("casc-archive-group", resource.StageOptions{ParentID: dependency, Wave: wave, Condition: "group-index-present", DependsOn: []resource.StageDependency{resource.Dependency(dependency, resource.StageRelationJoin)}})
		groupLocator := &cdnGroupLocator{remote: r, key: group.key, archiveKeys: group.archives, stage: groupStage, wave: wave}
		entries, groupErr := groupLocator.LocateArchives(remaining)
		resource.FinishStage(groupStage, groupErr)
		dependency = groupStage
		if groupErr != nil {
			return dependency, groupErr
		}
		for key, entry := range entries {
			r.Archives[key] = entry
			delete(remaining, key)
		}
	}
	if len(remaining) == 0 {
		return dependency, nil
	}
	// These keys were resolved through the normal encoding file, so only the
	// normal archives can contain their payloads. Patch locators belong to the
	// separate patch-encoding graph and probing them here is pure request cost.
	allArchiveKeys := append([]string(nil), archiveKeys...)
	tailProbe := r.ArchiveTailProbe
	if tailProbe <= 0 {
		tailProbe = archiveIndexTailProbe(r.CDNConfig)
	}
	locatorStage := resource.StartStage("casc-archive-locator", resource.StageOptions{ParentID: parent, Wave: wave, Condition: "remaining-keys", DependsOn: []resource.StageDependency{resource.Dependency(dependency, resource.StageRelationFallback)}})
	probeLimit := r.ResourcePlan().MetadataWorkers * 8
	if probeLimit < 64 {
		probeLimit = 64
	}
	if probeLimit > 256 {
		probeLimit = 256
	}
	locator := &cdnArchiveLocator{remote: r, archiveKeys: allArchiveKeys, tailProbe: tailProbe, probeLimit: probeLimit, adaptive: true, stage: locatorStage, wave: wave}
	entries, err := locator.LocateArchives(remaining)
	for hash, entry := range entries {
		r.Archives[hash] = entry
	}
	if err != nil {
		resource.FinishStage(locatorStage, err)
		return locatorStage, err
	}
	resource.FinishStage(locatorStage, nil)
	return locatorStage, nil
}

func (r *CASCRemote) loadArchives() error {
	archiveKeys := strings.Fields(r.CDNConfig["archives"])
	if len(archiveKeys) == 0 {
		return nil
	}
	jobs := make(chan string)
	var mu sync.Mutex
	var failures []string
	var wg sync.WaitGroup

	workerCount := r.ResourcePlan().MetadataWorkers
	if len(archiveKeys) < workerCount {
		workerCount = len(archiveKeys)
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range jobs {
				data, err := r.downloadArchiveIndex(key)
				var entries map[string]ArchiveEntry
				if err == nil {
					entries = ParseArchiveIndexEntries(data, key)
				}
				mu.Lock()
				if err != nil {
					failures = append(failures, key+": "+err.Error())
				} else {
					for hash, entry := range entries {
						r.Archives[hash] = entry
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, key := range archiveKeys {
		jobs <- key
	}
	close(jobs)
	wg.Wait()
	if len(failures) == len(archiveKeys) {
		return fmt.Errorf("all archive indexes failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (r *CASCRemote) loadServerConfig() error {
	if r.Build == nil {
		return fmt.Errorf("build is not selected")
	}
	url := r.PatchHost + r.Build.Product + "/cdns"
	cachePath := r.manifestPath(r.Build.Product + "-cdns")
	if body, ok := readFreshManifest(cachePath, r.ManifestTTL); ok {
		servers := ParseVersionConfig(string(body))
		for i := range servers {
			if servers[i].Name == r.Region || servers[i].Product == r.Region {
				r.Server = &servers[i]
				return nil
			}
		}
	}
	resp, err := httpGet(url)
	var body []byte
	if err == nil {
		defer resp.Body.Close()
		body, err = io.ReadAll(resp.Body)
		if err == nil {
			_ = storage.AtomicWriteFile(cachePath, body, 0o644)
		}
	}
	if err != nil {
		body, err = os.ReadFile(cachePath)
		if err != nil {
			return err
		}
		fmt.Fprintf(downloadProgressWriter, "cache source=offline-manifest path=%s\n", filepath.ToSlash(cachePath))
	}
	servers := ParseVersionConfig(string(body))
	for i := range servers {
		if servers[i].Name == r.Region || servers[i].Product == r.Region {
			r.Server = &servers[i]
			return nil
		}
	}
	return fmt.Errorf("CDN config does not contain entry for region %s", r.Region)
}

func readFreshManifest(path string, ttl time.Duration) ([]byte, bool) {
	if ttl <= 0 {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) >= ttl {
		return nil, false
	}
	body, err := os.ReadFile(path)
	return body, err == nil && len(body) > 0
}

func (r *CASCRemote) getConfigWithCache(name, url string) (map[string]string, error) {
	if r.Cache != nil {
		if body, err := r.Cache.GetFile(name, ""); err == nil && len(body) > 0 {
			return ParseCDNConfig(string(body))
		}
	}
	resp, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	config, err := ParseCDNConfig(string(body))
	if err != nil {
		return nil, err
	}
	if r.Cache != nil {
		_ = r.Cache.StoreFile(name, body, "")
	}
	return config, nil
}

func (r *CASCRemote) bestCDNHost(server *VersionEntry) string {
	if server == nil {
		return r.Host
	}
	host := ""
	for _, h := range strings.Fields(server.Hosts) {
		host = h
		break
	}
	if host == "" {
		return r.Host
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}
	host = strings.TrimRight(host, "/") + "/"
	path := strings.Trim(server.Path, "/")
	if path != "" {
		host += path + "/"
	}
	return host
}

func (r *CASCRemote) downloadArchiveIndex(key string) ([]byte, error) {
	fileName := key + ".index"
	data, err := r.Cache.GetObject(fileName)
	if err == nil && len(data) > 0 {
		return data, nil
	}
	lock, err := r.Cache.AcquireObjectLock(fileName, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	data, err = r.Cache.GetObject(fileName)
	if err == nil && len(data) > 0 {
		return data, nil
	}
	cdnKey := FormatCDNKey(key) + ".index"
	data, err = r.GetDataFile(cdnKey)
	if err != nil {
		return nil, err
	}
	if err := r.storeCompleteArchiveIndex(fileName, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (r *CASCRemote) storeCompleteArchiveIndex(fileName string, data []byte) error {
	if err := r.Cache.StoreObject(fileName, data); err != nil {
		return err
	}
	if err := r.Cache.RemoveObjectsPrefix(fileName + ".tail"); err != nil {
		return err
	}
	return r.Cache.RemoveObjectsPrefix(fileName + ".page-")
}

func (r *CASCRemote) loadEncoding() error {
	encKeys := strings.Split(r.BuildConfig["encoding"], " ")
	if len(encKeys) < 2 {
		return fmt.Errorf("encoding key not found in build config")
	}
	encKey := encKeys[1]

	data, err := r.getConfigFileWithCache("build_encoding", encKey)
	if err != nil {
		return err
	}
	return r.withResource(resource.BLTEDecodePool, func() error { return r.ParseEncodingFile(data) })
}

func (r *CASCRemote) loadRoot() error {
	rootEntry, ok := r.EncodingEntries[r.BuildConfig["root"]]
	if !ok {
		return fmt.Errorf("no encoding entry found for root key")
	}

	data, err := r.getConfigFileWithCache("build_root", rootEntry.Key)
	if err != nil {
		return err
	}
	return r.withResource(resource.BLTEDecodePool, func() error {
		_, parseErr := r.ParseRootFile(data)
		return parseErr
	})
}

func (r *CASCRemote) withResource(pool resource.Pool, fn func() error) error {
	release, err := r.ResourceScheduler().Acquire(context.Background(), pool)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

func (r *CASCRemote) getConfigFileWithCache(name, key string) ([]byte, error) {
	data, err := r.Cache.GetBLTEFile(name, "")
	if err == nil && len(data) > 0 {
		return data, nil
	}
	data, err = r.GetDataFile(FormatCDNKey(key))
	if err != nil {
		return nil, err
	}
	if err := r.Cache.StoreFile(name, data, ""); err != nil {
		return nil, err
	}
	if err := r.Cache.RemoveObjectsPrefix(key + ".blte.range-"); err != nil {
		return nil, err
	}
	return data, nil
}

func (r *CASCRemote) getConfigRangeReader(name, key string) (decompressedRangeReader, error) {
	if data, err := r.Cache.GetBLTEFile(name, ""); err == nil && len(data) > 0 {
		return blte.NewReader(data)
	}
	reader, err := blte.NewRangeReader(func(offset, length int) ([]byte, error) {
		objectName := fmt.Sprintf("%s.blte.range-%d-%d", key, offset, length)
		if data, cacheErr := r.Cache.GetObject(objectName); cacheErr == nil && len(data) == length {
			return data, nil
		}
		lock, lockErr := r.Cache.AcquireObjectLock(objectName, 5*time.Minute)
		if lockErr != nil {
			return nil, lockErr
		}
		defer lock.Release()
		if data, cacheErr := r.Cache.GetObject(objectName); cacheErr == nil && len(data) == length {
			return data, nil
		}
		data, fetchErr := r.getDataFilePartial(FormatCDNKey(key), offset, length)
		if fetchErr != nil {
			return nil, fetchErr
		}
		if len(data) != length {
			return nil, fmt.Errorf("BLTE range %s %d+%d returned %d bytes", key, offset, length, len(data))
		}
		if storeErr := r.Cache.StoreObject(objectName, data); storeErr != nil {
			return nil, storeErr
		}
		return data, nil
	})
	if err == nil {
		return reader, nil
	}
	data, fallbackErr := r.getConfigFileWithCache(name, key)
	if fallbackErr != nil {
		return nil, errors.Join(err, fallbackErr)
	}
	return blte.NewReader(data)
}

func (r *CASCRemote) GetProductList() []Product {
	var products []Product
	for i, build := range r.Builds {
		if build.Product == "" {
			continue
		}
		title := knownProductTitle(build.Product)
		if title == "" {
			title = build.Product
		}
		label := fmt.Sprintf("%s %s", title, build.VersionsName)
		products = append(products, Product{
			Label:          strings.TrimSpace(label),
			BuildIndex:     i,
			Product:        build.Product,
			Region:         build.Region,
			Version:        buildVersion(build),
			BuildID:        buildID(build),
			BuildConfigKey: build.BuildConfig,
			CDNConfigKey:   build.CDNConfig,
			Branch:         build.Branch,
			Locales:        LocaleNames(),
		})
	}
	return products
}

func buildVersion(build VersionEntry) string {
	if build.VersionsName != "" {
		return build.VersionsName
	}
	return build.Version
}

func buildID(build VersionEntry) string {
	version := buildVersion(build)
	if index := strings.LastIndex(version, "."); index >= 0 && index+1 < len(version) {
		return version[index+1:]
	}
	return ""
}

var httpClient = &http.Client{Timeout: 120 * time.Second, Transport: instrumentedTransport{base: newHTTPTransport()}}

func newHTTPTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 512
	transport.MaxIdleConnsPerHost = 256
	transport.MaxConnsPerHost = 256
	transport.ForceAttemptHTTP2 = true
	return transport
}

func httpGet(url string) (*http.Response, error) {
	return httpGetWithStage(url, 0)
}

func httpGetWithStage(url string, stage resource.StageID) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if stage != 0 {
		request = request.WithContext(resource.ContextWithStage(request.Context(), stage))
	}
	resp, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return resp, nil
}

// DownloadHTTPConcurrent downloads a URL with one request for small/non-range
// objects and concurrent chunks for large range-capable objects.
func DownloadHTTPConcurrent(url string) ([]byte, error) {
	return DownloadHTTPConcurrentWithWorkers(url, rangeWorkerCount)
}

func DownloadHTTPConcurrentWithWorkers(url string, workers int) ([]byte, error) {
	return downloadHTTPConcurrentWithScheduler(url, workers, nil)
}

func downloadHTTPConcurrentWithScheduler(url string, workers int, scheduler *resource.Scheduler) ([]byte, error) {
	return httpGetConcurrent(url, workers, scheduler)
}

func httpGetConcurrent(url string, workers int, scheduler *resource.Scheduler) ([]byte, error) {
	startedAt := time.Now()
	// Use the first payload range as the capability/size probe. This removes the
	// serialized HEAD round trip and avoids downloading the first chunk twice.
	probeEnd := rangeChunkSize - 1
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", probeEnd))
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		data, readErr := io.ReadAll(resp.Body)
		if readErr == nil {
			ReportDownloadProgress(url, "get", len(data), 0, time.Since(startedAt))
		}
		return data, readErr
	}
	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("HTTP %d range probe from %s", resp.StatusCode, url)
	}
	probeStart, probeLast, total, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil {
		return nil, err
	}
	if probeStart != 0 {
		return nil, fmt.Errorf("range probe started at %d, want 0", probeStart)
	}
	size := int(total)
	out := make([]byte, size)
	probeLength := int(probeLast + 1)
	if _, err := io.ReadFull(resp.Body, out[:probeLength]); err != nil {
		return nil, err
	}
	if size <= rangeChunkSize {
		ReportDownloadProgress(url, "range", size, 1, time.Since(startedAt))
		return out, nil
	}
	if _, err := resp.Body.Read(make([]byte, 1)); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("range probe returned more than %d bytes", probeLength)
		}
		return nil, err
	}
	actualWorkers, err := httpRangeConcurrentInto(url, out, 0, probeLength, size-1, workers, scheduler, 0)
	if err != nil {
		return nil, err
	}
	ReportDownloadProgress(url, "range", size, actualWorkers, time.Since(startedAt))
	return out, nil
}

func parseContentRange(value string) (start, end, total int64, err error) {
	if _, err = fmt.Sscanf(strings.TrimSpace(value), "bytes %d-%d/%d", &start, &end, &total); err != nil || start < 0 || end < start || total <= end {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	return start, end, total, nil
}

func httpRange(url string, start, end int) ([]byte, error) {
	return httpRangeWithWorkers(url, start, end, rangeWorkerCount)
}

func httpRangeWithWorkers(url string, start, end, workers int) ([]byte, error) {
	return httpRangeWithScheduler(url, start, end, workers, nil)
}

func httpRangeWithScheduler(url string, start, end, workers int, scheduler *resource.Scheduler) ([]byte, error) {
	return httpRangeWithSchedulerStage(url, start, end, workers, scheduler, 0)
}

func httpRangeWithSchedulerStage(url string, start, end, workers int, scheduler *resource.Scheduler, stage resource.StageID) ([]byte, error) {
	length := end - start + 1
	if length > rangeChunkSize {
		return httpRangeConcurrentWithSchedulerStage(url, start, end, workers, scheduler, stage)
	}
	return httpRangeSingleScheduledStage(url, start, end, scheduler, stage)
}

func httpRangeSingle(url string, start, end int) ([]byte, error) {
	return httpRangeSingleScheduled(url, start, end, nil)
}

func httpRangeSingleScheduled(url string, start, end int, scheduler *resource.Scheduler) ([]byte, error) {
	return httpRangeSingleScheduledStage(url, start, end, scheduler, 0)
}

func httpRangeSingleScheduledStage(url string, start, end int, scheduler *resource.Scheduler, stage resource.StageID) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < httpDownloadAttempts; attempt++ {
		data, err := httpRangeSingleOnceScheduledStage(url, start, end, scheduler, stage)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if attempt+1 < httpDownloadAttempts {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}
	return nil, lastErr
}

func httpRangeSingleOnce(url string, start, end int) ([]byte, error) {
	return httpRangeSingleOnceScheduled(url, start, end, nil)
}

func httpRangeSingleOnceScheduled(url string, start, end int, scheduler *resource.Scheduler) ([]byte, error) {
	return httpRangeSingleOnceScheduledStage(url, start, end, scheduler, 0)
}

func httpRangeSingleOnceScheduledStage(url string, start, end int, scheduler *resource.Scheduler, stage resource.StageID) ([]byte, error) {
	release, err := scheduler.Acquire(context.Background(), resource.LargeRangePool)
	if err != nil {
		return nil, err
	}
	defer release()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	if stage != 0 {
		req = req.WithContext(resource.ContextWithStage(req.Context(), stage))
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d range request", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func httpRangeConcurrent(url string, start, end int) ([]byte, error) {
	return httpRangeConcurrentWithWorkers(url, start, end, rangeWorkerCount)
}

func httpRangeConcurrentWithWorkers(url string, start, end, workers int) ([]byte, error) {
	return httpRangeConcurrentWithScheduler(url, start, end, workers, nil)
}

func httpRangeConcurrentWithScheduler(url string, start, end, workers int, scheduler *resource.Scheduler) ([]byte, error) {
	return httpRangeConcurrentWithSchedulerStage(url, start, end, workers, scheduler, 0)
}

func httpRangeConcurrentWithSchedulerStage(url string, start, end, workers int, scheduler *resource.Scheduler, stage resource.StageID) ([]byte, error) {
	startedAt := time.Now()
	length := end - start + 1
	if length <= 0 {
		return nil, fmt.Errorf("invalid range %d-%d", start, end)
	}
	out := make([]byte, length)
	actualWorkers, err := httpRangeConcurrentInto(url, out, start, start, end, workers, scheduler, stage)
	if err != nil {
		return nil, err
	}
	ReportDownloadProgress(url, "range", length, actualWorkers, time.Since(startedAt))
	return out, nil
}

func httpRangeConcurrentInto(url string, out []byte, outBase, start, end, workers int, scheduler *resource.Scheduler, stage resource.StageID) (int, error) {
	length := end - start + 1
	if length <= 0 {
		return 0, nil
	}
	type chunk struct {
		start int
		end   int
	}
	jobs := make(chan chunk)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	if workers <= 0 {
		workers = rangeWorkerCount
	}
	chunkCount := (length + rangeChunkSize - 1) / rangeChunkSize
	if chunkCount < workers {
		workers = chunkCount
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				select {
				case <-errCh:
					return
				default:
				}
				data, err := httpRangeSingleScheduledStage(url, job.start, job.end, scheduler, stage)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				offset := job.start - outBase
				copy(out[offset:offset+len(data)], data)
			}
		}()
	}

	for chunkStart := start; chunkStart <= end; chunkStart += rangeChunkSize {
		chunkEnd := chunkStart + rangeChunkSize - 1
		if chunkEnd > end {
			chunkEnd = end
		}
		select {
		case err := <-errCh:
			close(jobs)
			wg.Wait()
			return workers, err
		case jobs <- chunk{start: chunkStart, end: chunkEnd}:
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errCh:
		return workers, err
	default:
		return workers, nil
	}
}
