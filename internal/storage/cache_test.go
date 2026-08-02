package storage

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
