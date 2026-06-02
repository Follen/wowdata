package http

import (
	"testing"

	appruntime "wowdata/internal/local/runtime"
)

func TestContextPoolKeepsPinnedDuringEviction(t *testing.T) {
	pool := NewContextPool(2)
	pool.Pin("pinned")
	pool.Put("pinned", &appruntime.Context{Product: "wow", BuildKey: "a"})
	pool.Put("normal-a", &appruntime.Context{Product: "wowt", BuildKey: "b"})
	pool.Put("normal-b", &appruntime.Context{Product: "wow_classic", BuildKey: "c"})
	if _, ok := pool.Get("pinned"); !ok {
		t.Fatal("pinned context was evicted")
	}
	if _, ok := pool.Get("normal-a"); ok {
		t.Fatal("least recently used normal context was not evicted")
	}
}

func TestContextPoolGetRefreshesLRU(t *testing.T) {
	pool := NewContextPool(2)
	pool.Put("normal-a", &appruntime.Context{Product: "wow", BuildKey: "a"})
	pool.Put("normal-b", &appruntime.Context{Product: "wowt", BuildKey: "b"})
	if _, ok := pool.Get("normal-a"); !ok {
		t.Fatal("normal-a was not found")
	}
	pool.Put("normal-c", &appruntime.Context{Product: "wow_classic", BuildKey: "c"})
	if _, ok := pool.Get("normal-a"); !ok {
		t.Fatal("touched context was evicted")
	}
	if _, ok := pool.Get("normal-b"); ok {
		t.Fatal("untouched least recently used context was not evicted")
	}
}

func TestNewContextPoolZeroMaxTreatsAsOne(t *testing.T) {
	pool := NewContextPool(0)
	pool.Put("normal-a", &appruntime.Context{Product: "wow", BuildKey: "a"})
	pool.Put("normal-b", &appruntime.Context{Product: "wowt", BuildKey: "b"})
	if _, ok := pool.Get("normal-a"); ok {
		t.Fatal("oldest context was not evicted")
	}
	if _, ok := pool.Get("normal-b"); !ok {
		t.Fatal("newest context was evicted")
	}
}
