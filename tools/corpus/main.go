package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"wowdata/internal/casc"
	"wowdata/internal/db2"
	"wowdata/internal/dbd"
	appruntime "wowdata/internal/runtime"
	"wowdata/internal/storage"
)

type targetSpec struct {
	Region  string `json:"region"`
	Product string `json:"product"`
	Locale  string `json:"locale"`
}

type tableResult struct {
	Table                string         `json:"table"`
	FileDataID           uint32         `json:"fileDataID"`
	Status               string         `json:"status"`
	Error                string         `json:"error,omitempty"`
	Bytes                int            `json:"bytes,omitempty"`
	Magic                string         `json:"magic,omitempty"`
	Class                string         `json:"class,omitempty"`
	HeaderOnly           bool           `json:"headerOnly,omitempty"`
	Rows                 int            `json:"rows,omitempty"`
	Sections             int            `json:"sections,omitempty"`
	Compression          map[string]int `json:"compression,omitempty"`
	CopyRows             int            `json:"copyRows,omitempty"`
	RelationshipValues   int            `json:"relationshipValues,omitempty"`
	LookupStrategies     map[string]int `json:"lookupStrategies,omitempty"`
	LookupIndexedRows    int            `json:"lookupIndexedRows,omitempty"`
	LookupDenseSlots     int            `json:"lookupDenseSlots,omitempty"`
	LookupIndexBytes     uint64         `json:"lookupIndexBytes,omitempty"`
	LookupUnclassified   int            `json:"lookupUnclassified,omitempty"`
	SchemaSHA256         string         `json:"schemaSha256,omitempty"`
	LegacySampleSHA256   string         `json:"legacySampleSha256,omitempty"`
	EngineSampleSHA256   string         `json:"engineSampleSha256,omitempty"`
	PointSamples         int            `json:"pointSamples,omitempty"`
	CopySamples          int            `json:"copySamples,omitempty"`
	RelationshipSample   bool           `json:"relationshipSample,omitempty"`
	SampleDifferentialOK bool           `json:"sampleDifferentialOk"`
	FullDifferentialOK   bool           `json:"fullDifferentialOk"`
	FullRows             int            `json:"fullRows"`
	FullPointRows        int            `json:"fullPointRows"`
	LegacyFullSHA256     string         `json:"legacyFullSha256,omitempty"`
	EngineFullSHA256     string         `json:"engineFullSha256,omitempty"`
	RelationshipFullOK   bool           `json:"relationshipFullOk"`
	RelationshipSHA256   string         `json:"relationshipSha256,omitempty"`
	DifferentialOK       bool           `json:"differentialOk,omitempty"`
	DurationMillis       float64        `json:"durationMillis"`
}

type targetReport struct {
	Target           targetSpec     `json:"target"`
	BuildName        string         `json:"buildName"`
	BuildKey         string         `json:"buildKey"`
	ManifestTables   int            `json:"manifestTables"`
	RootPresent      int            `json:"rootPresent"`
	Loaded           int            `json:"loaded"`
	Missing          int            `json:"missing"`
	Errors           int            `json:"errors"`
	DifferentialFail int            `json:"differentialFailures"`
	Classes          map[string]int `json:"classes"`
	LookupStrategies map[string]int `json:"lookupStrategies"`
	LookupIndexBytes uint64         `json:"lookupIndexBytes"`
	Tables           []tableResult  `json:"tables"`
	DurationMillis   float64        `json:"durationMillis"`
}

type report struct {
	Schema             string         `json:"schema"`
	GeneratedAt        string         `json:"generatedAt"`
	DBDCommit          string         `json:"dbdCommit"`
	Workers            int            `json:"workers"`
	CacheBefore        int64          `json:"cacheBeforeBytes"`
	CacheAfter         int64          `json:"cacheAfterBytes"`
	CacheDelta         int64          `json:"cacheDeltaBytes"`
	PeakHeapAllocBytes uint64         `json:"peakHeapAllocBytes"`
	Targets            []targetReport `json:"targets"`
	TotalTables        int            `json:"totalTables"`
	TotalLoaded        int            `json:"totalLoaded"`
	TotalErrors        int            `json:"totalErrors"`
	DifferentialOK     bool           `json:"differentialOk"`
	DurationMillis     float64        `json:"durationMillis"`
	Status             string         `json:"status"`
}

func main() {
	home := flag.String("home", "", "isolated WOWDATA_HOME")
	out := flag.String("out", "analyze/benchmark/db2-corpus/report.json", "report path")
	targets := flag.String("targets", "cn/wow/zhCN,us/wow_classic_era/enUS", "comma-separated region/product/locale targets")
	workers := flag.Int("workers", 8, "bounded table workers")
	flag.Parse()
	if err := run(*home, *out, *targets, *workers); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(home, out, targetText string, workers int) error {
	started := time.Now()
	var peakHeap atomic.Uint64
	stopHeapMonitor := monitorHeap(&peakHeap)
	defer stopHeapMonitor()
	if home == "" {
		return fmt.Errorf("--home is required")
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return err
	}
	if workers < 1 {
		workers = 1
	}
	cacheRoot := filepath.Join(home, "cache")
	before, err := storage.DirSize(cacheRoot)
	if err != nil {
		return err
	}
	dbdCacheRoot := filepath.Join(cacheRoot, "dbd")
	revisionSource := appruntime.NewHTTPDBDRevisionSource(dbdCacheRoot, "https://api.github.com/repos/wowdev/WoWDBDefs/git/ref/heads/master")
	commit, err := revisionSource.Revision()
	if err != nil {
		return err
	}
	dbdRoot := filepath.Join(dbdCacheRoot, commit)
	manifestSource := appruntime.NewHTTPDBDManifestSource(dbdRoot, []string{
		"https://raw.githubusercontent.com/wowdev/WoWDBDefs/" + commit + "/manifest.json",
	}).WithCacheFirst()
	manifest, err := manifestSource.Manifest()
	if err != nil {
		return err
	}
	definitions := appruntime.NewHTTPDBDSource(dbdRoot, []string{
		"https://raw.githubusercontent.com/wowdev/WoWDBDefs/" + commit + "/definitions/%s.dbd",
	}).WithCacheFirst()

	result := report{Schema: "wowdata.db2-corpus.v2", GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), DBDCommit: commit, Workers: workers, CacheBefore: before, DifferentialOK: true, Status: "pass"}
	for _, target := range parseTargets(targetText) {
		targetResult, err := inspectTarget(cacheRoot, manifest, definitions, target, workers)
		if err != nil {
			return fmt.Errorf("%s/%s/%s: %w", target.Region, target.Product, target.Locale, err)
		}
		result.Targets = append(result.Targets, targetResult)
		result.TotalTables += targetResult.ManifestTables
		result.TotalLoaded += targetResult.Loaded
		result.TotalErrors += targetResult.Errors
		if targetResult.DifferentialFail > 0 {
			result.DifferentialOK = false
		}
	}
	updateReportStatus(&result)
	after, err := storage.DirSize(cacheRoot)
	if err != nil {
		return err
	}
	result.CacheAfter = after
	result.CacheDelta = after - before
	stopHeapMonitor()
	result.PeakHeapAllocBytes = peakHeap.Load()
	result.DurationMillis = float64(time.Since(started).Microseconds()) / 1000
	if err := storage.AtomicWriteJSON(out, result, 0644); err != nil {
		return err
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(encoded))
	if result.Status != "pass" {
		return fmt.Errorf("DB2 corpus gate failed: %d table errors, differential ok=%t", result.TotalErrors, result.DifferentialOK)
	}
	return nil
}

func updateReportStatus(result *report) {
	if result.TotalErrors > 0 || !result.DifferentialOK {
		result.Status = "fail"
		return
	}
	result.Status = "pass"
}

func inspectTarget(cacheRoot string, manifest *dbd.Manifest, definitions *appruntime.HTTPDBDSource, target targetSpec, workers int) (targetReport, error) {
	started := time.Now()
	remote := casc.NewCASCRemote(target.Region)
	remote.CacheRoot = cacheRoot
	remote.CacheMaxBytes = storage.DefaultCacheMaxBytes
	remote.Workers = workers
	remote.ManifestTTL = time.Hour
	locale, ok := casc.LocaleFlagByNameOK(target.Locale)
	if !ok {
		return targetReport{}, fmt.Errorf("unknown locale %s", target.Locale)
	}
	remote.Locale = locale
	if err := remote.InitProduct(target.Product); err != nil {
		return targetReport{}, err
	}
	if len(remote.Builds) == 0 {
		return targetReport{}, fmt.Errorf("no builds")
	}
	if err := remote.Preload(0); err != nil {
		return targetReport{}, err
	}
	names := manifest.TableNames()
	result := targetReport{Target: target, BuildName: remote.GetBuildName(), BuildKey: remote.GetBuildKey(), ManifestTables: len(names), Classes: make(map[string]int), LookupStrategies: make(map[string]int), Tables: make([]tableResult, len(names))}
	jobs := make(chan int)
	var completed atomic.Int64
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				name := names[index]
				id, _ := manifest.GetByTableName(name)
				result.Tables[index] = inspectTable(remote, definitions, target, name, id)
				done := completed.Add(1)
				if done%100 == 0 || done == int64(len(names)) {
					fmt.Fprintf(os.Stderr, "corpus %s/%s/%s: %d/%d tables\n", target.Region, target.Product, target.Locale, done, len(names))
				}
			}
		}()
	}
	for index := range names {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	for _, table := range result.Tables {
		switch table.Status {
		case "loaded", "loaded-empty-header":
			result.RootPresent++
			result.Loaded++
			result.Classes[table.Class]++
			for strategy, count := range table.LookupStrategies {
				result.LookupStrategies[strategy] += count
			}
			result.LookupIndexBytes += table.LookupIndexBytes
			if !table.DifferentialOK {
				result.DifferentialFail++
			}
		case "missing-root":
			result.Missing++
		default:
			result.RootPresent++
			result.Errors++
		}
	}
	result.DurationMillis = float64(time.Since(started).Microseconds()) / 1000
	return result, nil
}

func monitorHeap(peak *atomic.Uint64) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			for current := peak.Load(); stats.HeapAlloc > current && !peak.CompareAndSwap(current, stats.HeapAlloc); current = peak.Load() {
			}
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		once.Do(func() { close(stop) })
		<-done
	}
}

func inspectTable(remote *casc.CASCRemote, definitions *appruntime.HTTPDBDSource, target targetSpec, table string, fileDataID uint32) (result tableResult) {
	started := time.Now()
	result = tableResult{Table: table, FileDataID: fileDataID, Status: "error"}
	defer func() { result.DurationMillis = float64(time.Since(started).Microseconds()) / 1000 }()
	if !remote.FileExists(fileDataID) {
		result.Status = "missing-root"
		return result
	}
	data, err := remote.ReadFileDataPartial(fileDataID)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Bytes = len(data)
	if len(data) < 4 {
		result.Error = "file shorter than magic"
		return result
	}
	result.Magic = string(data[:4])
	rawDefinition, err := definitions.Definition(table)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	parser, err := dbd.Parse(strings.NewReader(rawDefinition))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	entry := parser.GetStructure(remote.GetBuildName(), "")
	if entry == nil {
		if reader, loadErr := db2.NewWDCReaderFromBytes(table, data, nil); loadErr == nil && isProvablyEmptyWDC(reader) {
			result.Status = "loaded-empty-header"
			result.HeaderOnly = true
			result.Rows = 0
			result.Sections = len(reader.Sections)
			result.SchemaSHA256 = hashJSON([]db2.SchemaField(nil))
			result.Compression = compressionCounts(reader.FieldInfo)
			profile := reader.PhysicalProfile()
			result.LookupStrategies = profile.Strategies
			result.LookupIndexedRows = profile.IndexedRows
			result.LookupDenseSlots = profile.DenseSlots
			result.LookupIndexBytes = profile.EstimatedBytes
			result.LookupUnclassified = profile.Unclassified
			result.Class = classifyWDC(reader, nil) + "/schema-less-header"
			result.LegacySampleSHA256 = hashJSON([]map[string]interface{}{})
			result.EngineSampleSHA256 = result.LegacySampleSHA256
			result.DifferentialOK = profile.Unclassified == 0 && strategyCount(profile.Strategies) == profile.Sections
			result.SampleDifferentialOK = true
			result.FullDifferentialOK = true
			result.RelationshipFullOK = true
			result.LegacyFullSHA256 = newCanonicalRowDigest().Sum()
			result.EngineFullSHA256 = result.LegacyFullSHA256
			if !result.DifferentialOK {
				result.Error = fmt.Sprintf("lookup dispatch classified %d of %d sections", strategyCount(profile.Strategies), profile.Sections)
			}
			return result
		}
		result.Error = fmt.Sprintf("no DBD structure for build %s", remote.GetBuildName())
		return result
	}
	schema, err := db2.SchemaFromDBD(entry)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.SchemaSHA256 = hashJSON(schema)

	var reader db2.RowReader
	switch binary.LittleEndian.Uint32(data) {
	case 0x43424457:
		dbcReader, loadErr := db2.NewDBCReaderFromBytes(table, remote.GetBuildName(), data, schema)
		if loadErr != nil {
			result.Error = loadErr.Error()
			return result
		}
		reader = dbcReader
		result.Rows = dbcReader.Size()
		result.Class = classifyDBC(schema, result.Rows)
	case 0x32434457, 0x33434457, 0x34434457, 0x35434457, 0x434c5331:
		wdcReader, loadErr := db2.NewWDCReaderFromBytes(table, data, schema)
		if loadErr != nil {
			result.Error = loadErr.Error()
			return result
		}
		reader = wdcReader
		result.Rows = wdcReader.Size()
		result.Sections = len(wdcReader.Sections)
		result.CopyRows = len(wdcReader.CopyTable)
		result.RelationshipValues = len(wdcReader.RelationshipLookup)
		result.Compression = compressionCounts(wdcReader.FieldInfo)
		profile := wdcReader.PhysicalProfile()
		result.LookupStrategies = profile.Strategies
		result.LookupIndexedRows = profile.IndexedRows
		result.LookupDenseSlots = profile.DenseSlots
		result.LookupIndexBytes = profile.EstimatedBytes
		result.LookupUnclassified = profile.Unclassified
		if profile.Unclassified != 0 || strategyCount(profile.Strategies) != profile.Sections {
			result.Error = fmt.Sprintf("lookup dispatch classified %d of %d sections", strategyCount(profile.Strategies), profile.Sections)
			return result
		}
		result.Class = classifyWDC(wdcReader, schema)
	default:
		result.Status = "error"
		result.Error = fmt.Sprintf("unsupported magic %q", result.Magic)
		return result
	}
	result.SampleDifferentialOK, result.PointSamples, result.CopySamples, result.RelationshipSample, result.LegacySampleSHA256, result.EngineSampleSHA256, result.Error = differential(reader, schema, table)
	fullOK, rows, pointRows, legacyFull, engineFull, relationshipOK, relationshipSHA, fullErr := fullDifferential(reader, schema, table)
	result.FullDifferentialOK = fullOK
	result.FullRows = rows
	result.FullPointRows = pointRows
	result.LegacyFullSHA256 = legacyFull
	result.EngineFullSHA256 = engineFull
	result.RelationshipFullOK = relationshipOK
	result.RelationshipSHA256 = relationshipSHA
	result.DifferentialOK = result.SampleDifferentialOK && result.FullDifferentialOK && result.RelationshipFullOK
	if result.Error == "" && fullErr != "" {
		result.Error = fullErr
	}
	result.Status = "loaded"
	return result
}

type canonicalRowDigest struct {
	hash hash.Hash
	rows int
}

func newCanonicalRowDigest() *canonicalRowDigest {
	return &canonicalRowDigest{hash: sha256.New()}
}

func (d *canonicalRowDigest) Add(row map[string]interface{}) error {
	encoded, err := json.Marshal(row)
	if err != nil {
		return err
	}
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(encoded)))
	_, _ = d.hash.Write(length[:])
	_, _ = d.hash.Write(encoded)
	d.rows++
	return nil
}

func (d *canonicalRowDigest) Sum() string { return hex.EncodeToString(d.hash.Sum(nil)) }

func fullDifferential(reader db2.RowReader, schema []db2.SchemaField, table string) (bool, int, int, string, string, bool, string, string) {
	streamer, ok := reader.(db2.ContextStreamRowReader)
	if !ok {
		return false, 0, 0, "", "", false, "", "reader has no bounded stream"
	}
	legacy := newCanonicalRowDigest()
	idField := "ID"
	pointReplay := true
	if wdcReader, ok := reader.(*db2.WDCReader); ok && wdcReader.IDField != "" {
		idField = wdcReader.IDField
		profile := wdcReader.PhysicalProfile()
		pointReplay = profile.Strategies[db2.LookupInlineScan] == 0
	}
	rowIDs := make([]uint32, 0)
	pointRows := 0
	if err := streamer.StreamRowsContext(context.Background(), nil, nil, 0, func(row map[string]interface{}) error {
		candidate := row
		if id, hasID := asUint32(row[idField]); hasID {
			rowIDs = append(rowIDs, id)
			if pointReplay {
				candidate = reader.GetRow(id)
				if candidate == nil {
					return fmt.Errorf("stream row ID %d is missing from point lookup", id)
				}
				pointRows++
			}
		}
		return legacy.Add(candidate)
	}); err != nil {
		return false, legacy.rows, pointRows, legacy.Sum(), "", false, "", err.Error()
	}
	engineDigest := newCanonicalRowDigest()
	engine := db2.NewEngine()
	if err := engine.Register(table, schema, reader); err != nil {
		return false, legacy.rows, pointRows, legacy.Sum(), "", false, "", err.Error()
	}
	snapshot := engine.Snapshot()
	_, stats, err := snapshot.Execute(context.Background(), db2.QueryPlan{Table: table, Mode: db2.PlanStream, Yield: engineDigest.Add})
	snapshot.Close()
	if err != nil {
		return false, legacy.rows, pointRows, legacy.Sum(), engineDigest.Sum(), false, "", err.Error()
	}
	expectedRows := legacy.rows
	if sized, ok := reader.(db2.SizedReader); ok {
		expectedRows = sized.Size()
	}
	rowsOK := legacy.rows == expectedRows && legacy.rows == engineDigest.rows && legacy.rows == stats.OutputRows && legacy.Sum() == engineDigest.Sum()
	relationshipOK, relationshipSHA, relationshipErr := validateFullRelationships(reader, rowIDs)
	if relationshipErr != "" {
		return rowsOK, legacy.rows, pointRows, legacy.Sum(), engineDigest.Sum(), false, relationshipSHA, relationshipErr
	}
	errorText := ""
	if !rowsOK || !relationshipOK {
		errorText = "full streaming reader and engine digest differ"
	}
	return rowsOK, legacy.rows, pointRows, legacy.Sum(), engineDigest.Sum(), relationshipOK, relationshipSHA, errorText
}

func validateFullRelationships(reader db2.RowReader, rowIDs []uint32) (bool, string, string) {
	wdcReader, ok := reader.(*db2.WDCReader)
	if !ok || len(wdcReader.RelationshipLookup) == 0 {
		return true, "", ""
	}
	expected := make([]uint64, 0)
	baseOffset := 0
	for sectionIndex := range wdcReader.Sections {
		section := &wdcReader.Sections[sectionIndex]
		if section.IsEncrypted {
			continue
		}
		for reference, value := range section.RelationshipMap {
			id := reference
			if section.IsNormal {
				index := baseOffset + int(reference)
				if index < 0 || index >= len(rowIDs) {
					return false, "", fmt.Sprintf("relationship reference %d exceeds streamed base rows", reference)
				}
				id = rowIDs[index]
			}
			expected = append(expected, uint64(value)<<32|uint64(id))
		}
		baseOffset += int(section.Header.RecordCount)
	}
	sort.Slice(expected, func(i, j int) bool { return expected[i] < expected[j] })
	expectedDigest := sha256.New()
	var tuple [8]byte
	for _, packed := range expected {
		binary.LittleEndian.PutUint32(tuple[0:4], uint32(packed>>32))
		binary.LittleEndian.PutUint32(tuple[4:8], uint32(packed))
		_, _ = expectedDigest.Write(tuple[:])
	}
	expectedSHA := hex.EncodeToString(expectedDigest.Sum(nil))

	digest := sha256.New()
	sortedRowIDs := append([]uint32(nil), rowIDs...)
	sort.Slice(sortedRowIDs, func(i, j int) bool { return sortedRowIDs[i] < sortedRowIDs[j] })
	values := make([]uint32, 0, len(wdcReader.RelationshipLookup))
	for value := range wdcReader.RelationshipLookup {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	lookupCount := 0
	for _, value := range values {
		ids := wdcReader.RelationshipLookup[value]
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			index := sort.Search(len(sortedRowIDs), func(index int) bool { return sortedRowIDs[index] >= id })
			if index == len(sortedRowIDs) || sortedRowIDs[index] != id {
				return false, hex.EncodeToString(digest.Sum(nil)), fmt.Sprintf("relationship %d references missing row %d", value, id)
			}
			binary.LittleEndian.PutUint32(tuple[0:4], value)
			binary.LittleEndian.PutUint32(tuple[4:8], id)
			_, _ = digest.Write(tuple[:])
			lookupCount++
		}
	}
	lookupSHA := hex.EncodeToString(digest.Sum(nil))
	if lookupCount != len(expected) || lookupSHA != expectedSHA {
		return false, lookupSHA, fmt.Sprintf("relationship tuples differ: lookup=%d expected=%d", lookupCount, len(expected))
	}
	return true, lookupSHA, ""
}

func isProvablyEmptyWDC(reader *db2.WDCReader) bool {
	if reader == nil || !reader.IsLoaded || reader.Size() != 0 || len(reader.CopyTable) != 0 || len(reader.RelationshipLookup) != 0 {
		return false
	}
	for index := range reader.Sections {
		section := &reader.Sections[index]
		header := section.Header
		if header.RecordCount != 0 || section.RecordDataSize != 0 || header.StringTableSize != 0 ||
			header.CopyTableSize != 0 || header.CopyTableCount != 0 || header.IDListSize != 0 ||
			header.RelationshipDataSize != 0 || header.OffsetMapIDCount != 0 || len(section.OffsetMap) != 0 {
			return false
		}
	}
	return true
}

func strategyCount(strategies map[string]int) int {
	total := 0
	for _, count := range strategies {
		total += count
	}
	return total
}

func differential(reader db2.RowReader, schema []db2.SchemaField, table string) (bool, int, int, bool, string, string, string) {
	scanner, ok := reader.(db2.ScanRowReader)
	if !ok {
		return false, 0, 0, false, "", "", "reader has no bounded scan"
	}
	legacyRows := scanner.Scan(nil, nil, 3)
	ids := make([]uint32, 0, len(legacyRows))
	idField := "ID"
	if wdcReader, ok := reader.(*db2.WDCReader); ok && wdcReader.IDField != "" {
		idField = wdcReader.IDField
	}
	for _, row := range legacyRows {
		if id, ok := asUint32(row[idField]); ok {
			ids = append(ids, id)
		}
	}
	if wdcReader, ok := reader.(*db2.WDCReader); ok {
		copyIDs := make([]uint32, 0, 2)
		for id := range wdcReader.CopyTable {
			copyIDs = append(copyIDs, id)
		}
		sort.Slice(copyIDs, func(i, j int) bool { return copyIDs[i] < copyIDs[j] })
		if len(copyIDs) > 2 {
			copyIDs = copyIDs[:2]
		}
		ids = append(ids, copyIDs...)
	}
	ids = uniqueIDs(ids)
	legacy := legacyRows
	plan := db2.QueryPlan{Table: table, Mode: db2.PlanRows, Limit: 3}
	if len(ids) > 0 {
		legacy = make([]map[string]interface{}, 0, len(ids))
		for _, id := range ids {
			if row := reader.GetRow(id); row != nil {
				legacy = append(legacy, row)
			}
		}
		plan.IDs = ids
		plan.Limit = 0
	}
	engine := db2.NewEngine()
	if err := engine.Register(table, schema, reader); err != nil {
		return false, 0, 0, false, "", "", err.Error()
	}
	snapshot := engine.Snapshot()
	engineResult, _, err := snapshot.Execute(context.Background(), plan)
	snapshot.Close()
	if err != nil {
		return false, 0, 0, false, "", "", err.Error()
	}
	relationshipOK := true
	relationshipSample := false
	if wdcReader, ok := reader.(*db2.WDCReader); ok && len(wdcReader.RelationshipLookup) > 0 {
		var value uint32
		minRows := int(^uint(0) >> 1)
		for candidate, recordIDs := range wdcReader.RelationshipLookup {
			if len(recordIDs) > 0 && (len(recordIDs) < minRows || (len(recordIDs) == minRows && candidate < value)) {
				value = candidate
				minRows = len(recordIDs)
			}
		}
		if minRows != int(^uint(0)>>1) {
			fullExpected := wdcReader.GetRelationshipRowsBatch([]uint32{value}, nil)[value]
			if relationshipField := inferRelationshipField(schema, fullExpected, value); relationshipField != "" {
				expected := fullExpected
				if len(expected) > 3 {
					expected = expected[:3]
				}
				snapshot = engine.Snapshot()
				actual, stats, relationErr := snapshot.Execute(context.Background(), db2.QueryPlan{Table: table, Mode: db2.PlanForeignKey, ForeignField: relationshipField, ForeignValue: value, Limit: 3})
				snapshot.Close()
				relationshipSample = true
				relationshipOK = relationErr == nil && stats.Physical == "relationship" && reflect.DeepEqual(expected, actual.Rows)
			}
		}
	}
	legacyHash := hashJSON(legacy)
	engineHash := hashJSON(engineResult.Rows)
	copySamples := 0
	if wdcReader, ok := reader.(*db2.WDCReader); ok {
		for _, id := range ids {
			if _, exists := wdcReader.CopyTable[id]; exists {
				copySamples++
			}
		}
	}
	ok = reflect.DeepEqual(legacy, engineResult.Rows) && relationshipOK
	errorText := ""
	if !ok {
		errorText = "legacy reader and engine sample differ"
	}
	return ok, len(ids) - copySamples, copySamples, relationshipSample, legacyHash, engineHash, errorText
}

func inferRelationshipField(schema []db2.SchemaField, rows []map[string]interface{}, value uint32) string {
	if len(rows) == 0 {
		return ""
	}
	match := func(name string) bool {
		for _, row := range rows {
			candidate, ok := asUint32(row[name])
			if !ok || candidate != value {
				return false
			}
		}
		return true
	}
	for _, field := range schema {
		if field.Type == db2.FieldRelation && match(field.Name) {
			return field.Name
		}
	}
	for _, field := range schema {
		if match(field.Name) {
			return field.Name
		}
	}
	return ""
}

func classifyWDC(reader *db2.WDCReader, schema []db2.SchemaField) string {
	layout := "normal"
	for _, section := range reader.Sections {
		if !section.IsNormal {
			layout = "sparse"
			break
		}
	}
	features := []string{fmt.Sprintf("WDC%d", reader.WDCVersion), layout, fmt.Sprintf("s%d", len(reader.Sections)), scale(reader.Size())}
	if len(reader.CopyTable) > 0 {
		features = append(features, "copy")
	}
	if len(reader.RelationshipLookup) > 0 {
		features = append(features, "relation")
	}
	if schemaHas(schema, db2.FieldString) {
		features = append(features, "string")
	}
	if schemaHasArray(schema) {
		features = append(features, "array")
	}
	compressions := compressionCounts(reader.FieldInfo)
	keys := make([]string, 0, len(compressions))
	for key, count := range compressions {
		if count > 0 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	features = append(features, keys...)
	return strings.Join(features, "/")
}

func classifyDBC(schema []db2.SchemaField, rows int) string {
	features := []string{"WDBC", scale(rows)}
	if schemaHas(schema, db2.FieldString) {
		features = append(features, "string")
	}
	if schemaHasArray(schema) {
		features = append(features, "array")
	}
	return strings.Join(features, "/")
}

func compressionCounts(infos []db2.FieldStorageInfo) map[string]int {
	out := make(map[string]int)
	for _, info := range infos {
		name := map[db2.CompressionType]string{0: "none", 1: "bitpacked", 2: "common", 3: "pallet", 4: "pallet-array", 5: "signed"}[info.FieldCompression]
		if name == "" {
			name = "unknown-" + strconv.FormatUint(uint64(info.FieldCompression), 10)
		}
		out[name]++
	}
	return out
}

func scale(rows int) string {
	switch {
	case rows == 0:
		return "empty"
	case rows < 100:
		return "tiny"
	case rows < 10000:
		return "small"
	case rows < 1000000:
		return "medium"
	default:
		return "large"
	}
}

func schemaHas(schema []db2.SchemaField, fieldType db2.FieldType) bool {
	for _, field := range schema {
		if field.Type == fieldType {
			return true
		}
	}
	return false
}
func schemaHasArray(schema []db2.SchemaField) bool {
	for _, field := range schema {
		if field.ArrayLen > 0 {
			return true
		}
	}
	return false
}

func asUint32(value interface{}) (uint32, bool) {
	switch v := value.(type) {
	case uint8:
		return uint32(v), true
	case uint16:
		return uint32(v), true
	case uint32:
		return v, true
	case uint:
		if uint64(v) <= uint64(^uint32(0)) {
			return uint32(v), true
		}
	case int8:
		if v >= 0 {
			return uint32(v), true
		}
	case int16:
		if v >= 0 {
			return uint32(v), true
		}
	case int32:
		if v >= 0 {
			return uint32(v), true
		}
	case uint64:
		if v <= uint64(^uint32(0)) {
			return uint32(v), true
		}
	case int64:
		if v >= 0 && uint64(v) <= uint64(^uint32(0)) {
			return uint32(v), true
		}
	case int:
		if v >= 0 && uint64(v) <= uint64(^uint32(0)) {
			return uint32(v), true
		}
	}
	return 0, false
}

func uniqueIDs(ids []uint32) []uint32 {
	seen := make(map[uint32]struct{}, len(ids))
	out := ids[:0]
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

func hashJSON(value interface{}) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func parseTargets(value string) []targetSpec {
	var targets []targetSpec
	for _, item := range strings.Split(value, ",") {
		parts := strings.Split(strings.TrimSpace(item), "/")
		if len(parts) == 3 {
			targets = append(targets, targetSpec{Region: parts[0], Product: parts[1], Locale: parts[2]})
		}
	}
	return targets
}
