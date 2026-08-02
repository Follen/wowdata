package storage

import (
	"testing"
	"time"
)

func TestTargetLockSerializesWriters(t *testing.T) {
	layout, _ := Resolve(t.TempDir())
	first, err := layout.AcquireLock("target", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := layout.AcquireLock("target", 20*time.Millisecond); err == nil {
		t.Fatal("expected second lock to time out")
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := layout.AcquireLock("target", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = second.Release()
}
