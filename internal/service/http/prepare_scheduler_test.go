package http

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrepareSchedulerLimitsConcurrency(t *testing.T) {
	s := NewPrepareScheduler(1)
	var running int32
	var maxRunning int32
	run := func(context.Context) error {
		now := atomic.AddInt32(&running, 1)
		if now > atomic.LoadInt32(&maxRunning) {
			atomic.StoreInt32(&maxRunning, now)
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return nil
	}
	done := make(chan error, 2)
	go func() { done <- s.Run(context.Background(), run) }()
	go func() { done <- s.Run(context.Background(), run) }()
	if err := <-done; err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("second run: %v", err)
	}
	if maxRunning != 1 {
		t.Fatalf("maxRunning = %d, want 1", maxRunning)
	}
}

func TestPrepareSchedulerCanceledContextWhileFullDoesNotRun(t *testing.T) {
	s := NewPrepareScheduler(1)
	release := make(chan struct{})
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.Run(context.Background(), func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var ran int32
	err := s.Run(ctx, func(context.Context) error {
		atomic.StoreInt32(&ran, 1)
		return nil
	})
	close(release)
	if firstErr := <-done; firstErr != nil {
		t.Fatalf("first run: %v", firstErr)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context canceled", err)
	}
	if ran != 0 {
		t.Fatal("fn ran despite canceled context and full scheduler")
	}
}

func TestPrepareSchedulerTreatsInvalidMaxAsOne(t *testing.T) {
	s := NewPrepareScheduler(0)
	var running int32
	var maxRunning int32
	var firstTask int32
	releaseFirst := make(chan struct{})
	firstStarted := make(chan struct{})
	run := func(context.Context) error {
		now := atomic.AddInt32(&running, 1)
		if now > atomic.LoadInt32(&maxRunning) {
			atomic.StoreInt32(&maxRunning, now)
		}
		if atomic.CompareAndSwapInt32(&firstTask, 0, 1) {
			close(firstStarted)
			<-releaseFirst
		}
		atomic.AddInt32(&running, -1)
		return nil
	}
	done := make(chan error, 2)
	go func() { done <- s.Run(context.Background(), run) }()
	<-firstStarted
	go func() { done <- s.Run(context.Background(), run) }()

	select {
	case err := <-done:
		t.Fatalf("a task finished before the first task released: %v", err)
	default:
	}
	close(releaseFirst)
	if err := <-done; err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("second run: %v", err)
	}
	if maxRunning != 1 {
		t.Fatalf("maxRunning = %d, want 1", maxRunning)
	}
}
