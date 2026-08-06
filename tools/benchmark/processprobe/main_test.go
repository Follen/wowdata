package main

import (
	"reflect"
	"testing"
)

func TestSummarizePreservesRawOrderAndComputesSortedPercentiles(t *testing.T) {
	samples := []int64{90, 10, 50, 20, 70}
	got := summarize("wowdata.exe", "abc", samples)
	if !reflect.DeepEqual(got.Samples, samples) {
		t.Fatalf("samples changed: %v", got.Samples)
	}
	if got.MinNanos != 10 || got.P50Nanos != 50 || got.P95Nanos != 90 || got.MaxNanos != 90 {
		t.Fatalf("unexpected summary: %+v", got)
	}
}
