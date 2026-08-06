package main

import (
	"runtime/metrics"
	"time"
)

const runtimeMemorySampleInterval = 10 * time.Millisecond

type runtimeMemoryReport struct {
	Schema         string `json:"schema"`
	PeakHeapBytes  uint64 `json:"peakHeapBytes"`
	AllocatedBytes uint64 `json:"allocatedBytes"`
	GCCollections  uint64 `json:"gcCollections"`
	Samples        uint64 `json:"samples"`
	IntervalNanos  int64  `json:"intervalNanos"`
}

type runtimeMemorySnapshot struct {
	heap, allocated, collections uint64
}

func startRuntimeMemorySampler(enabled bool) func() runtimeMemoryReport {
	if !enabled {
		return func() runtimeMemoryReport { return runtimeMemoryReport{} }
	}
	stop := make(chan struct{})
	done := make(chan runtimeMemoryReport, 1)
	go func() {
		first := readRuntimeMemorySnapshot()
		report := runtimeMemoryReport{
			Schema: "wowdata.runtime-memory.v1", PeakHeapBytes: first.heap,
			Samples: 1, IntervalNanos: int64(runtimeMemorySampleInterval),
		}
		ticker := time.NewTicker(runtimeMemorySampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				updateRuntimeMemoryReport(&report, first, readRuntimeMemorySnapshot())
			case <-stop:
				updateRuntimeMemoryReport(&report, first, readRuntimeMemorySnapshot())
				done <- report
				return
			}
		}
	}()
	return func() runtimeMemoryReport {
		close(stop)
		return <-done
	}
}

func updateRuntimeMemoryReport(report *runtimeMemoryReport, first, current runtimeMemorySnapshot) {
	if current.heap > report.PeakHeapBytes {
		report.PeakHeapBytes = current.heap
	}
	if current.allocated >= first.allocated {
		report.AllocatedBytes = current.allocated - first.allocated
	}
	if current.collections >= first.collections {
		report.GCCollections = current.collections - first.collections
	}
	report.Samples++
}

func readRuntimeMemorySnapshot() runtimeMemorySnapshot {
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/gc/heap/allocs:bytes"},
		{Name: "/gc/cycles/total:gc-cycles"},
	}
	metrics.Read(samples)
	return runtimeMemorySnapshot{
		heap: samples[0].Value.Uint64(), allocated: samples[1].Value.Uint64(),
		collections: samples[2].Value.Uint64(),
	}
}
