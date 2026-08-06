package resource

import (
	"sync"
	"testing"
)

func TestWorkMetricsAccumulateConcurrentlyAndReset(t *testing.T) {
	ResetWorkMetrics()
	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			RecordBLTEBlock('Z', 10, 20)
			RecordWDCOpen(30, 4)
			RecordWDCDecode(3)
			RecordWDCQuery(2, 1, 3, 1)
			RecordDBDInput(40, 5)
			RecordDBDDefinition(6)
			RecordBLPOpen(50)
			RecordBLPDecode(64)
			RecordImageEncode("png", 64, 80)
		}()
	}
	wg.Wait()

	snapshot := SnapshotMetrics(nil)
	if snapshot.Schema != MetricsSchema || counterUnits(snapshot.Work, "blte-zlib-decode") != workers*20 {
		t.Fatalf("work snapshot = %+v", snapshot.Work)
	}
	if counterUnits(snapshot.Work, "wdc-row-decode") != workers*3 || counterUnits(snapshot.Work, "wdc-row-visit") != workers*2 || counterUnits(snapshot.Work, "dbd-parse") != workers*40 || counterUnits(snapshot.Work, "blp-decode") != workers*64 || counterUnits(snapshot.Work, "png-encode") != workers*64 {
		t.Fatalf("counter snapshot = %+v", snapshot.Work)
	}

	ResetWorkMetrics()
	if reset := SnapshotWorkMetrics(); len(reset.Counters) != 0 {
		t.Fatalf("reset snapshot = %+v", reset)
	}
}

func TestWorkCoverageRequiresRegisteredCalibratedClasses(t *testing.T) {
	ResetWorkMetrics()
	BeginWork(true, []string{"sha256", "json-encode"})
	if snapshot := SnapshotWorkMetrics(); !snapshot.Complete || len(snapshot.UncoveredClasses) != 0 {
		t.Fatalf("calibrated coverage = %+v", snapshot)
	}
	BeginWork(true, []string{"future-unmeasured-class"})
	if snapshot := SnapshotWorkMetrics(); snapshot.Complete || len(snapshot.UncoveredClasses) != 1 || snapshot.UncoveredClasses[0] != "future-unmeasured-class" {
		t.Fatalf("unmeasured coverage = %+v", snapshot)
	}
	BeginWork(false, nil)
	if snapshot := SnapshotWorkMetrics(); snapshot.Complete || len(snapshot.UncoveredClasses) != 1 || snapshot.UncoveredClasses[0] != "unregistered-command" {
		t.Fatalf("unregistered coverage = %+v", snapshot)
	}
}

func counterUnits(snapshot WorkSnapshot, class string) uint64 {
	for _, counter := range snapshot.Counters {
		if counter.Class == class {
			return counter.Units
		}
	}
	return 0
}
