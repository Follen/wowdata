package http

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSingleflightRunsSameKeyOnce(t *testing.T) {
	group := NewSingleflight()
	var calls int32
	var wg sync.WaitGroup
	ready := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			value, err := group.Do("cn/wow/SpellName", func() (interface{}, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(20 * time.Millisecond)
				return "ok", nil
			})
			if err != nil || value.(string) != "ok" {
				t.Errorf("value=%#v err=%v", value, err)
			}
		}()
	}
	close(ready)
	wg.Wait()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestSingleflightErrorSharedAndNextCallRetries(t *testing.T) {
	group := NewSingleflight()
	wantErr := errors.New("prepare failed")
	var calls int32
	var wg sync.WaitGroup
	ready := make(chan struct{})
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			_, err := group.Do("cn/wow/ItemSparse", func() (interface{}, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(20 * time.Millisecond)
				return nil, wantErr
			})
			errs <- err
		}()
	}
	close(ready)
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want %v", err, wantErr)
		}
	}
	if calls != 1 {
		t.Fatalf("calls after shared error = %d, want 1", calls)
	}

	value, err := group.Do("cn/wow/ItemSparse", func() (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return "retried", nil
	})
	if err != nil {
		t.Fatalf("retry err = %v", err)
	}
	if value.(string) != "retried" {
		t.Fatalf("retry value = %#v, want retried", value)
	}
	if calls != 2 {
		t.Fatalf("calls after retry = %d, want 2", calls)
	}
}

func TestSingleflightReleasesWaitersBeforeDeletingInflight(t *testing.T) {
	group := NewSingleflight()
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	waiterReady := make(chan struct{})
	waiterDone := make(chan error, 1)
	lateStarted := make(chan struct{})
	lateDone := make(chan error, 1)
	var calls int32

	go func() {
		_, err := group.Do("cn/wow/SpellName", func() (interface{}, error) {
			atomic.AddInt32(&calls, 1)
			close(firstStarted)
			<-releaseFirst
			return "ok", nil
		})
		firstDone <- err
	}()
	<-firstStarted

	go func() {
		close(waiterReady)
		value, err := group.Do("cn/wow/SpellName", func() (interface{}, error) {
			atomic.AddInt32(&calls, 1)
			return "unexpected waiter run", nil
		})
		if err == nil && value.(string) != "ok" {
			err = errors.New("waiter received unexpected value")
		}
		waiterDone <- err
	}()
	<-waiterReady

	group.mu.Lock()
	close(releaseFirst)

	go func() {
		close(lateStarted)
		_, err := group.Do("cn/wow/SpellName", func() (interface{}, error) {
			atomic.AddInt32(&calls, 1)
			return "late", nil
		})
		lateDone <- err
	}()
	<-lateStarted

	select {
	case err := <-waiterDone:
		if err != nil {
			t.Fatalf("waiter err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter was not released before inflight deletion needed the mutex")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls before releasing delete lock = %d, want 1", got)
	}

	group.mu.Unlock()
	if err := <-firstDone; err != nil {
		t.Fatalf("first err = %v", err)
	}
	if err := <-lateDone; err != nil {
		t.Fatalf("late err = %v", err)
	}
}
