package storage

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyCacheDetectsCorruption(t *testing.T) {
	layout, _ := Resolve(t.TempDir())
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(layout.Cache, "casc", "build")
	dataPath := filepath.Join(root, "data.bin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("good")
	hash := sha256.Sum256(body)
	if err := os.WriteFile(dataPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteJSON(filepath.Join(root, "cache_integrity.json"), map[string]string{"data.bin": fmt.Sprintf("%x", hash)}, 0o644); err != nil {
		t.Fatal(err)
	}
	if result := layout.VerifyCache(); !result.OK || result.Files != 1 {
		t.Fatalf("valid cache rejected: %#v", result)
	}
	if err := os.WriteFile(dataPath, []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := layout.VerifyCache(); result.OK || len(result.Corrupt) != 1 {
		t.Fatalf("corruption not detected: %#v", result)
	}
}

func TestPruneRemovesOnlyUnreferencedContentObjects(t *testing.T) {
	layout, _ := Resolve(t.TempDir())
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	objects := filepath.Join(layout.Cache, "casc", "objects")
	shared := filepath.Join(objects, "aa", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	orphan := filepath.Join(objects, "bb", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	for _, path := range []string{shared, orphan} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("payload"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".sha256", []byte("hash"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	refs := map[string]string{"build-a": shared, "build-b": orphan}
	for build, object := range refs {
		root := filepath.Join(layout.Cache, "casc", "builds", build)
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, object)
		if err != nil {
			t.Fatal(err)
		}
		if err := AtomicWriteJSON(filepath.Join(root, "cache_integrity.json"), map[string]string{filepath.ToSlash(rel): "hash"}, 0644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := layout.PruneCacheKeeping(1, []string{"build-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "build-b" {
		t.Fatalf("removed = %#v", result.Removed)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatalf("shared object removed: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan object still exists: %v", err)
	}
	if _, err := os.Stat(orphan + ".sha256"); !os.IsNotExist(err) {
		t.Fatalf("orphan sidecar still exists: %v", err)
	}
}

func TestPruneCleansExpiredResumeAndOrphanSidecarBelowQuota(t *testing.T) {
	layout, _ := Resolve(t.TempDir())
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	resumeRoot := filepath.Join(layout.Cache, "casc", "resume")
	if err := os.MkdirAll(resumeRoot, 0755); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(resumeRoot, "stale.part")
	state := filepath.Join(resumeRoot, "stale.json")
	for _, path := range []string{part, state} {
		if err := os.WriteFile(path, []byte("stale"), 0644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-DefaultResumeStateTTL - time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	orphanSidecar := filepath.Join(layout.Cache, "casc", "objects", "aa", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.sha256")
	if err := os.MkdirAll(filepath.Dir(orphanSidecar), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphanSidecar, []byte("hash"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := layout.PruneCacheKeeping(DefaultCacheMaxBytes, nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{part, state, orphanSidecar} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale managed file still exists: %s (%v)", path, err)
		}
	}
}
