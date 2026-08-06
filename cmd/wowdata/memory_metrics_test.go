package main

import (
	"testing"
	"time"
)

func TestRuntimeMemorySamplerReportsMachineReadableMetrics(t *testing.T) {
	stop := startRuntimeMemorySampler(true)
	data := make([]byte, 2<<20)
	for index := range data {
		data[index] = byte(index)
	}
	time.Sleep(runtimeMemorySampleInterval + time.Millisecond)
	report := stop()
	if report.Schema != "wowdata.runtime-memory.v1" || report.PeakHeapBytes == 0 || report.Samples < 2 {
		t.Fatalf("runtime memory report = %#v", report)
	}
	if len(data) != 2<<20 {
		t.Fatal("allocation unexpectedly changed")
	}
}

func TestDisabledRuntimeMemorySamplerIsEmpty(t *testing.T) {
	if report := startRuntimeMemorySampler(false)(); report.Schema != "" {
		t.Fatalf("disabled report = %#v", report)
	}
}
