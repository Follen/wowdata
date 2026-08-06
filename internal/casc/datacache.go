package casc

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wowdata/internal/blte"
	"wowdata/internal/resource"
	"wowdata/internal/storage"
)

type CacheMeta struct {
	LastAccess int64 `json:"lastAccess"`
}

type DataCache struct {
	mu           sync.Mutex
	base         string
	root         string
	objects      string
	key          string
	manifestPath string
	meta         CacheMeta
	integrity    map[string]string
}

func NewDataCache(base string, key string) *DataCache {
	root := filepath.Join(base, "casc", "builds", key)
	dc := &DataCache{
		base:         base,
		root:         root,
		objects:      filepath.Join(base, "casc", "objects"),
		key:          key,
		manifestPath: filepath.Join(root, "build_manifest.json"),
		meta:         CacheMeta{LastAccess: time.Now().UnixMilli()},
		integrity:    make(map[string]string),
	}
	os.MkdirAll(root, 0755)

	// Try loading existing manifest
	if data, err := resource.ReadFile(dc.manifestPath); err == nil {
		json.Unmarshal(data, &dc.meta)
	}

	// Try loading integrity
	intPath := filepath.Join(root, "cache_integrity.json")
	if data, err := resource.ReadFile(intPath); err == nil {
		json.Unmarshal(data, &dc.integrity)
	}

	return dc
}

func (dc *DataCache) Root() string { return filepath.ToSlash(dc.root) }

func (dc *DataCache) ResumeRoot() string {
	return filepath.ToSlash(filepath.Join(dc.base, "casc", "resume"))
}

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

func (dc *DataCache) ObjectPath(file string) string {
	name := filepath.Base(filepath.Clean(file))
	key := strings.TrimSuffix(strings.ToLower(name), ".index")
	prefix := "misc"
	if len(key) >= 2 {
		prefix = key[:2]
	}
	return filepath.ToSlash(filepath.Join(dc.objects, prefix, name))
}

func (dc *DataCache) integrityKey(filePath string) string {
	rel, err := filepath.Rel(dc.root, filePath)
	if err != nil {
		return filepath.ToSlash(filePath)
	}
	return filepath.ToSlash(rel)
}

func (dc *DataCache) GetFile(file string, dir string) ([]byte, error) {
	return dc.getFileAt(dc.FilePath(file, dir))
}

func (dc *DataCache) GetObject(file string) ([]byte, error) {
	return dc.getFileAt(dc.ObjectPath(file))
}

func (dc *DataCache) getFileAt(filePath string) ([]byte, error) {
	dc.mu.Lock()
	intHash, ok := dc.integrity[dc.integrityKey(filePath)]
	dc.mu.Unlock()
	if !ok {
		if dc.isObjectPath(filePath) {
			data, err := resource.ReadFile(filePath + ".sha256")
			if err == nil {
				intHash = strings.TrimSpace(string(data))
				ok = len(intHash) == 64
			}
		}
		if !ok {
			return nil, fmt.Errorf("integrity not available for: %s", filePath)
		}
		if err := dc.trackIntegrity(filePath, intHash); err != nil {
			return nil, err
		}
	}

	data, err := resource.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	actualHash := fmt.Sprintf("%x", resource.SumSHA256(data))
	if actualHash != intHash {
		return nil, fmt.Errorf("bad integrity for %s: expected %s, got %s", filePath, intHash, actualHash)
	}
	return data, nil
}

// GetBLTEFile avoids hashing a payload twice when its explicit BLTE block
// hashes will be verified by the decoder immediately after this read. Files
// without complete block hashes retain the normal SHA-256 cache check.
func (dc *DataCache) GetBLTEFile(file string, dir string) ([]byte, error) {
	return dc.getBLTEFileAt(file, dc.FilePath(file, dir))
}

func (dc *DataCache) GetBLTEObject(file string) ([]byte, error) {
	return dc.getBLTEFileAt(file, dc.ObjectPath(file))
}

func (dc *DataCache) getBLTEFileAt(file, filePath string) ([]byte, error) {
	dc.mu.Lock()
	_, tracked := dc.integrity[dc.integrityKey(filePath)]
	dc.mu.Unlock()
	if !tracked && dc.isObjectPath(filePath) {
		if data, err := resource.ReadFile(filePath + ".sha256"); err == nil && len(strings.TrimSpace(string(data))) == 64 {
			tracked = true
			if err := dc.trackIntegrity(filePath, strings.TrimSpace(string(data))); err != nil {
				return nil, err
			}
		}
	}
	if !tracked {
		return nil, fmt.Errorf("integrity not available for: %s", filePath)
	}
	data, err := resource.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	header := blte.ParseBLTEHeader(data)
	if header == nil || len(header.Blocks) == 0 {
		return dc.getFileAt(filePath)
	}
	for _, block := range header.Blocks {
		if block.Hash == "00000000000000000000000000000000" {
			key := strings.ToLower(filepath.Base(file))
			if len(key) == 32 && fmt.Sprintf("%x", md5.Sum(data)) == key {
				return data, nil
			}
			return dc.getFileAt(filePath)
		}
	}
	return data, nil
}

func (dc *DataCache) StoreFile(file string, data []byte, dir string) error {
	return dc.storeFileAt(dc.FilePath(file, dir), data)
}

func (dc *DataCache) StoreObject(file string, data []byte) error {
	return dc.storeFileAt(dc.ObjectPath(file), data)
}

func (dc *DataCache) RemoveObjectsPrefix(prefix string) error {
	prefix = strings.ToLower(filepath.Base(filepath.Clean(prefix)))
	if prefix == "." || prefix == "" {
		return fmt.Errorf("object prefix is empty")
	}
	dir := filepath.Dir(dc.ObjectPath(prefix))
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(strings.ToLower(entry.Name()), prefix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (dc *DataCache) storeFileAt(filePath string, data []byte) error {
	if dc.isObjectPath(filePath) {
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return err
		}
		if err := storage.AtomicWriteFile(filePath, data, 0o644); err != nil {
			return err
		}
		hash := fmt.Sprintf("%x", resource.SumSHA256(data))
		if err := storage.AtomicWriteFile(filePath+".sha256", []byte(hash+"\n"), 0o644); err != nil {
			_ = os.Remove(filePath)
			return err
		}
		dc.mu.Lock()
		dc.integrity[dc.integrityKey(filePath)] = hash
		dc.meta.LastAccess = time.Now().UnixMilli()
		dc.mu.Unlock()
		return nil
	}

	dc.mu.Lock()
	defer dc.mu.Unlock()

	// Ensure parent dir exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	if err := storage.AtomicWriteFile(filePath, data, 0o644); err != nil {
		return err
	}

	// Publish integrity only after the complete cache object is visible.
	hash := fmt.Sprintf("%x", resource.SumSHA256(data))
	dc.integrity[dc.integrityKey(filePath)] = hash

	dc.meta.LastAccess = time.Now().UnixMilli()
	return dc.saveStateLocked()
}

func (dc *DataCache) isObjectPath(path string) bool {
	rel, err := filepath.Rel(dc.objects, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (dc *DataCache) AcquireObjectLock(file string, timeout time.Duration) (*storage.Lock, error) {
	return storage.AcquirePathLock(filepath.Join(dc.base, "casc", "object_locks"), strings.ToLower(file), timeout)
}

func (dc *DataCache) trackIntegrity(filePath, hash string) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	key := dc.integrityKey(filePath)
	if dc.integrity[key] == hash {
		return nil
	}
	dc.integrity[key] = hash
	dc.meta.LastAccess = time.Now().UnixMilli()
	if dc.isObjectPath(filePath) {
		return nil
	}
	return dc.saveStateLocked()
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

func (dc *DataCache) saveStateLocked() error {
	lock, err := storage.AcquirePathLock(filepath.Join(dc.base, "casc", "build_locks"), dc.key, 30*time.Second)
	if err != nil {
		return err
	}
	defer lock.Release()
	intPath := filepath.Join(dc.root, "cache_integrity.json")
	if data, err := resource.ReadFile(intPath); err == nil {
		var disk map[string]string
		if json.Unmarshal(data, &disk) == nil {
			for key, hash := range disk {
				if _, exists := dc.integrity[key]; !exists {
					dc.integrity[key] = hash
				}
			}
		}
	}
	if err := dc.saveManifest(); err != nil {
		return err
	}
	return dc.saveIntegrity()
}
