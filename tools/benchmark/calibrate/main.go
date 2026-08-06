// Command calibrate measures local machine limits used by wowdata performance runs.
package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"wowdata/internal/blp"
	"wowdata/internal/blte"
	"wowdata/internal/casc"
	"wowdata/internal/crypto"
	"wowdata/internal/db2"
	"wowdata/internal/dbd"
	"wowdata/internal/listfile"
	"wowdata/internal/storage"
	"wowdata/internal/tact"
	"wowdata/internal/video"

	"github.com/HugoSmits86/nativewebp"
)

const schemaVersion = 1

type config struct {
	Output      string
	TempDir     string
	FileMiB     int
	Duration    time.Duration
	RandomReads int
	Inputs      codecInputs
}

type codecInputs struct {
	BLTE string
	WDC  string
	DBC  string
	DBD  string
	BLP  string
}

type report struct {
	SchemaVersion int                `json:"schemaVersion"`
	GeneratedAt   time.Time          `json:"generatedAt"`
	Command       string             `json:"command"`
	Machine       machineReport      `json:"machine"`
	Limits        limitReport        `json:"limits"`
	Disk          diskReport         `json:"disk"`
	Compute       []measurement      `json:"compute"`
	Storage       []measurement      `json:"storage"`
	Codecs        []codecMeasurement `json:"codecs"`
	Notes         []string           `json:"notes"`
}

type machineReport struct {
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	GoVersion       string `json:"goVersion"`
	LogicalCores    int    `json:"logicalCores"`
	PhysicalCores   int    `json:"physicalCores,omitempty"`
	CoreProbe       string `json:"coreProbe"`
	GOMAXPROCS      int    `json:"gomaxprocs"`
	TotalMemory     uint64 `json:"totalMemoryBytes,omitempty"`
	AvailableMemory uint64 `json:"availableMemoryBytes,omitempty"`
	MemoryProbe     string `json:"memoryProbe"`
}

type limitReport struct {
	OpenFiles       uint64 `json:"openFiles,omitempty"`
	OpenFilesProbe  string `json:"openFilesProbe"`
	Connections     int    `json:"connections"`
	ConnectionProbe string `json:"connectionProbe"`
}

type diskReport struct {
	Directory            string  `json:"directory"`
	TemporaryBytes       int64   `json:"temporaryBytes"`
	SequentialWriteBps   float64 `json:"sequentialWriteBytesPerSecond"`
	SequentialReadBps    float64 `json:"sequentialReadBytesPerSecond"`
	RandomReadP50Nanos   int64   `json:"randomReadP50Nanos"`
	RandomReadP95Nanos   int64   `json:"randomReadP95Nanos"`
	RandomReadMaxNanos   int64   `json:"randomReadMaxNanos"`
	RandomReadBlockBytes int     `json:"randomReadBlockBytes"`
	RandomReadSamples    int     `json:"randomReadSamples"`
	RandomReadOperations int     `json:"randomReadOperations"`
	RandomReadCacheMode  string  `json:"randomReadCacheMode"`
}

type measurement struct {
	Name           string  `json:"name"`
	Bytes          int64   `json:"bytes"`
	Iterations     int64   `json:"iterations"`
	ElapsedNs      int64   `json:"elapsedNanos"`
	BytesPerSecond float64 `json:"bytesPerSecond"`
	ResourceClass  string  `json:"resourceClass"`
	WorkUnit       string  `json:"workUnit"`
	Units          int64   `json:"units"`
	UnitsPerSecond float64 `json:"unitsPerSecond"`
}

type codecMeasurement struct {
	measurement
	Source     string `json:"source"`
	InputBytes int    `json:"inputBytes"`
	Mode       string `json:"mode"`
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Output, "output", "", "write JSON to this path (stdout when empty)")
	flag.StringVar(&cfg.TempDir, "temp-dir", "", "directory used for disposable disk calibration data")
	flag.IntVar(&cfg.FileMiB, "file-mib", 64, "temporary disk calibration file size in MiB")
	flag.DurationVar(&cfg.Duration, "duration", 500*time.Millisecond, "minimum duration for each compute/codec sample")
	flag.IntVar(&cfg.RandomReads, "random-reads", 2048, "number of buffered 4 KiB random-read latency samples")
	flag.StringVar(&cfg.Inputs.BLTE, "blte", "", "real BLTE file; synthetic data is used when omitted")
	flag.StringVar(&cfg.Inputs.WDC, "wdc", "", "real WDC file; synthetic data is used when omitted")
	flag.StringVar(&cfg.Inputs.DBC, "dbc", "", "real DBC file; synthetic data is used when omitted")
	flag.StringVar(&cfg.Inputs.DBD, "dbd", "", "real DBD file; synthetic data is used when omitted")
	flag.StringVar(&cfg.Inputs.BLP, "blp", "", "real BLP file; synthetic data is used when omitted")
	flag.Parse()

	r, err := calibrate(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "calibrate:", err)
		os.Exit(1)
	}
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "calibrate:", err)
		os.Exit(1)
	}
	encoded = append(encoded, '\n')
	if cfg.Output == "" {
		_, err = os.Stdout.Write(encoded)
	} else {
		err = os.MkdirAll(filepath.Dir(cfg.Output), 0o755)
		if err == nil {
			err = os.WriteFile(cfg.Output, encoded, 0o644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "calibrate:", err)
		os.Exit(1)
	}
}

func calibrate(cfg config) (report, error) {
	if cfg.FileMiB < 1 || cfg.FileMiB > 1024 {
		return report{}, errors.New("file-mib must be between 1 and 1024")
	}
	if cfg.Duration <= 0 || cfg.RandomReads < 1 {
		return report{}, errors.New("duration and random-reads must be positive")
	}
	if cfg.TempDir == "" {
		cfg.TempDir = os.TempDir()
	}
	absTemp, err := filepath.Abs(cfg.TempDir)
	if err != nil {
		return report{}, err
	}
	machine, limits := inspectSystem()
	disk, err := calibrateDisk(absTemp, int64(cfg.FileMiB)<<20, cfg.RandomReads)
	if err != nil {
		return report{}, err
	}
	compute := calibrateCompute(cfg.Duration)
	storageMeasurements, err := calibrateStorage(absTemp, cfg.Duration)
	if err != nil {
		return report{}, err
	}
	codecs, err := calibrateCodecs(cfg.Duration, cfg.Inputs)
	if err != nil {
		return report{}, err
	}
	return report{
		SchemaVersion: schemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Command:       strings.Join(os.Args, " "),
		Machine:       machine,
		Limits:        limits,
		Disk:          disk,
		Compute:       compute,
		Storage:       storageMeasurements,
		Codecs:        codecs,
		Notes: []string{
			"Synthetic codec fixtures are startup calibration only; final corpus reports should pass representative real files.",
			"The disposable disk file is removed before this command exits.",
			"OS page cache can affect sequential read throughput; raw samples retain the probe directory and byte count.",
		},
	}, nil
}

func calibrateDisk(dir string, size int64, randomReads int) (diskReport, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return diskReport{}, err
	}
	f, err := os.CreateTemp(dir, "wowdata-calibration-*.tmp")
	if err != nil {
		return diskReport{}, err
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()

	block := make([]byte, 1<<20)
	for i := range block {
		block[i] = byte(i*31 + 17)
	}
	written := int64(0)
	start := time.Now()
	for written < size {
		chunk := block
		if left := size - written; left < int64(len(chunk)) {
			chunk = chunk[:left]
		}
		n, writeErr := f.Write(chunk)
		written += int64(n)
		if writeErr != nil {
			return diskReport{}, writeErr
		}
	}
	if err := f.Sync(); err != nil {
		return diskReport{}, err
	}
	writeElapsed := time.Since(start)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return diskReport{}, err
	}
	start = time.Now()
	readBytes, err := io.Copy(io.Discard, f)
	if err != nil {
		return diskReport{}, err
	}
	readElapsed := time.Since(start)

	randBlock := make([]byte, 4096)
	latencies := make([]int64, randomReads)
	rng := rand.New(rand.NewSource(1))
	blocks := size / int64(len(randBlock))
	const operationsPerSample = 64
	for sample := range latencies {
		started := time.Now()
		for i := 0; i < operationsPerSample; i++ {
			offset := rng.Int63n(blocks) * int64(len(randBlock))
			if _, err := f.ReadAt(randBlock, offset); err != nil {
				return diskReport{}, err
			}
		}
		latencies[sample] = time.Since(started).Nanoseconds() / operationsPerSample
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	return diskReport{
		Directory: filepath.ToSlash(dir), TemporaryBytes: size,
		SequentialWriteBps: rate(written, writeElapsed), SequentialReadBps: rate(readBytes, readElapsed),
		RandomReadP50Nanos: percentile(latencies, 0.50), RandomReadP95Nanos: percentile(latencies, 0.95),
		RandomReadMaxNanos: latencies[len(latencies)-1], RandomReadBlockBytes: len(randBlock),
		RandomReadSamples: randomReads, RandomReadOperations: randomReads * operationsPerSample,
		RandomReadCacheMode: "os-default-buffered",
	}, nil
}

func calibrateCompute(duration time.Duration) []measurement {
	data := make([]byte, 16<<20)
	for i := range data {
		data[i] = byte(i*13 + 5)
	}
	sha := measure("sha256", int64(len(data)), duration, func() error {
		sum := sha256.Sum256(data)
		if sum[0] == 0xff && sum[1] == 0xff {
			return errors.New("impossible digest")
		}
		return nil
	})
	setWorkMeasurement(&sha, "sha256-single", "input-bytes")
	workers := runtime.GOMAXPROCS(0)
	parallelSHA := measure("sha256-parallel", int64(len(data))*int64(workers), duration, func() error {
		var wg sync.WaitGroup
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				_ = sha256.Sum256(data)
			}()
		}
		wg.Wait()
		return nil
	})
	setWorkMeasurement(&parallelSHA, "sha256", "input-bytes")
	payload := makeJSONPayload(4096)
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	jsonMeasure := measure("json-encode", int64(len(encodedPayload))+1, duration, func() error {
		enc := json.NewEncoder(io.Discard)
		return enc.Encode(payload)
	})
	setWorkMeasurement(&jsonMeasure, "json-encode", "output-bytes")
	return []measurement{sha, parallelSHA, jsonMeasure}
}

func calibrateStorage(dir string, duration time.Duration) ([]measurement, error) {
	payload := make([]byte, 16<<20)
	for i := range payload {
		payload[i] = byte(i*19 + 11)
	}
	path := filepath.Join(dir, "wowdata-work-calibration.tmp")
	defer os.Remove(path)
	if err := storage.AtomicWriteFile(path, payload, 0o600); err != nil {
		return nil, err
	}
	write := measure("file-write", int64(len(payload)), duration, func() error {
		return storage.AtomicWriteFile(path, payload, 0o600)
	})
	setWorkMeasurement(&write, "file-write", "bytes")
	read := measure("file-read", int64(len(payload)), duration, func() error {
		data, err := os.ReadFile(path)
		if len(data) != len(payload) && err == nil {
			return fmt.Errorf("file read returned %d bytes, want %d", len(data), len(payload))
		}
		return err
	})
	setWorkMeasurement(&read, "file-read", "bytes")
	return []measurement{read, write}, nil
}

func calibrateCodecs(duration time.Duration, inputs codecInputs) ([]codecMeasurement, error) {
	type codecCase struct {
		name, path, mode, resourceClass, workUnit string
		synthetic                                 func() []byte
		prepare                                   func([]byte) (func() error, int64, error)
	}
	cases := []codecCase{
		{"blte-normal", inputs.BLTE, "decode-normal", "blte-normal-decode", "decoded-bytes", syntheticBLTE, prepareBLTE},
		{"blte-zlib", "", "decode-zlib", "blte-zlib-decode", "decoded-bytes", syntheticZlibBLTE, prepareBLTE},
		{"blte-nested", "", "decode-nested-frame", "blte-nested-decode", "decoded-bytes", syntheticNestedBLTE, prepareBLTE},
		{"blte-encrypted", "", "decrypt-salsa20", "blte-encrypted-decode", "decoded-bytes", syntheticEncryptedBLTE, prepareEncryptedBLTE},
		{"wdc-open", inputs.WDC, "parse-header-sections", "wdc-open", "metadata-bytes", func() []byte { return db2.BuildMinimalWDC2ForTest() }, prepareWDCOpen},
		{"wdc-row-decode", inputs.WDC, "decode-all-rows", "wdc-row-decode", "fields", func() []byte { return db2.BuildMinimalWDC2ForTest() }, prepareWDCRows},
		{"wdc-encryption-scan", "", "scan-zeroed-section", "wdc-encryption-scan", "input-bytes", syntheticScanData, prepareEncryptionScan},
		{"dbc-open", inputs.DBC, "parse-record-layout", "dbc-open", "metadata-bytes", syntheticDBC, prepareDBCOpen},
		{"dbc-row-decode", inputs.DBC, "decode-all-rows", "dbc-row-decode", "fields", syntheticDBC, prepareDBCRows},
		{"dbd-parse", inputs.DBD, "parse-definition", "dbd-parse", "input-bytes", syntheticDBD, prepareDBD},
		{"blp-decode", inputs.BLP, "decode-mipmap-rgba", "blp-decode", "pixels", syntheticBLP, prepareBLP},
		{"png-encode", inputs.BLP, "encode-rgba", "png-encode", "pixels", syntheticBLP, preparePNG},
		{"webp-encode", inputs.BLP, "encode-lossless-rgba", "webp-encode", "pixels", syntheticBLP, prepareWebP},
		{"casc-metadata", "", "parse-archive-index", "casc-metadata-parse", "input-bytes", syntheticArchiveIndex, prepareCASCMetadata},
		{"listfile-parse", "", "parse-text-listfile", "listfile-parse", "input-bytes", syntheticListfile, prepareListfile},
		{"video-demux", "", "parse-header-and-frames", "video-demux", "input-bytes", syntheticAVI, prepareVideo},
	}
	for _, prefix := range []string{"wdc", "dbc"} {
		for _, query := range []struct{ suffix, unit string }{{"row-visit", "rows"}, {"predicate", "predicates"}, {"project", "fields"}, {"relationship", "probes"}} {
			class := prefix + "-" + query.suffix
			cases = append(cases, codecCase{class, "", "query-primitive", class, query.unit, syntheticQueryData, prepareQueryPrimitive(query.suffix)})
		}
	}
	result := make([]codecMeasurement, 0, len(cases))
	for _, c := range cases {
		data, source, err := loadInput(c.path, c.synthetic)
		if err != nil {
			return nil, fmt.Errorf("%s input: %w", c.name, err)
		}
		run, unitsPerIteration, err := c.prepare(data)
		if err != nil {
			return nil, fmt.Errorf("%s preparation: %w", c.name, err)
		}
		if unitsPerIteration <= 0 {
			return nil, fmt.Errorf("%s preparation returned %d %s", c.name, unitsPerIteration, c.workUnit)
		}
		if err := run(); err != nil {
			return nil, fmt.Errorf("%s validation: %w", c.name, err)
		}
		m := measure(c.name, unitsPerIteration, duration, run)
		setWorkMeasurement(&m, c.resourceClass, c.workUnit)
		result = append(result, codecMeasurement{
			measurement: m, Source: source, InputBytes: len(data), Mode: c.mode,
		})
	}
	return result, nil
}

func prepareBLTE(data []byte) (func() error, int64, error) {
	r, err := blte.NewReader(data)
	if err != nil {
		return nil, 0, err
	}
	decoded, err := r.ReadAll()
	if err != nil {
		return nil, 0, err
	}
	return func() error { return decodeBLTE(data) }, int64(len(decoded)), nil
}

func prepareEncryptedBLTE(data []byte) (func() error, int64, error) {
	keys := tact.NewKeyRing()
	keys.AddKey(calibrationKeyName, calibrationKeyHex)
	run := func() error {
		reader, err := blte.NewReaderWithKeys(data, keys)
		if err != nil {
			return err
		}
		_, err = reader.ReadAll()
		return err
	}
	reader, err := blte.NewReaderWithKeys(data, keys)
	if err != nil {
		return nil, 0, err
	}
	decoded, err := reader.ReadAll()
	if err != nil {
		return nil, 0, err
	}
	return run, int64(len(decoded)), nil
}

func prepareWDCOpen(data []byte) (func() error, int64, error) {
	reader, err := openWDC(data)
	if err != nil {
		return nil, 0, err
	}
	return func() error { return decodeWDC(data) }, int64(reader.MetadataBytes()), nil
}

func prepareWDCRows(data []byte) (func() error, int64, error) {
	reader, err := openWDC(data)
	if err != nil {
		return nil, 0, err
	}
	run := func() error {
		return reader.StreamRowsContext(context.Background(), nil, nil, 0, func(map[string]interface{}) error { return nil })
	}
	return run, int64(reader.Size() * len(reader.Schema)), nil
}

func prepareDBCOpen(data []byte) (func() error, int64, error) {
	reader, err := openDBC(data)
	if err != nil {
		return nil, 0, err
	}
	return func() error { _, err := openDBC(data); return err }, int64(reader.MetadataBytes()), nil
}

func prepareDBCRows(data []byte) (func() error, int64, error) {
	reader, err := openDBC(data)
	if err != nil {
		return nil, 0, err
	}
	run := func() error {
		return reader.StreamRowsContext(context.Background(), nil, nil, 0, func(map[string]interface{}) error { return nil })
	}
	return run, int64(reader.Size() * len(reader.Schema)), nil
}

func prepareDBD(data []byte) (func() error, int64, error) {
	return func() error { return decodeDBD(data) }, int64(len(data)), nil
}

func prepareBLP(data []byte) (func() error, int64, error) {
	img, err := blp.Decode(data)
	if err != nil {
		return nil, 0, err
	}
	return func() error { _, _, _, err := img.GetMipmap(0); return err }, int64(img.Width * img.Height), nil
}

func preparePNG(data []byte) (func() error, int64, error) {
	rgba, pixels, err := calibrationImage(data)
	if err != nil {
		return nil, 0, err
	}
	return func() error { return png.Encode(io.Discard, rgba) }, pixels, nil
}

func prepareWebP(data []byte) (func() error, int64, error) {
	rgba, pixels, err := calibrationImage(data)
	if err != nil {
		return nil, 0, err
	}
	options := &nativewebp.Options{CompressionLevel: nativewebp.BestCompression}
	return func() error { return nativewebp.Encode(io.Discard, rgba, options) }, pixels, nil
}

func calibrationImage(data []byte) (image.Image, int64, error) {
	img, err := blp.Decode(data)
	if err != nil {
		return nil, 0, err
	}
	rgba, width, height, err := img.Image(0)
	if err != nil {
		return nil, 0, err
	}
	return rgba, int64(width * height), nil
}

func prepareEncryptionScan(data []byte) (func() error, int64, error) {
	run := func() error {
		zero := byte(0)
		for _, value := range data {
			zero |= value
		}
		calibrationSink = uint64(zero)
		return nil
	}
	return run, int64(len(data)), nil
}

func prepareCASCMetadata(data []byte) (func() error, int64, error) {
	run := func() error {
		entries := casc.ParseArchiveIndexEntries(data, "calibration")
		if len(entries) == 0 {
			return errors.New("archive index calibration returned no entries")
		}
		calibrationSink = uint64(len(entries))
		return nil
	}
	return run, int64(len(data)), nil
}

func prepareListfile(data []byte) (func() error, int64, error) {
	run := func() error {
		lf := listfile.New()
		return lf.Load(bytes.NewReader(data))
	}
	return run, int64(len(data)), nil
}

func prepareVideo(data []byte) (func() error, int64, error) {
	run := func() error {
		demuxer := video.NewVP9AVIDemuxer(data)
		if err := demuxer.ParseHeader(); err != nil {
			return err
		}
		_, err := demuxer.ExtractFrames()
		return err
	}
	return run, int64(len(data) * 2), nil
}

func prepareQueryPrimitive(kind string) func([]byte) (func() error, int64, error) {
	return func([]byte) (func() error, int64, error) {
		const rows = 4096
		values := make([]map[string]interface{}, rows)
		relationships := make(map[uint32][]uint32, rows)
		for i := range values {
			values[i] = map[string]interface{}{"ID": uint32(i), "Name": "spell", "Value": uint32(i * 3)}
			relationships[uint32(i)] = []uint32{uint32(i)}
		}
		switch kind {
		case "row-visit":
			return func() error {
				var sum uint64
				for _, row := range values {
					sum += uint64(row["ID"].(uint32))
				}
				calibrationSink = sum
				return nil
			}, rows, nil
		case "predicate":
			return func() error {
				var matches uint64
				for _, row := range values {
					if strings.Contains(row["Name"].(string), "ell") {
						matches++
					}
				}
				calibrationSink = matches
				return nil
			}, rows, nil
		case "project":
			return func() error {
				var fields uint64
				for _, row := range values {
					projected := map[string]interface{}{"ID": row["ID"], "Name": row["Name"]}
					fields += uint64(len(projected))
				}
				calibrationSink = fields
				return nil
			}, rows * 2, nil
		case "relationship":
			return func() error {
				var found uint64
				for i := 0; i < rows; i++ {
					found += uint64(len(relationships[uint32(i)]))
				}
				calibrationSink = found
				return nil
			}, rows, nil
		default:
			return nil, 0, fmt.Errorf("unknown query primitive %q", kind)
		}
	}
}

var calibrationSink uint64

func loadInput(path string, synthetic func() []byte) ([]byte, string, error) {
	if path == "" {
		return synthetic(), "synthetic", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(abs)
	return data, filepath.ToSlash(abs), err
}

func measure(name string, bytesPerIteration int64, minimum time.Duration, fn func() error) measurement {
	iterations := int64(0)
	started := time.Now()
	for time.Since(started) < minimum || iterations == 0 {
		if err := fn(); err != nil {
			panic(err)
		}
		iterations++
	}
	elapsed := time.Since(started)
	bytesDone := bytesPerIteration * iterations
	return measurement{Name: name, Bytes: bytesDone, Iterations: iterations, ElapsedNs: elapsed.Nanoseconds(), BytesPerSecond: rate(bytesDone, elapsed)}
}

func setWorkMeasurement(measurement *measurement, resourceClass, workUnit string) {
	measurement.ResourceClass = resourceClass
	measurement.WorkUnit = workUnit
	measurement.Units = measurement.Bytes
	measurement.UnitsPerSecond = measurement.BytesPerSecond
}

func rate(bytes int64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return 0
	}
	return float64(bytes) / elapsed.Seconds()
}

func percentile(sorted []int64, q float64) int64 {
	index := int(float64(len(sorted)-1)*q + 0.5)
	return sorted[index]
}

func makeJSONPayload(rows int) []map[string]interface{} {
	out := make([]map[string]interface{}, rows)
	for i := range out {
		out[i] = map[string]interface{}{"id": i, "name": fmt.Sprintf("spell-%d", i), "values": []int{i, i + 1, i + 2}, "active": i%2 == 0}
	}
	return out
}

func syntheticBLTE() []byte {
	payload := make([]byte, 4<<20)
	for i := range payload {
		payload[i] = byte(i*7 + 3)
	}
	data := make([]byte, 9+len(payload))
	binary.LittleEndian.PutUint32(data, 0x45544c42)
	data[8] = 'N'
	copy(data[9:], payload)
	return data
}

func syntheticZlibBLTE() []byte {
	payload := make([]byte, 4<<20)
	for i := range payload {
		payload[i] = byte(i*7 + 3)
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, _ = writer.Write(payload)
	_ = writer.Close()
	data := make([]byte, 9+compressed.Len())
	binary.LittleEndian.PutUint32(data, 0x45544c42)
	data[8] = 'Z'
	copy(data[9:], compressed.Bytes())
	return data
}

func syntheticNestedBLTE() []byte {
	inner := syntheticBLTE()
	data := make([]byte, 9+len(inner))
	binary.LittleEndian.PutUint32(data, 0x45544c42)
	data[8] = 'F'
	copy(data[9:], inner)
	return data
}

const (
	calibrationKeyName = "0123456789abcdef"
	calibrationKeyHex  = "00112233445566778899aabbccddeeff"
)

func syntheticEncryptedBLTE() []byte {
	payload := make([]byte, 4<<20)
	for i := range payload {
		payload[i] = byte(i*23 + 7)
	}
	key, _ := hex.DecodeString(calibrationKeyHex)
	nonce := []byte{1, 2, 3, 4, 0, 0, 0, 0}
	plain := append([]byte{'N'}, payload...)
	ciphertext := make([]byte, len(plain))
	salsa, _ := crypto.NewSalsa20(nonce, key, 20)
	salsa.Process(ciphertext, plain)
	keyName, _ := hex.DecodeString(calibrationKeyName)
	for left, right := 0, len(keyName)-1; left < right; left, right = left+1, right-1 {
		keyName[left], keyName[right] = keyName[right], keyName[left]
	}
	var encrypted bytes.Buffer
	encrypted.WriteByte(8)
	encrypted.Write(keyName)
	encrypted.WriteByte(4)
	encrypted.Write(nonce[:4])
	encrypted.WriteByte(0x53)
	encrypted.Write(ciphertext)
	data := make([]byte, 9+encrypted.Len())
	binary.LittleEndian.PutUint32(data, 0x45544c42)
	data[8] = 'E'
	copy(data[9:], encrypted.Bytes())
	return data
}

func syntheticScanData() []byte { return make([]byte, 4<<20) }

func syntheticArchiveIndex() []byte {
	const entries = 16384
	data := make([]byte, entries*24+12)
	for i := 0; i < entries; i++ {
		offset := i * 24
		binary.BigEndian.PutUint64(data[offset:], uint64(i+1))
		binary.BigEndian.PutUint64(data[offset+8:], uint64(i*17+3))
		binary.BigEndian.PutUint32(data[offset+16:], 4096)
		binary.BigEndian.PutUint32(data[offset+20:], uint32(i*4096))
	}
	binary.LittleEndian.PutUint32(data[len(data)-12:], entries)
	return data
}

func syntheticListfile() []byte {
	var builder strings.Builder
	for i := 1; i <= 65536; i++ {
		fmt.Fprintf(&builder, "%d;interface/icons/calibration_%d.blp\n", i, i)
	}
	return []byte(builder.String())
}

func syntheticAVI() []byte {
	var buffer bytes.Buffer
	buffer.WriteString("RIFF")
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(0))
	buffer.WriteString("AVI ")
	buffer.WriteString("LIST")
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(68))
	buffer.WriteString("hdrl")
	buffer.WriteString("avih")
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(56))
	header := make([]byte, 56)
	binary.LittleEndian.PutUint32(header, 33333)
	buffer.Write(header)
	buffer.WriteString("movi")
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(1032))
	buffer.WriteString("00db")
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(1024))
	buffer.Write(make([]byte, 1024))
	data := buffer.Bytes()
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	return data
}

func syntheticQueryData() []byte { return []byte{1} }

func decodeBLTE(data []byte) error {
	r, err := blte.NewReader(data)
	if err != nil {
		return err
	}
	_, err = r.ReadAll()
	return err
}

func syntheticDBC() []byte {
	const records = 65536
	data := make([]byte, 20+records*8+1)
	binary.LittleEndian.PutUint32(data, 0x43424457)
	binary.LittleEndian.PutUint32(data[4:], records)
	binary.LittleEndian.PutUint32(data[8:], 2)
	binary.LittleEndian.PutUint32(data[12:], 8)
	binary.LittleEndian.PutUint32(data[16:], 1)
	for i := 0; i < records; i++ {
		binary.LittleEndian.PutUint32(data[20+i*8:], uint32(i+1))
	}
	return data
}

func decodeDBC(data []byte) error {
	_, err := openDBC(data)
	return err
}

func openDBC(data []byte) (*db2.DBCReader, error) {
	if len(data) < 12 {
		return nil, errors.New("DBC data too short")
	}
	fieldCount := binary.LittleEndian.Uint32(data[8:])
	if fieldCount > 1<<16 {
		return nil, fmt.Errorf("unreasonable DBC field count %d", fieldCount)
	}
	schema := make([]db2.SchemaField, int(fieldCount))
	for i := range schema {
		schema[i] = db2.SchemaField{Name: fmt.Sprintf("Field%d", i), Type: db2.FieldUInt32}
	}
	return db2.NewDBCReaderFromBytes("calibration", "synthetic", data, schema)
}

func decodeWDC(data []byte) error {
	_, err := openWDC(data)
	return err
}

func openWDC(data []byte) (*db2.WDCReader, error) {
	fieldCountOffset := 8
	if len(data) >= 4 && binary.LittleEndian.Uint32(data) == 0x35434457 {
		fieldCountOffset = 140
	}
	if len(data) < fieldCountOffset+4 {
		return nil, errors.New("WDC data too short")
	}
	fieldCount := binary.LittleEndian.Uint32(data[fieldCountOffset:])
	if fieldCount > 1<<16 {
		return nil, fmt.Errorf("unreasonable WDC field count %d", fieldCount)
	}
	schema := make([]db2.SchemaField, fieldCount)
	for i := range schema {
		schema[i] = db2.SchemaField{Name: fmt.Sprintf("Field%d", i), Type: db2.FieldUInt32}
	}
	return db2.NewWDCReaderFromBytes("calibration", data, schema)
}

func syntheticDBD() []byte {
	var b strings.Builder
	b.WriteString("COLUMNS\n")
	for i := 0; i < 256; i++ {
		fmt.Fprintf(&b, "int Field%d\n", i)
	}
	b.WriteString("\nBUILD 11.0.0.60000\n")
	for i := 0; i < 256; i++ {
		fmt.Fprintf(&b, "Field%d<u32>\n", i)
	}
	return []byte(b.String())
}

func decodeDBD(data []byte) error { _, err := dbd.Parse(bytes.NewReader(data)); return err }

func syntheticBLP() []byte {
	const width, height = 256, 256
	const offset = 148 + 256*4
	data := make([]byte, offset+width*height*4)
	binary.LittleEndian.PutUint32(data, 0x32504c42)
	binary.LittleEndian.PutUint32(data[4:], 1)
	data[8], data[11] = 3, 1
	binary.LittleEndian.PutUint32(data[12:], width)
	binary.LittleEndian.PutUint32(data[16:], height)
	binary.LittleEndian.PutUint32(data[20:], offset)
	binary.LittleEndian.PutUint32(data[84:], width*height*4)
	for i := offset; i < len(data); i += 4 {
		data[i], data[i+1], data[i+2], data[i+3] = 0x33, 0x88, 0xcc, 0xff
	}
	return data
}

func decodeBLP(data []byte) error {
	img, err := blp.Decode(data)
	if err != nil {
		return err
	}
	_, _, _, err = img.Image(0)
	return err
}
