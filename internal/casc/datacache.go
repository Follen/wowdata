package casc

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wowdata/internal/storage"
)

type CacheMeta struct {
	LastAccess int64 `json:"lastAccess"`
}

type DataCache struct {
	mu           sync.Mutex
	root         string
	key          string
	manifestPath string
	meta         CacheMeta
	integrity    map[string]string
}

func NewDataCache(base string, key string) *DataCache {
	root := filepath.Join(base, "casc", key)
	dc := &DataCache{
		root:         root,
		key:          key,
		manifestPath: filepath.Join(root, "build_manifest.json"),
		meta:         CacheMeta{LastAccess: time.Now().UnixMilli()},
		integrity:    make(map[string]string),
	}
	os.MkdirAll(root, 0755)

	// Try loading existing manifest
	if data, err := os.ReadFile(dc.manifestPath); err == nil {
		json.Unmarshal(data, &dc.meta)
	}

	// Try loading integrity
	intPath := filepath.Join(root, "cache_integrity.json")
	if data, err := os.ReadFile(intPath); err == nil {
		json.Unmarshal(data, &dc.integrity)
	}

	// Save manifest
	dc.saveManifest()

	return dc
}

func (dc *DataCache) Root() string { return filepath.ToSlash(dc.root) }

func (dc *DataCache) Key() string { return dc.key }

func (dc *DataCache) ManifestPath() string { return filepath.ToSlash(dc.manifestPath) }

func (dc *DataCache) Meta() CacheMeta {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.meta
}

func (dc *DataCache) FilePath(file string, dir string) string {
	base := dir
	if base == "" {
		base = dc.root
	}
	return filepath.ToSlash(filepath.Join(base, file))
}

func (dc *DataCache) integrityKey(filePath string) string {
	rel, err := filepath.Rel(dc.root, filePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(filePath)
	}
	return filepath.ToSlash(rel)
}

func (dc *DataCache) GetFile(file string, dir string) ([]byte, error) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	filePath := dc.FilePath(file, dir)

	intHash, ok := dc.integrity[dc.integrityKey(filePath)]
	if !ok {
		return nil, fmt.Errorf("integrity not available for: %s", filePath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	actualHash := fmt.Sprintf("%x", sha256.Sum256(data))
	if actualHash != intHash {
		return nil, fmt.Errorf("bad integrity for %s: expected %s, got %s", filePath, intHash, actualHash)
	}

	return data, nil
}

func (dc *DataCache) StoreFile(file string, data []byte, dir string) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	filePath := dc.FilePath(file, dir)

	// Ensure parent dir exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	if err := storage.AtomicWriteFile(filePath, data, 0o644); err != nil {
		return err
	}

	// Publish integrity only after the complete cache object is visible.
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	dc.integrity[dc.integrityKey(filePath)] = hash

	dc.meta.LastAccess = time.Now().UnixMilli()
	dc.saveManifest()
	dc.saveIntegrity()

	return nil
}

func (dc *DataCache) saveManifest() error {
	data, err := json.Marshal(dc.meta)
	if err != nil {
		return err
	}
	return storage.AtomicWriteFile(dc.manifestPath, data, 0o644)
}

func (dc *DataCache) saveIntegrity() error {
	data, err := json.Marshal(dc.integrity)
	if err != nil {
		return err
	}
	intPath := filepath.Join(dc.root, "cache_integrity.json")
	return storage.AtomicWriteFile(intPath, data, 0o644)
}
