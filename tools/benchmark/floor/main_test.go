package main

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestCalculateAccountsForDAGAndAggregateContention(t *testing.T) {
	in := input{
		Command: "encounter export", Protocol: "independent-cold",
		Calibration: calibration{ProcessStartNanos: 10, NetworkBytesPerSecond: 100, DiskBytesPerSecond: 200, CPUWorkBytesPerSecond: 50, OutputBytesPerSecond: 100},
		Actual:      actual{P50Nanos: 2_500_000_010, P95Nanos: 3_000_000_010},
		Nodes: []node{
			{ID: "metadata", CriticalPathRTTNanos: 100, RequiredUniqueNetworkBytes: 100},
			{ID: "table-a", DependsOn: []string{"metadata"}, RequiredUniqueNetworkBytes: 100, RequiredCPUWorkBytes: 50},
			{ID: "table-b", DependsOn: []string{"metadata"}, RequiredUniqueNetworkBytes: 100, RequiredCPUWorkBytes: 50},
			{ID: "write", DependsOn: []string{"table-a", "table-b"}, UnavoidableOutputBytes: 50},
		},
	}
	got, err := calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	const wantFloor = int64(3_000_000_010)
	if got.FloorNanos != wantFloor {
		t.Fatalf("floor=%d want %d", got.FloorNanos, wantFloor)
	}
	if got.AggregateResourceFloor.Dominant != "network" {
		t.Fatalf("dominant=%q", got.AggregateResourceFloor.Dominant)
	}
	if !reflect.DeepEqual(got.CriticalPath, []string{"metadata", "table-a", "write"}) {
		t.Fatalf("critical path=%v", got.CriticalPath)
	}
	if got.Actual == nil || !got.Actual.P50Pass || !got.Actual.P95Pass {
		t.Fatalf("expected thresholds to pass: %+v", got.Actual)
	}
}

func TestCalculateAggregatesSameClassButOverlapsIndependentClasses(t *testing.T) {
	in := input{
		Command: "encounter export",
		Calibration: calibration{ResourceClasses: map[string]resourceClassCalibration{
			"cn-cdn": {Resource: "network", BytesPerSecond: 100},
			"github": {Resource: "network", BytesPerSecond: 50},
		}},
		Nodes: []node{
			{ID: "manifest", RequiredUniqueNetworkBytes: 100, NetworkResourceClass: "cn-cdn"},
			{ID: "root", RequiredUniqueNetworkBytes: 100, NetworkResourceClass: "cn-cdn"},
			{ID: "dbd", RequiredUniqueNetworkBytes: 150, NetworkResourceClass: "github"},
			{ID: "join", DependsOn: []string{"manifest", "root", "dbd"}},
		},
	}
	got, err := calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	// The two cn-cdn branches contend for 2s of class capacity. GitHub is an
	// independent 3s capacity; they overlap rather than forming a 5s sum.
	if got.FloorNanos != 3_000_000_000 {
		t.Fatalf("floor=%d want 3s", got.FloorNanos)
	}
	if got.DominantResourceClass != "github" {
		t.Fatalf("dominant class=%q", got.DominantResourceClass)
	}
	want := []classCapacityFloor{
		{Class: "cn-cdn", Resource: "network", WorkBytes: 200, BytesPerSecond: 100, Nanos: 2_000_000_000},
		{Class: "github", Resource: "network", WorkBytes: 150, BytesPerSecond: 50, Nanos: 3_000_000_000},
	}
	if !reflect.DeepEqual(got.ClassCapacityFloors, want) {
		t.Fatalf("class floors=%+v want %+v", got.ClassCapacityFloors, want)
	}
	if !reflect.DeepEqual(got.CriticalPath, []string{"dbd"}) {
		t.Fatalf("critical path=%v", got.CriticalPath)
	}
}

func TestCalculateSupportsNodeThroughputOverride(t *testing.T) {
	got, err := calculate(input{Command: "file get", Nodes: []node{{
		ID: "payload", RequiredUniqueNetworkBytes: 25, NetworkResourceClass: "origin", NetworkBytesPerSecond: 10,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.FloorNanos != 2_500_000_000 || got.DominantResourceClass != "origin" {
		t.Fatalf("unexpected report: %+v", got)
	}
}

func TestCalculateRejectsInvalidClassRatesAndTypes(t *testing.T) {
	tests := []struct {
		name string
		in   input
		want string
	}{
		{
			name: "invalid calibrated rate",
			in: input{Command: "x", Calibration: calibration{ResourceClasses: map[string]resourceClassCalibration{
				"cn-cdn": {Resource: "network", BytesPerSecond: math.NaN()},
			}}, Nodes: []node{{ID: "n"}}},
			want: "throughput must be positive",
		},
		{
			name: "wrong resource type",
			in: input{Command: "x", Calibration: calibration{ResourceClasses: map[string]resourceClassCalibration{
				"ssd": {Resource: "disk", BytesPerSecond: 10},
			}}, Nodes: []node{{ID: "n", RequiredUniqueNetworkBytes: 1, NetworkResourceClass: "ssd"}}},
			want: "not network",
		},
		{
			name: "conflicting node override",
			in: input{Command: "x", Calibration: calibration{ResourceClasses: map[string]resourceClassCalibration{
				"cn-cdn": {Resource: "network", BytesPerSecond: 10},
			}}, Nodes: []node{{ID: "n", RequiredUniqueNetworkBytes: 1, NetworkResourceClass: "cn-cdn", NetworkBytesPerSecond: 20}}},
			want: "conflicts with calibration",
		},
		{
			name: "inconsistent node overrides",
			in: input{Command: "x", Nodes: []node{
				{ID: "a", RequiredUniqueNetworkBytes: 1, NetworkResourceClass: "cn-cdn", NetworkBytesPerSecond: 10},
				{ID: "b", RequiredUniqueNetworkBytes: 1, NetworkResourceClass: "cn-cdn", NetworkBytesPerSecond: 20},
			}},
			want: "inconsistent throughput",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := calculate(test.in)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
		})
	}
}

func TestV1JSONRemainsCompatible(t *testing.T) {
	var in input
	err := json.Unmarshal([]byte(`{
		"command":"doctor",
		"calibration":{"networkBytesPerSecond":100},
		"nodes":[{"id":"fetch","requiredUniqueNetworkBytes":100}]
	}`), &in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.FloorNanos != 1_000_000_000 || got.DominantResourceClass != "network" {
		t.Fatalf("unexpected v1 report: %+v", got)
	}
}

func TestCalculateRejectsCycle(t *testing.T) {
	_, err := calculate(input{Command: "db2 stream", Calibration: calibration{NetworkBytesPerSecond: 1, DiskBytesPerSecond: 1, CPUWorkBytesPerSecond: 1, OutputBytesPerSecond: 1}, Nodes: []node{{ID: "a", DependsOn: []string{"b"}}, {ID: "b", DependsOn: []string{"a"}}}})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestCalculateRequiresRateOnlyForUsedResource(t *testing.T) {
	got, err := calculate(input{Command: "doctor", Nodes: []node{{ID: "inspect", FixedNanos: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.FloorNanos != 5 {
		t.Fatalf("unexpected report: %+v", got)
	}
	_, err = calculate(input{Command: "file get", Nodes: []node{{ID: "download", RequiredUniqueNetworkBytes: 1}}})
	if err == nil || !strings.Contains(err.Error(), "network throughput") {
		t.Fatalf("expected missing rate error, got %v", err)
	}
}
