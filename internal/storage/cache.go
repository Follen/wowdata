package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CacheVerification struct {
	OK        bool     `json:"ok"`
	Manifests int      `json:"manifests"`
	Files     int      `json:"files"`
	Missing   []string `json:"missing"`
	Corrupt   []string `json:"corrupt"`
	Errors    []string `json:"errors"`
}

type CachePruneResult struct {
	BeforeBytes int64    `json:"beforeBytes"`
	AfterBytes  int64    `json:"afterBytes"`
	MaxBytes    int64    `json:"maxBytes"`
	Removed     []string `json:"removed"`
}

func (l Layout) VerifyCache() CacheVerification {
	result := CacheVerification{OK: true, Missing: []string{}, Corrupt: []string{}, Errors: []string{}}
	err := filepath.WalkDir(l.Cache, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return nil
		}
		if entry.IsDir() || entry.Name() != "cache_integrity.json" {
			return nil
		}
		result.Manifests++
		data, err := os.ReadFile(path)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return nil
		}
		var manifest map[string]string
		if err := json.Unmarshal(data, &manifest); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", filepath.ToSlash(path), err))
			return nil
		}
		root := filepath.Dir(path)
		for name, expected := range manifest {
			result.Files++
			filePath := name
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(root, filepath.FromSlash(name))
			}
			body, err := os.ReadFile(filePath)
			if errors.Is(err, os.ErrNotExist) {
				result.Missing = append(result.Missing, filepath.ToSlash(filePath))
				continue
			}
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			actual := sha256.Sum256(body)
			if len(expected) != 64 || !strings.EqualFold(hex.EncodeToString(actual[:]), expected) {
				result.Corrupt = append(result.Corrupt, filepath.ToSlash(filePath))
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		result.Errors = append(result.Errors, err.Error())
	}
	sort.Strings(result.Missing)
	sort.Strings(result.Corrupt)
	sort.Strings(result.Errors)
	result.OK = len(result.Missing) == 0 && len(result.Corrupt) == 0 && len(result.Errors) == 0
	return result
}

func (l Layout) PruneCache(maxBytes int64) (CachePruneResult, error) {
	return l.PruneCacheKeeping(maxBytes, nil)
}

func (l Layout) PruneCacheKeeping(maxBytes int64, extraBuildKeys []string) (CachePruneResult, error) {
	before, err := DirSize(l.Cache)
	result := CachePruneResult{BeforeBytes: before, AfterBytes: before, MaxBytes: maxBytes, Removed: []string{}}
	if err != nil || before <= maxBytes {
		return result, err
	}
	protected, err := l.protectedBuildKeys()
	if err != nil {
		return result, err
	}
	for _, key := range extraBuildKeys {
		if key != "" {
			protected[key] = true
		}
	}
	cascRoot := filepath.Join(l.Cache, "casc")
	entries, err := os.ReadDir(cascRoot)
	if err != nil {
		return result, err
	}
	type candidate struct {
		path string
		name string
		mod  int64
		size int64
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || protected[entry.Name()] {
			continue
		}
		path := filepath.Join(cascRoot, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		size, err := DirSize(path)
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{path: path, name: entry.Name(), mod: info.ModTime().UnixNano(), size: size})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].mod < candidates[j].mod })
	for _, candidate := range candidates {
		if result.AfterBytes <= maxBytes {
			break
		}
		if err := ensureChild(l.Cache, candidate.path); err != nil {
			return result, err
		}
		if err := os.RemoveAll(candidate.path); err != nil {
			return result, err
		}
		result.AfterBytes -= candidate.size
		result.Removed = append(result.Removed, candidate.name)
	}
	actual, err := DirSize(l.Cache)
	if err == nil {
		result.AfterBytes = actual
	}
	return result, err
}

func (l Layout) ClearCache() error {
	if err := ensureChild(l.Root, l.Cache); err != nil {
		return err
	}
	if err := os.RemoveAll(l.Cache); err != nil {
		return err
	}
	return l.Ensure()
}

func (l Layout) protectedBuildKeys() (map[string]bool, error) {
	profiles, err := l.ListProfiles()
	if err != nil {
		return nil, err
	}
	protected := make(map[string]bool)
	for _, profile := range profiles {
		for _, ref := range profile.RecentBuilds {
			data, err := os.ReadFile(filepath.Join(l.Builds, ref+".json"))
			if err != nil {
				continue
			}
			var snapshot BuildSnapshot
			if json.Unmarshal(data, &snapshot) == nil && snapshot.BuildConfigKey != "" {
				protected[snapshot.BuildConfigKey] = true
			}
		}
	}
	return protected, nil
}

func ensureChild(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %s is outside managed root %s", target, root)
	}
	return nil
}
