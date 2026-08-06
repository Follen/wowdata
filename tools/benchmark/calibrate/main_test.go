package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"wowdata/internal/resource"
)

func TestDecodeWDC5UsesVersionedFieldCountOffset(t *testing.T) {
	data := make([]byte, 204)
	binary.LittleEndian.PutUint32(data, 0x35434457)
	binary.LittleEndian.PutUint32(data[4:], 5)
	copy(data[8:136], []byte("WOWSTATIC_1_15_8_63631"))
	// Empty WDC5: record count, field count, and section count remain zero.
	if err := decodeWDC(data); err != nil {
		t.Fatalf("decode empty WDC5: %v", err)
	}
}

func TestCalibrateProducesCompleteReportAndRemovesTempFile(t *testing.T) {
	dir := t.TempDir()
	r, err := calibrate(config{TempDir: dir, FileMiB: 1, Duration: time.Millisecond, RandomReads: 8})
	if err != nil {
		t.Fatal(err)
	}
	if r.SchemaVersion != schemaVersion || r.Machine.LogicalCores < 1 {
		t.Fatalf("invalid report header: %+v", r)
	}
	if r.Disk.SequentialWriteBps <= 0 || r.Disk.SequentialReadBps <= 0 || r.Disk.RandomReadSamples != 8 || r.Disk.RandomReadOperations != 8*64 {
		t.Fatalf("invalid disk report: %+v", r.Disk)
	}
	if len(r.Compute) != 3 || len(r.Storage) != 2 || len(r.Codecs) != 24 {
		t.Fatalf("compute/storage/codecs = %d/%d/%d", len(r.Compute), len(r.Storage), len(r.Codecs))
	}
	if r.Limits.OpenFiles == 0 || r.Limits.Connections == 0 {
		t.Fatalf("missing scheduler limits: %+v", r.Limits)
	}
	for _, m := range append([]codecMeasurement(nil), r.Codecs...) {
		if m.BytesPerSecond <= 0 || m.UnitsPerSecond <= 0 || m.ResourceClass == "" || m.WorkUnit == "" || m.Source != "synthetic" {
			t.Fatalf("invalid codec result: %+v", m)
		}
	}
	for _, measurement := range r.Storage {
		if measurement.ResourceClass == "" || measurement.WorkUnit != "bytes" || measurement.UnitsPerSecond <= 0 {
			t.Fatalf("invalid storage calibration: %+v", measurement)
		}
	}
	available := make(map[string]string)
	for _, measurement := range r.Compute {
		available[measurement.ResourceClass] = measurement.WorkUnit
	}
	for _, measurement := range r.Storage {
		available[measurement.ResourceClass] = measurement.WorkUnit
	}
	for _, measurement := range r.Codecs {
		available[measurement.ResourceClass] = measurement.WorkUnit
	}
	for _, class := range resource.CalibratedWorkClasses() {
		if available[class] == "" {
			t.Errorf("production class %q has no independent calibration", class)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary calibration payload was retained: %v", entries)
	}
}

func TestCalibrateRejectsUnboundedOrInvalidSettings(t *testing.T) {
	for _, cfg := range []config{{FileMiB: 0, Duration: time.Millisecond, RandomReads: 1}, {FileMiB: 1025, Duration: time.Millisecond, RandomReads: 1}, {FileMiB: 1, RandomReads: 1}, {FileMiB: 1, Duration: time.Millisecond}} {
		if _, err := calibrate(cfg); err == nil {
			t.Fatalf("expected error for %+v", cfg)
		}
	}
}

func TestRealCodecInputRecordsAbsoluteSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.dbd")
	if err := os.WriteFile(path, syntheticDBD(), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := calibrateCodecs(time.Millisecond, codecInputs{DBD: path})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Name == "dbd-parse" && result.Source == "synthetic" {
			t.Fatalf("real input source was not recorded: %+v", result)
		}
	}
}

func TestPercentileUsesDeterministicNearestRank(t *testing.T) {
	values := []int64{1, 2, 3, 4, 5}
	if got := percentile(values, .5); got != 3 {
		t.Fatalf("p50=%d", got)
	}
	if got := percentile(values, .95); got != 5 {
		t.Fatalf("p95=%d", got)
	}
}
