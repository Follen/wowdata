package casc

import (
	"testing"

	"wowdata/internal/resource"
)

func TestSameSchedulerPlanIgnoresRuntimeMemoryFluctuation(t *testing.T) {
	base := resource.Plan{
		Class: resource.Composite, GOMAXPROCS: 8, AvailableMemory: 20 << 30, MemoryBudget: 2 << 30,
		MetadataWorkers: 24, LargeRangeWorkers: 4, DB2Workers: 8, BLTEWorkers: 8,
		ImageWorkers: 4, ConnectionBudget: 128, FileHandleBudget: 128,
	}
	changedMemory := base
	changedMemory.AvailableMemory -= 512 << 20
	changedMemory.MemoryBudget -= 64 << 20
	if !sameSchedulerPlan(base, changedMemory) {
		t.Fatal("available-memory fluctuation would replace the shared scheduler")
	}
	changedWorkers := base
	changedWorkers.LargeRangeWorkers++
	if sameSchedulerPlan(base, changedWorkers) {
		t.Fatal("worker configuration change did not replace the scheduler")
	}
}
