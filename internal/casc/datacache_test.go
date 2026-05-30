package casc

import (
	"os"
	"path/filepath"
	"strings"
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
