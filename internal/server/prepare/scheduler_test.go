package prepare

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundPrepareStartsAfterListenerReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var prepareCalls int32
	target := Target{Label: "us-retail", Region: "us", Product: "wow", Locale: "enUS"}
	scheduler := NewScheduler(
		[]Target{target},
		Limits{MaxParallelContextPrepares: 1, MaxParallelTableMaterializations: 1},
		func(context.Context, Target) error {
			atomic.AddInt32(&prepareCalls, 1)
			return nil
		},
		func(context.Context, Target, string) error { return nil },
	)

	ready := make(chan struct{})
	scheduler.StartAfterReady(ctx, ready)
	time.Sleep(25 * time.Millisecond)
	if got := atomic.LoadInt32(&prepareCalls); got != 0 {
		t.Fatalf("prepare calls before listener ready = %d, want 0", got)
	}

	close(ready)
	if err := scheduler.Wait(); err != nil {
		t.Fatalf("wait after ready: %v", err)
	}
	if got := atomic.LoadInt32(&prepareCalls); got != 1 {
		t.Fatalf("prepare calls after listener ready = %d, want 1", got)
	}
	if err := scheduler.CheckReadyForUserQuery(target); err != nil {
		t.Fatalf("ready target user query error: %v", err)
	}
}

func TestMaxParallelContextPreparesIsEnforced(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})

	var current int32
	var maxObserved int32
	targets := []Target{
		{Label: "target-1", Region: "us", Product: "wow", Locale: "enUS"},
		{Label: "target-2", Region: "eu", Product: "wow", Locale: "enUS"},
		{Label: "target-3", Region: "kr", Product: "wow", Locale: "koKR"},
		{Label: "target-4", Region: "tw", Product: "wow", Locale: "zhTW"},
	}
	limit := int32(2)

	scheduler := NewScheduler(
		targets,
		Limits{MaxParallelContextPrepares: int(limit), MaxParallelTableMaterializations: 1},
		func(ctx context.Context, _ Target) error {
			now := atomic.AddInt32(&current, 1)
			recordMax(&maxObserved, now)
			if now > limit {
				t.Fatalf("parallel context prepares = %d, want <= %d", now, limit)
			}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			atomic.AddInt32(&current, -1)
			return nil
		},
		func(context.Context, Target, string) error { return nil },
	)

	ready := make(chan struct{})
	scheduler.StartAfterReady(ctx, ready)
	close(ready)
	time.Sleep(25 * time.Millisecond)
	close(release)

	if err := scheduler.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if got := atomic.LoadInt32(&maxObserved); got > limit {
		t.Fatalf("max observed parallel context prepares = %d, want <= %d", got, limit)
	}
}

func TestMaxParallelTableMaterializationsIsEnforced(t *testing.T) {
	ctx := context.Background()
	release := make(chan struct{})

	var current int32
	var maxObserved int32
	targets := []Target{
		{Label: "target-1", Region: "us", Product: "wow", Locale: "enUS", Tables: []string{"Spell", "Item"}},
		{Label: "target-2", Region: "eu", Product: "wow", Locale: "enUS", Tables: []string{"Quest", "Creature"}},
	}
	limit := int32(2)

	scheduler := NewScheduler(
		targets,
		Limits{MaxParallelContextPrepares: 2, MaxParallelTableMaterializations: int(limit)},
		func(context.Context, Target) error { return nil },
		func(ctx context.Context, _ Target, _ string) error {
			now := atomic.AddInt32(&current, 1)
			recordMax(&maxObserved, now)
			if now > limit {
				t.Fatalf("parallel table materializations = %d, want <= %d", now, limit)
			}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			atomic.AddInt32(&current, -1)
			return nil
		},
	)

	ready := make(chan struct{})
	scheduler.StartAfterReady(ctx, ready)
	close(ready)
	time.Sleep(25 * time.Millisecond)
	close(release)

	if err := scheduler.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if got := atomic.LoadInt32(&maxObserved); got > limit {
		t.Fatalf("max observed parallel table materializations = %d, want <= %d", got, limit)
	}
}

func TestUserQueryReturnsContextNotReadyWithoutTriggeringPrepare(t *testing.T) {
	var prepareCalls int32
	target := Target{Label: "us-retail", Region: "us", Product: "wow", Locale: "enUS", Tables: []string{"Spell"}}
	scheduler := NewScheduler(
		[]Target{target},
		Limits{MaxParallelContextPrepares: 1, MaxParallelTableMaterializations: 1},
		func(context.Context, Target) error {
			atomic.AddInt32(&prepareCalls, 1)
			return nil
		},
		func(context.Context, Target, string) error { return nil },
	)

	err := scheduler.CheckReadyForUserQuery(target)
	if !errors.Is(err, ErrContextNotReady) {
		t.Fatalf("user query readiness error = %v, want %v", err, ErrContextNotReady)
	}
	if got := atomic.LoadInt32(&prepareCalls); got != 0 {
		t.Fatalf("prepare calls after user query = %d, want 0", got)
	}
}

func recordMax(maxObserved *int32, value int32) {
	for {
		currentMax := atomic.LoadInt32(maxObserved)
		if value <= currentMax || atomic.CompareAndSwapInt32(maxObserved, currentMax, value) {
			return
		}
	}
}
