package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

func TestAcquireLockReclaimsDeadOwner(t *testing.T) {
	layout, _ := Resolve(t.TempDir())
	if err := os.MkdirAll(layout.Locks, 0755); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("dead-owner"))
	path := filepath.Join(layout.Locks, hex.EncodeToString(hash[:16])+".lock")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("pid=%d\ncreated=2000-01-01T00:00:00Z\n", 0x3fffffff)), 0644); err != nil {
		t.Fatal(err)
	}
	lock, err := layout.AcquireLock("dead-owner", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}
