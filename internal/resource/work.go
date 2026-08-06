package resource

import (
	"sort"
	"sync"
	"sync/atomic"
)

const MetricsSchema = "wowdata.resource-metrics.v2"

type WorkCounter struct {
	Class string `json:"class"`
	Unit  string `json:"unit"`
	Units uint64 `json:"units"`
}

type WorkSnapshot struct {
	Complete         bool          `json:"complete"`
	UncoveredClasses []string      `json:"uncoveredClasses"`
	Counters         []WorkCounter `json:"counters"`
}

type MetricsSnapshot struct {
	Schema string `json:"schema"`
	SchedulerSnapshot
	Work              WorkSnapshot        `json:"work"`
	Stages            StageSnapshot       `json:"stages"`
	StageDependencies map[string][]string `json:"stageDependencies"`
}

type atomicDB2Work struct {
	inputBytes, metadataBytes, rowsDecoded, fieldsDecoded atomic.Uint64
	rowsVisited, predicatesEvaluated, fieldsProjected     atomic.Uint64
	relationshipProbes                                    atomic.Uint64
}

type workMetrics struct {
	coverageMu                                        sync.Mutex
	commandRegistered                                 bool
	requiredClasses                                   map[string]struct{}
	blteCompressedBytes, blteDecodedBytes, blteBlocks atomic.Uint64
	blteNormalBlocks, blteZlibBlocks                  atomic.Uint64
	blteNestedBlocks, blteEncryptedBlocks             atomic.Uint64
	blteNormalBytes, blteZlibBytes                    atomic.Uint64
	blteNestedBytes, blteEncryptedBytes               atomic.Uint64
	wdc, dbc                                          atomicDB2Work
	wdcEncryptionScanBytes                            atomic.Uint64
	dbdInputBytes, dbdLinesParsed                     atomic.Uint64
	dbdDefinitionsParsed, dbdFieldsParsed             atomic.Uint64
	blpInputBytes, blpPixelsDecoded                   atomic.Uint64
	pngImages, pngPixels, pngOutputBytes              atomic.Uint64
	webpImages, webpPixels, webpOutputBytes           atomic.Uint64
	cascMetadataBytes, fileReadBytes, fileWriteBytes  atomic.Uint64
	jsonEncodedBytes, listfileParsedBytes             atomic.Uint64
	sha256Bytes, videoDemuxBytes                      atomic.Uint64
}

var currentWork atomic.Pointer[workMetrics]

func init() { ResetWorkMetrics() }

func ResetWorkMetrics() { currentWork.Store(&workMetrics{}) }

func BeginWork(commandRegistered bool, requiredClasses []string) {
	m := meter()
	m.coverageMu.Lock()
	m.commandRegistered = commandRegistered
	m.requiredClasses = make(map[string]struct{}, len(requiredClasses))
	for _, class := range requiredClasses {
		if class != "" {
			m.requiredClasses[class] = struct{}{}
		}
	}
	m.coverageMu.Unlock()
}

func SnapshotMetrics(scheduler *Scheduler) MetricsSnapshot {
	var schedulerSnapshot SchedulerSnapshot
	if scheduler != nil {
		schedulerSnapshot = scheduler.Snapshot()
	}
	return MetricsSnapshot{Schema: MetricsSchema, SchedulerSnapshot: schedulerSnapshot, Work: SnapshotWorkMetrics(), Stages: SnapshotStages(), StageDependencies: SnapshotStageDependencies()}
}

func SnapshotWorkMetrics() WorkSnapshot {
	m := meter()
	result := WorkSnapshot{Counters: make([]WorkCounter, 0, 32)}
	appendCounter := func(class, unit string, units uint64) {
		if units > 0 {
			result.Counters = append(result.Counters, WorkCounter{Class: class, Unit: unit, Units: units})
		}
	}
	appendCounter("blte-normal-decode", "decoded-bytes", m.blteNormalBytes.Load())
	appendCounter("blte-zlib-decode", "decoded-bytes", m.blteZlibBytes.Load())
	appendCounter("blte-nested-decode", "decoded-bytes", m.blteNestedBytes.Load())
	appendCounter("blte-encrypted-decode", "decoded-bytes", m.blteEncryptedBytes.Load())
	appendDB2Counters := func(prefix string, work *atomicDB2Work) {
		appendCounter(prefix+"-open", "metadata-bytes", work.metadataBytes.Load())
		appendCounter(prefix+"-row-decode", "fields", work.fieldsDecoded.Load())
		appendCounter(prefix+"-row-visit", "rows", work.rowsVisited.Load())
		appendCounter(prefix+"-predicate", "predicates", work.predicatesEvaluated.Load())
		appendCounter(prefix+"-project", "fields", work.fieldsProjected.Load())
		appendCounter(prefix+"-relationship", "probes", work.relationshipProbes.Load())
	}
	appendDB2Counters("wdc", &m.wdc)
	appendCounter("wdc-encryption-scan", "input-bytes", m.wdcEncryptionScanBytes.Load())
	appendDB2Counters("dbc", &m.dbc)
	appendCounter("dbd-parse", "input-bytes", m.dbdInputBytes.Load())
	appendCounter("blp-decode", "pixels", m.blpPixelsDecoded.Load())
	appendCounter("png-encode", "pixels", m.pngPixels.Load())
	appendCounter("webp-encode", "pixels", m.webpPixels.Load())
	appendCounter("casc-metadata-parse", "input-bytes", m.cascMetadataBytes.Load())
	appendCounter("file-read", "bytes", m.fileReadBytes.Load())
	appendCounter("file-write", "bytes", m.fileWriteBytes.Load())
	appendCounter("json-encode", "output-bytes", m.jsonEncodedBytes.Load())
	appendCounter("listfile-parse", "input-bytes", m.listfileParsedBytes.Load())
	appendCounter("sha256", "input-bytes", m.sha256Bytes.Load())
	appendCounter("video-demux", "input-bytes", m.videoDemuxBytes.Load())

	m.coverageMu.Lock()
	registered := m.commandRegistered
	required := make(map[string]struct{}, len(m.requiredClasses)+len(result.Counters))
	for class := range m.requiredClasses {
		required[class] = struct{}{}
	}
	m.coverageMu.Unlock()
	for _, counter := range result.Counters {
		required[counter.Class] = struct{}{}
	}
	if !registered {
		result.UncoveredClasses = append(result.UncoveredClasses, "unregistered-command")
	}
	for class := range required {
		if !calibratedWorkClasses[class] {
			result.UncoveredClasses = append(result.UncoveredClasses, class)
		}
	}
	sort.Strings(result.UncoveredClasses)
	result.Complete = registered && len(result.UncoveredClasses) == 0
	return result
}

var calibratedWorkClasses = map[string]bool{
	"blte-normal-decode": true, "blte-zlib-decode": true, "blte-nested-decode": true, "blte-encrypted-decode": true,
	"wdc-open": true, "wdc-row-decode": true, "wdc-row-visit": true, "wdc-predicate": true, "wdc-project": true, "wdc-relationship": true, "wdc-encryption-scan": true,
	"dbc-open": true, "dbc-row-decode": true, "dbc-row-visit": true, "dbc-predicate": true, "dbc-project": true, "dbc-relationship": true,
	"dbd-parse": true, "blp-decode": true, "png-encode": true, "webp-encode": true,
	"casc-metadata-parse": true, "file-read": true, "file-write": true, "json-encode": true, "listfile-parse": true, "sha256": true, "video-demux": true,
}

func CalibratedWorkClasses() []string {
	classes := make([]string, 0, len(calibratedWorkClasses))
	for class := range calibratedWorkClasses {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	return classes
}

func RecordBLTEBlock(kind byte, compressedBytes, decodedBytes int) {
	m := meter()
	addInt(&m.blteCompressedBytes, compressedBytes)
	addInt(&m.blteDecodedBytes, decodedBytes)
	m.blteBlocks.Add(1)
	switch kind {
	case 'N':
		m.blteNormalBlocks.Add(1)
		addInt(&m.blteNormalBytes, decodedBytes)
	case 'Z':
		m.blteZlibBlocks.Add(1)
		addInt(&m.blteZlibBytes, decodedBytes)
	case 'F':
		m.blteNestedBlocks.Add(1)
		addInt(&m.blteNestedBytes, decodedBytes)
	case 'E':
		m.blteEncryptedBlocks.Add(1)
		addInt(&m.blteEncryptedBytes, decodedBytes)
	}
}

func RecordWDCOpen(inputBytes, metadataBytes int) {
	recordDB2Open(&meter().wdc, inputBytes, metadataBytes)
}
func RecordWDCEncryptionScan(bytes int) { addInt(&meter().wdcEncryptionScanBytes, bytes) }
func RecordDBCOpen(inputBytes, metadataBytes int) {
	recordDB2Open(&meter().dbc, inputBytes, metadataBytes)
}
func RecordWDCDecode(fields int) { recordDB2Decode(&meter().wdc, fields) }
func RecordDBCDecode(fields int) { recordDB2Decode(&meter().dbc, fields) }

func RecordWDCQuery(rowsVisited, predicatesEvaluated, fieldsProjected, relationshipProbes int) {
	recordDB2Query(&meter().wdc, rowsVisited, predicatesEvaluated, fieldsProjected, relationshipProbes)
}

func RecordDBCQuery(rowsVisited, predicatesEvaluated, fieldsProjected, relationshipProbes int) {
	recordDB2Query(&meter().dbc, rowsVisited, predicatesEvaluated, fieldsProjected, relationshipProbes)
}

func RecordDBDInput(bytes, lines int) {
	m := meter()
	addInt(&m.dbdInputBytes, bytes)
	addInt(&m.dbdLinesParsed, lines)
}

func RecordDBDDefinition(fields int) {
	m := meter()
	m.dbdDefinitionsParsed.Add(1)
	addInt(&m.dbdFieldsParsed, fields)
}

func RecordBLPOpen(inputBytes int) { addInt(&meter().blpInputBytes, inputBytes) }
func RecordBLPDecode(pixels int)   { addInt(&meter().blpPixelsDecoded, pixels) }

func RecordImageEncode(format string, pixels, outputBytes int) {
	m := meter()
	switch format {
	case "png":
		m.pngImages.Add(1)
		addInt(&m.pngPixels, pixels)
		addInt(&m.pngOutputBytes, outputBytes)
	case "webp":
		m.webpImages.Add(1)
		addInt(&m.webpPixels, pixels)
		addInt(&m.webpOutputBytes, outputBytes)
	}
}

func RecordCASCMetadata(bytes int)  { addInt(&meter().cascMetadataBytes, bytes) }
func RecordFileRead(bytes int)      { addInt(&meter().fileReadBytes, bytes) }
func RecordFileWrite(bytes int)     { addInt(&meter().fileWriteBytes, bytes) }
func RecordJSONEncode(bytes int)    { addInt(&meter().jsonEncodedBytes, bytes) }
func RecordListfileParse(bytes int) { addInt(&meter().listfileParsedBytes, bytes) }
func RecordSHA256(bytes int)        { addInt(&meter().sha256Bytes, bytes) }
func RecordVideoDemux(bytes int)    { addInt(&meter().videoDemuxBytes, bytes) }

func meter() *workMetrics {
	if m := currentWork.Load(); m != nil {
		return m
	}
	m := &workMetrics{}
	if currentWork.CompareAndSwap(nil, m) {
		return m
	}
	return currentWork.Load()
}

func addInt(counter *atomic.Uint64, value int) {
	if value > 0 {
		counter.Add(uint64(value))
	}
}

func recordDB2Open(work *atomicDB2Work, inputBytes, metadataBytes int) {
	addInt(&work.inputBytes, inputBytes)
	addInt(&work.metadataBytes, metadataBytes)
}

func recordDB2Decode(work *atomicDB2Work, fields int) {
	work.rowsDecoded.Add(1)
	addInt(&work.fieldsDecoded, fields)
}

func recordDB2Query(work *atomicDB2Work, rowsVisited, predicatesEvaluated, fieldsProjected, relationshipProbes int) {
	addInt(&work.rowsVisited, rowsVisited)
	addInt(&work.predicatesEvaluated, predicatesEvaluated)
	addInt(&work.fieldsProjected, fieldsProjected)
	addInt(&work.relationshipProbes, relationshipProbes)
}
