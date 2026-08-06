package resource

import "testing"

func TestClassMemoryBudget(t *testing.T) {
	available := int64(32 * GiB)
	if got := classMemoryBudget(PointQuery, available); got != GiB {
		t.Fatalf("point budget = %d", got)
	}
	if got := classMemoryBudget(Composite, available); got != 2*GiB {
		t.Fatalf("composite budget = %d", got)
	}
	if got := classMemoryBudget(Corpus, available); got != 4*GiB {
		t.Fatalf("corpus budget = %d", got)
	}
}

func TestExplicitWorkersBoundEveryPool(t *testing.T) {
	plan := NewPlan(PointQuery, 3)
	for name, workers := range map[string]int{
		"metadata": plan.MetadataWorkers, "range": plan.LargeRangeWorkers,
		"db2": plan.DB2Workers, "blte": plan.BLTEWorkers, "image": plan.ImageWorkers,
	} {
		if workers < 1 || workers > 3 {
			t.Fatalf("%s workers = %d", name, workers)
		}
	}
}

func TestAutoNetworkPoolsAreIndependentFromTransportCapacity(t *testing.T) {
	plan := NewPlan(PointQuery, 0)
	wantMetadata := boundedWorkers(defaultMetadataWorkers(plan.GOMAXPROCS), plan.ConnectionBudget, plan.MemoryBudget, 256*1024)
	if plan.MetadataWorkers != wantMetadata {
		t.Fatalf("metadata workers = %d, want %d", plan.MetadataWorkers, wantMetadata)
	}
	wantLargeRange := boundedWorkers(defaultLargeRangeWorkers(plan.GOMAXPROCS), minInt(plan.ConnectionBudget, plan.GOMAXPROCS*2), plan.MemoryBudget, 8*MiB)
	if plan.LargeRangeWorkers != wantLargeRange {
		t.Fatalf("large-range workers = %d, want %d", plan.LargeRangeWorkers, wantLargeRange)
	}
	if plan.MetadataWorkers > plan.ConnectionBudget {
		t.Fatalf("metadata workers %d must not consume transport capacity %d", plan.MetadataWorkers, plan.ConnectionBudget)
	}
}

func TestAutoNetworkPoolTargetsScaleConservatively(t *testing.T) {
	tests := []struct {
		procs           int
		metadata, large int
	}{
		{procs: 1, metadata: 8, large: 1},
		{procs: 8, metadata: 12, large: 1},
		{procs: 16, metadata: 24, large: 2},
		{procs: 32, metadata: 36, large: 4},
		{procs: 128, metadata: 36, large: 4},
	}
	for _, test := range tests {
		if got := defaultMetadataWorkers(test.procs); got != test.metadata {
			t.Errorf("metadata target for %d procs = %d, want %d", test.procs, got, test.metadata)
		}
		if got := defaultLargeRangeWorkers(test.procs); got != test.large {
			t.Errorf("large-range target for %d procs = %d, want %d", test.procs, got, test.large)
		}
	}
}

func TestPoolOverridesChangeOnlyRequestedNetworkPool(t *testing.T) {
	baseline := NewPlan(PointQuery, 0)
	plan := NewPlanWithPoolOverrides(PointQuery, 0, 24, 7)
	if plan.MetadataWorkers != 24 || plan.LargeRangeWorkers != 7 {
		t.Fatalf("network pools = metadata %d range %d", plan.MetadataWorkers, plan.LargeRangeWorkers)
	}
	if plan.DB2Workers != baseline.DB2Workers || plan.BLTEWorkers != baseline.BLTEWorkers || plan.ImageWorkers != baseline.ImageWorkers {
		t.Fatalf("pool overrides changed non-network plan: %#v vs %#v", plan, baseline)
	}
}
