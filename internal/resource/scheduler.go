package resource

import (
	"context"
	"sync"
	"sync/atomic"
)

type Pool uint8

const (
	MetadataPool Pool = iota
	LargeRangePool
	DB2ParsePool
	BLTEDecodePool
	ImageDecodeWritePool
	poolCount
)

// Scheduler applies per-workload limits before a shared process-level network
// budget. A single Scheduler is shared by every transfer owned by one runtime.
type Scheduler struct {
	network chan struct{}
	compute chan struct{}
	pools   [poolCount]chan struct{}
	memory  *weightedBudget
	active  [poolCount]atomic.Int64
	peak    [poolCount]atomic.Int64
}

func NewScheduler(plan Plan) *Scheduler {
	networkWorkers := plan.MetadataWorkers + plan.LargeRangeWorkers
	if networkWorkers > plan.ConnectionBudget {
		networkWorkers = plan.ConnectionBudget
	}
	if networkWorkers < 1 {
		networkWorkers = 1
	}
	computeWorkers := plan.GOMAXPROCS
	if computeWorkers < 1 {
		computeWorkers = 1
	}
	s := &Scheduler{
		network: make(chan struct{}, networkWorkers),
		compute: make(chan struct{}, computeWorkers),
		memory:  newWeightedBudget(plan.MemoryBudget),
	}
	s.pools[MetadataPool] = make(chan struct{}, maxInt(1, plan.MetadataWorkers))
	s.pools[LargeRangePool] = make(chan struct{}, maxInt(1, plan.LargeRangeWorkers))
	s.pools[DB2ParsePool] = make(chan struct{}, maxInt(1, plan.DB2Workers))
	s.pools[BLTEDecodePool] = make(chan struct{}, maxInt(1, plan.BLTEWorkers))
	s.pools[ImageDecodeWritePool] = make(chan struct{}, maxInt(1, plan.ImageWorkers))
	return s
}

func (s *Scheduler) Acquire(ctx context.Context, pool Pool) (func(), error) {
	return s.AcquireBytes(ctx, pool, defaultTaskBytes(pool))
}

func (s *Scheduler) AcquireBytes(ctx context.Context, pool Pool, bytes int64) (func(), error) {
	if s == nil {
		return func() {}, nil
	}
	if pool >= poolCount {
		return nil, context.Canceled
	}
	permits := s.pools[pool]
	select {
	case permits <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := s.memory.acquire(ctx, bytes); err != nil {
		<-permits
		return nil, err
	}
	global := s.compute
	if pool == MetadataPool || pool == LargeRangePool {
		global = s.network
	}
	select {
	case global <- struct{}{}:
	case <-ctx.Done():
		s.memory.release(bytes)
		<-permits
		return nil, ctx.Err()
	}
	active := s.active[pool].Add(1)
	updatePeak(&s.peak[pool], active)
	var once sync.Once
	return func() {
		once.Do(func() {
			s.active[pool].Add(-1)
			<-global
			s.memory.release(bytes)
			<-permits
		})
	}, nil
}

func (s *Scheduler) Capacity(pool Pool) int {
	if s == nil || pool >= poolCount {
		return 0
	}
	return cap(s.pools[pool])
}

func (s *Scheduler) TotalCapacity() int {
	if s == nil {
		return 0
	}
	return cap(s.network)
}

func (s *Scheduler) ComputeCapacity() int {
	if s == nil {
		return 0
	}
	return cap(s.compute)
}

type SchedulerSnapshot struct {
	MemoryBudgetBytes int64            `json:"memoryBudgetBytes"`
	PeakReservedBytes int64            `json:"peakReservedBytes"`
	ConnectionBudget  int              `json:"connectionBudget"`
	ComputeCapacity   int              `json:"computeCapacity"`
	PoolCapacity      map[string]int   `json:"poolCapacity"`
	PoolPeakActive    map[string]int64 `json:"poolPeakActive"`
}

func (s *Scheduler) Snapshot() SchedulerSnapshot {
	if s == nil {
		return SchedulerSnapshot{}
	}
	result := SchedulerSnapshot{
		MemoryBudgetBytes: s.memory.limit,
		PeakReservedBytes: s.memory.peak.Load(),
		ConnectionBudget:  cap(s.network),
		ComputeCapacity:   cap(s.compute),
		PoolCapacity:      make(map[string]int, int(poolCount)),
		PoolPeakActive:    make(map[string]int64, int(poolCount)),
	}
	for pool := Pool(0); pool < poolCount; pool++ {
		result.PoolCapacity[pool.String()] = cap(s.pools[pool])
		result.PoolPeakActive[pool.String()] = s.peak[pool].Load()
	}
	return result
}

func (p Pool) String() string {
	switch p {
	case MetadataPool:
		return "metadata"
	case LargeRangePool:
		return "large-range"
	case DB2ParsePool:
		return "db2-parse"
	case BLTEDecodePool:
		return "blte-decode"
	case ImageDecodeWritePool:
		return "image-decode-write"
	default:
		return "unknown"
	}
}

func defaultTaskBytes(pool Pool) int64 {
	switch pool {
	case MetadataPool:
		return 256 * 1024
	case LargeRangePool, BLTEDecodePool:
		return 8 * MiB
	case DB2ParsePool:
		return 64 * MiB
	case ImageDecodeWritePool:
		return 32 * MiB
	default:
		return MiB
	}
}

type weightedBudget struct {
	mu     sync.Mutex
	limit  int64
	used   int64
	notify chan struct{}
	peak   atomic.Int64
}

func newWeightedBudget(limit int64) *weightedBudget {
	if limit < 1 {
		limit = GiB
	}
	return &weightedBudget{limit: limit, notify: make(chan struct{})}
}

func (b *weightedBudget) acquire(ctx context.Context, amount int64) error {
	if amount < 1 {
		amount = 1
	}
	if amount > b.limit {
		amount = b.limit
	}
	for {
		b.mu.Lock()
		if b.limit-b.used >= amount {
			b.used += amount
			used := b.used
			b.mu.Unlock()
			updatePeak(&b.peak, used)
			return nil
		}
		notify := b.notify
		b.mu.Unlock()
		select {
		case <-notify:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (b *weightedBudget) release(amount int64) {
	if amount < 1 {
		amount = 1
	}
	if amount > b.limit {
		amount = b.limit
	}
	b.mu.Lock()
	b.used -= amount
	if b.used < 0 {
		b.mu.Unlock()
		panic("resource: released more memory budget than acquired")
	}
	close(b.notify)
	b.notify = make(chan struct{})
	b.mu.Unlock()
}

func updatePeak(peak *atomic.Int64, value int64) {
	for current := peak.Load(); value > current; current = peak.Load() {
		if peak.CompareAndSwap(current, value) {
			return
		}
	}
}
