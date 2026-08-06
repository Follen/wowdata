package resource

import (
	"context"
	"testing"
	"time"
)

func TestSchedulerSharesLargeRangeCapacityAcrossCallers(t *testing.T) {
	plan := Plan{MetadataWorkers: 36, LargeRangeWorkers: 4, ConnectionBudget: 256}
	scheduler := NewScheduler(plan)
	releases := make([]func(), 0, plan.LargeRangeWorkers)
	for i := 0; i < plan.LargeRangeWorkers; i++ {
		release, err := scheduler.Acquire(context.Background(), LargeRangePool)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := scheduler.Acquire(ctx, LargeRangePool); err == nil {
		t.Fatal("fifth large-range request bypassed the shared four-request limit")
	}
	for _, release := range releases {
		release()
	}
}

func TestSchedulerKeepsTransportCapacitySeparate(t *testing.T) {
	plan := Plan{GOMAXPROCS: 8, MemoryBudget: GiB, MetadataWorkers: 36, LargeRangeWorkers: 4, DB2Workers: 8, BLTEWorkers: 8, ImageWorkers: 4, ConnectionBudget: 256}
	scheduler := NewScheduler(plan)
	if got := scheduler.TotalCapacity(); got != 40 {
		t.Fatalf("logical network capacity = %d, want 40", got)
	}
	if got := scheduler.Capacity(MetadataPool); got != 36 {
		t.Fatalf("metadata capacity = %d", got)
	}
	if got := scheduler.ComputeCapacity(); got != 8 {
		t.Fatalf("compute capacity = %d", got)
	}
}

func TestSchedulerSharesComputeAndMemoryAcrossPools(t *testing.T) {
	plan := Plan{GOMAXPROCS: 2, MemoryBudget: 64 * MiB, MetadataWorkers: 1, LargeRangeWorkers: 1, DB2Workers: 2, BLTEWorkers: 2, ImageWorkers: 2, ConnectionBudget: 2}
	scheduler := NewScheduler(plan)
	releaseDB2, err := scheduler.Acquire(context.Background(), DB2ParsePool)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := scheduler.Acquire(ctx, ImageDecodeWritePool); err == nil {
		t.Fatal("image task bypassed memory held by DB2 parse task")
	}
	releaseDB2()
	releaseImage, err := scheduler.Acquire(context.Background(), ImageDecodeWritePool)
	if err != nil {
		t.Fatal(err)
	}
	releaseBLTE, err := scheduler.Acquire(context.Background(), BLTEDecodePool)
	if err != nil {
		t.Fatal(err)
	}
	blocked, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if _, err := scheduler.Acquire(blocked, ImageDecodeWritePool); err == nil {
		t.Fatal("third compute task bypassed shared GOMAXPROCS capacity")
	}
	releaseImage()
	releaseBLTE()
	snapshot := scheduler.Snapshot()
	if snapshot.PeakReservedBytes != 64*MiB {
		t.Fatalf("peak reserved bytes = %d", snapshot.PeakReservedBytes)
	}
	if snapshot.PoolPeakActive["image-decode-write"] != 1 || snapshot.PoolPeakActive["blte-decode"] != 1 {
		t.Fatalf("unexpected peaks: %+v", snapshot.PoolPeakActive)
	}
}
