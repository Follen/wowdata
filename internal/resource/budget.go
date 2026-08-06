package resource

import "runtime"

const (
	MiB = int64(1024 * 1024)
	GiB = int64(1024 * MiB)
)

type Class uint8

const (
	PointQuery Class = iota
	Composite
	Corpus
)

type Plan struct {
	Class               Class `json:"-"`
	GOMAXPROCS          int   `json:"gomaxprocs"`
	AvailableMemory     int64 `json:"availableMemoryBytes"`
	MemoryBudget        int64 `json:"memoryBudgetBytes"`
	MetadataWorkers     int   `json:"metadataWorkers"`
	LargeRangeWorkers   int   `json:"largeRangeWorkers"`
	DB2Workers          int   `json:"db2Workers"`
	BLTEWorkers         int   `json:"blteWorkers"`
	ImageWorkers        int   `json:"imageWorkers"`
	ConnectionBudget    int   `json:"connectionBudget"`
	FileHandleBudget    int   `json:"fileHandleBudget"`
	ExplicitWorkerLimit int   `json:"explicitWorkerLimit,omitempty"`
	MetadataOverride    int   `json:"metadataOverride,omitempty"`
	LargeRangeOverride  int   `json:"largeRangeOverride,omitempty"`
}

func NewPlan(class Class, explicitWorkers int) Plan {
	return NewPlanWithPoolOverrides(class, explicitWorkers, 0, 0)
}

func NewPlanWithPoolOverrides(class Class, explicitWorkers, metadataWorkers, largeRangeWorkers int) Plan {
	procs := runtime.GOMAXPROCS(0)
	if procs < 1 {
		procs = 1
	}
	available := availableMemoryBytes()
	if available <= 0 {
		available = 8 * GiB
	}
	memoryBudget := classMemoryBudget(class, available)
	connectionBudget := minInt(256, maxInt(32, procs*16))
	fileHandleBudget := minInt(512, maxInt(64, procs*16))
	plan := Plan{
		Class: class, GOMAXPROCS: procs, AvailableMemory: available,
		MemoryBudget: memoryBudget, ConnectionBudget: connectionBudget,
		FileHandleBudget: fileHandleBudget, ExplicitWorkerLimit: explicitWorkers,
	}
	if explicitWorkers > 0 {
		plan.MetadataWorkers = boundedWorkers(explicitWorkers, connectionBudget, memoryBudget, 256*1024)
		plan.LargeRangeWorkers = boundedWorkers(explicitWorkers, procs*2, memoryBudget, 2*MiB)
		plan.DB2Workers = boundedWorkers(explicitWorkers, procs, memoryBudget, 64*MiB)
		plan.BLTEWorkers = boundedWorkers(explicitWorkers, procs, memoryBudget, 8*MiB)
		plan.ImageWorkers = boundedWorkers(explicitWorkers, procs, memoryBudget, 32*MiB)
	} else {
		// ConnectionBudget is transport capacity, not a concurrency target. Keep
		// metadata and payload ranges independently tunable because their request
		// sizes and CDN saturation points differ substantially.
		plan.MetadataWorkers = boundedWorkers(defaultMetadataWorkers(procs), connectionBudget, memoryBudget, 256*1024)
		plan.LargeRangeWorkers = boundedWorkers(defaultLargeRangeWorkers(procs), minInt(connectionBudget, procs*2), memoryBudget, 8*MiB)
		plan.DB2Workers = boundedWorkers(procs, procs, memoryBudget, 64*MiB)
		plan.BLTEWorkers = boundedWorkers(procs, procs, memoryBudget, 8*MiB)
		plan.ImageWorkers = boundedWorkers(maxInt(1, procs/2), procs, memoryBudget, 32*MiB)
	}
	if metadataWorkers > 0 {
		plan.MetadataWorkers = boundedWorkers(metadataWorkers, connectionBudget, memoryBudget, 256*1024)
		plan.MetadataOverride = metadataWorkers
	}
	if largeRangeWorkers > 0 {
		plan.LargeRangeWorkers = boundedWorkers(largeRangeWorkers, minInt(connectionBudget, procs*2), memoryBudget, 8*MiB)
		plan.LargeRangeOverride = largeRangeWorkers
	}
	return plan
}

func defaultMetadataWorkers(procs int) int {
	// Metadata requests are latency-bound, but the CN CDN tournament shows a
	// clear tail-latency penalty above roughly 1.5 workers per logical CPU.
	return minInt(36, maxInt(8, procs+procs/2))
}

func defaultLargeRangeWorkers(procs int) int {
	// Large ranges consume materially more memory and bandwidth per task. Scale
	// slowly with the machine and leave the scheduler's global budgets in force.
	return minInt(4, maxInt(1, procs/8))
}

func classMemoryBudget(class Class, available int64) int64 {
	percent, floor, ceiling := int64(5), int64(384*MiB), int64(GiB)
	switch class {
	case Composite:
		percent, floor, ceiling = 8, 768*MiB, 2*GiB
	case Corpus:
		percent, floor, ceiling = 15, GiB, 4*GiB
	}
	budget := available * percent / 100
	if budget < floor {
		budget = floor
	}
	if budget > ceiling {
		budget = ceiling
	}
	return budget
}

func boundedWorkers(want, hardLimit int, memoryBudget, bytesPerTask int64) int {
	if hardLimit > 0 && want > hardLimit {
		want = hardLimit
	}
	if bytesPerTask > 0 {
		memoryLimit := int(memoryBudget / bytesPerTask)
		if memoryLimit < want {
			want = memoryLimit
		}
	}
	if want < 1 {
		return 1
	}
	return want
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
