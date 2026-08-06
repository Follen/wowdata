package casc

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDataCacheRoot(t *testing.T) {
	dc := NewDataCache(filepath.Join(t.TempDir(), "cascroot"), "0123456789abcdef0123456789abcdef")
	if dc.Root() == "" {
		t.Fatal("empty root")
	}
	if !strings.HasSuffix(dc.Root(), "0123456789abcdef0123456789abcdef") {
		t.Fatalf("root = %s", dc.Root())
	}
}

func TestDataCacheManifestPath(t *testing.T) {
	dc := NewDataCache("/tmp/wow", "abc123abc123abc123abc123abc123ab")
	mp := dc.ManifestPath()
	if !strings.HasSuffix(mp, "build_manifest.json") {
		t.Fatalf("manifest path = %s", mp)
	}
}

func TestDataCacheFilePath(t *testing.T) {
	dc := NewDataCache("/tmp/wow", "key123")
	fp := dc.FilePath("file.dat", "subdir")
	if !strings.HasSuffix(fp, "file.dat") {
		t.Fatalf("file path = %s", fp)
	}
}

func TestDataCacheStoreAndGet(t *testing.T) {
	dir := t.TempDir()
	dc := NewDataCache(dir, "testbuildkey")

	data := []byte("cached file content!")
	if err := dc.StoreFile("test.bin", data, ""); err != nil {
		t.Fatalf("StoreFile: %v", err)
	}

	got, err := dc.GetFile("test.bin", "")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestDataCacheSharesContentObjectsAcrossBuilds(t *testing.T) {
	dir := t.TempDir()
	first := NewDataCache(dir, "build-a")
	second := NewDataCache(dir, "build-b")
	key := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body := []byte("shared BLTE payload")
	if err := first.StoreObject(key, body); err != nil {
		t.Fatal(err)
	}
	if first.ObjectPath(key) != second.ObjectPath(key) {
		t.Fatalf("object paths differ: %s %s", first.ObjectPath(key), second.ObjectPath(key))
	}
	if first.ResumeRoot() != second.ResumeRoot() {
		t.Fatalf("resume roots differ: %s %s", first.ResumeRoot(), second.ResumeRoot())
	}
	got, err := second.GetObject(key)
	if err != nil || string(got) != string(body) {
		t.Fatalf("shared object: data=%q err=%v", got, err)
	}
	if _, err := os.Stat(first.ObjectPath(key) + ".sha256"); err != nil {
		t.Fatalf("missing object integrity sidecar: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(first.ObjectPath(key)))
	if err != nil || len(entries) != 2 {
		t.Fatalf("object directory entries=%d err=%v", len(entries), err)
	}
	if _, err := os.Stat(filepath.Join(first.Root(), "data", key)); !os.IsNotExist(err) {
		t.Fatalf("Build view contains copied payload: %v", err)
	}
	if _, err := os.Stat(filepath.Join(first.Root(), "cache_integrity.json")); !os.IsNotExist(err) {
		t.Fatalf("content object was persisted into Build integrity map: %v", err)
	}
}

func TestRemoveObjectsPrefixDeletesOnlyMatchingPayloadsAndSidecars(t *testing.T) {
	cache := NewDataCache(t.TempDir(), "build")
	for name, data := range map[string][]byte{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.blte.range-0-12":  []byte("header"),
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.blte.range-12-24": []byte("block"),
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":                  []byte("full"),
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.blte.range-0-12":  []byte("other"),
	} {
		if err := cache.StoreObject(name, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.RemoveObjectsPrefix("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.blte.range-"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.GetObject("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.blte.range-0-12"); err == nil {
		t.Fatal("matching range payload remains")
	}
	for _, name := range []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.blte.range-0-12"} {
		if _, err := cache.GetObject(name); err != nil {
			t.Fatalf("unrelated object %s was removed: %v", name, err)
		}
	}
}

func TestDataCacheMergesConcurrentBuildReferences(t *testing.T) {
	dir := t.TempDir()
	first := NewDataCache(dir, "same-build")
	second := NewDataCache(dir, "same-build")
	keys := []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	var wg sync.WaitGroup
	errs := make(chan error, len(keys))
	for index, cache := range []*DataCache{first, second} {
		wg.Add(1)
		go func(cache *DataCache, key string) {
			defer wg.Done()
			errs <- cache.StoreObject(key, []byte(key))
		}(cache, keys[index])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	reloaded := NewDataCache(dir, "same-build")
	for _, key := range keys {
		data, err := reloaded.GetObject(key)
		if err != nil || string(data) != key {
			t.Fatalf("object %s data=%q err=%v", key, data, err)
		}
	}
}

func TestDataCacheGetMissingFile(t *testing.T) {
	dir := t.TempDir()
	dc := NewDataCache(dir, "testbuildkey")

	_, err := dc.GetFile("nonexistent.bin", "")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDataCacheBadIntegrity(t *testing.T) {
	dir := t.TempDir()
	dc := NewDataCache(dir, "testbuildkey")

	data := []byte("original content")
	if err := dc.StoreFile("corrupt.bin", data, ""); err != nil {
		t.Fatalf("StoreFile: %v", err)
	}

	// Corrupt the file on disk
	fp := dc.FilePath("corrupt.bin", "")
	if err := os.WriteFile(fp, []byte("corrupted!"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := dc.GetFile("corrupt.bin", "")
	if err == nil {
		t.Fatal("expected integrity error for corrupted file")
	}
}

func TestDataCacheManifestLastAccess(t *testing.T) {
	dir := t.TempDir()
	dc := NewDataCache(dir, "testbuildkey")

	// Init sets lastAccess
	if dc.Meta().LastAccess == 0 {
		t.Fatal("lastAccess should be set")
	}

	// Store another file, verify lastAccess updates
	oldAccess := dc.Meta().LastAccess
	dc.StoreFile("another.bin", []byte("data"), "")
	if dc.Meta().LastAccess == 0 {
		t.Fatal("lastAccess not set")
	}
	if dc.Meta().LastAccess < oldAccess {
		t.Fatal("lastAccess should increase")
	}
}

func TestDataCacheNormalizedPaths(t *testing.T) {
	dc := NewDataCache("C:\\Users\\test\\cache", "key")
	fp := dc.FilePath("sub", "file.bin")
	if strings.Contains(fp, "\\") {
		t.Fatal("paths should use forward slashes")
	}
}

func TestDataCacheSubdirStore(t *testing.T) {
	dir := t.TempDir()
	dc := NewDataCache(dir, "testbuildkey")

	data := []byte("subdirectory file content")
	if err := dc.StoreFile("sub/deep/nested/file.bin", data, ""); err != nil {
		t.Fatalf("StoreFile with subdir: %v", err)
	}

	got, err := dc.GetFile("sub/deep/nested/file.bin", "")
	if err != nil {
		t.Fatalf("GetFile with subdir: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("data mismatch for subdir file")
	}
}
