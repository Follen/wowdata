package storage

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wowdata/internal/resource"
)

const DefaultResumeStateTTL = 24 * time.Hour

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

type CacheUsage struct {
	TotalBytes            int64   `json:"totalBytes"`
	PayloadBytes          int64   `json:"payloadBytes"`
	UniquePayloadBytes    int64   `json:"uniquePayloadBytes"`
	DuplicatePayloadBytes int64   `json:"duplicatePayloadBytes"`
	ResumeBytes           int64   `json:"resumeBytes"`
	DerivedMetadataBytes  int64   `json:"derivedMetadataBytes"`
	PayloadFiles          int     `json:"payloadFiles"`
	ResumeFiles           int     `json:"resumeFiles"`
	DerivedMetadataFiles  int     `json:"derivedMetadataFiles"`
	DiskAmplification     float64 `json:"diskAmplificationRatio"`
	MaxBytes              int64   `json:"maxBytes"`
	WithinMaxBytes        bool    `json:"withinMaxBytes"`
	WithinAmplification   bool    `json:"withinAmplificationLimit"`
}

// MeasureCacheUsage classifies managed cache files without decoding payloads.
// CASC object names are content identities, so repeated names across Build
// views can be counted without hashing every large object on a status read.
func MeasureCacheUsage(cacheRoot string, maxBytes int64) (CacheUsage, error) {
	usage := CacheUsage{MaxBytes: maxBytes, WithinMaxBytes: true, WithinAmplification: true}
	uniquePayloads := make(map[string]int64)
	err := filepath.WalkDir(cacheRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cacheRoot, path)
		if err != nil {
			return err
		}
		size := info.Size()
		usage.TotalBytes += size
		switch cacheFileCategory(rel) {
		case "resume":
			usage.ResumeBytes += size
			usage.ResumeFiles++
		case "derived":
			usage.DerivedMetadataBytes += size
			usage.DerivedMetadataFiles++
		default:
			usage.PayloadBytes += size
			usage.PayloadFiles++
			identity := payloadIdentity(rel)
			if previous, ok := uniquePayloads[identity]; !ok || size > previous {
				uniquePayloads[identity] = size
			}
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return CacheUsage{}, err
	}
	for _, size := range uniquePayloads {
		usage.UniquePayloadBytes += size
	}
	usage.DuplicatePayloadBytes = usage.PayloadBytes - usage.UniquePayloadBytes
	if usage.UniquePayloadBytes > 0 {
		usage.DiskAmplification = float64(usage.DuplicatePayloadBytes+usage.ResumeBytes+usage.DerivedMetadataBytes) / float64(usage.UniquePayloadBytes)
	}
	usage.WithinMaxBytes = maxBytes <= 0 || usage.TotalBytes <= maxBytes
	usage.WithinAmplification = usage.DiskAmplification <= 0.05
	return usage, nil
}

func cacheFileCategory(rel string) string {
	normalized := strings.ToLower(filepath.ToSlash(rel))
	base := filepath.Base(normalized)
	if strings.Contains("/"+normalized+"/", "/resume/") || strings.HasSuffix(base, ".part") {
		return "resume"
	}
	if strings.Contains("/"+normalized+"/", "/object_refs/") {
		return "derived"
	}
	switch base {
	case "build_manifest.json", "cache_integrity.json", "revision.json", "format.json":
		return "derived"
	default:
		if strings.HasSuffix(base, ".sha256") {
			return "derived"
		}
		return "payload"
	}
}

func payloadIdentity(rel string) string {
	normalized := strings.ToLower(filepath.ToSlash(rel))
	parts := strings.Split(normalized, "/")
	if len(parts) >= 3 && parts[0] == "casc" {
		base := parts[len(parts)-1]
		key := strings.TrimSuffix(base, ".index")
		if isHexContentKey(key) {
			return "casc/" + base
		}
	}
	return normalized
}

func isHexContentKey(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
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
		data, err := resource.ReadFile(path)
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
			body, err := resource.ReadFile(filePath)
			if errors.Is(err, os.ErrNotExist) {
				result.Missing = append(result.Missing, filepath.ToSlash(filePath))
				continue
			}
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			actual := resource.SumSHA256(body)
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
	if err != nil {
		return result, err
	}
	if err := l.pruneExpiredResumeState(time.Now(), DefaultResumeStateTTL); err != nil {
		return result, err
	}
	if err := l.pruneUnreferencedCASCObjects(); err != nil {
		return result, err
	}
	result.AfterBytes, err = DirSize(l.Cache)
	if err != nil || result.AfterBytes <= maxBytes {
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
	cascRoot := filepath.Join(l.Cache, "casc", "builds")
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
	if err := l.pruneUnreferencedCASCObjects(); err != nil {
		return result, err
	}
	actual, err := DirSize(l.Cache)
	if err == nil {
		result.AfterBytes = actual
	}
	return result, err
}

func (l Layout) pruneExpiredResumeState(now time.Time, ttl time.Duration) error {
	type resumePair struct {
		paths  []string
		latest time.Time
	}
	pairs := make(map[string]*resumePair)
	err := filepath.WalkDir(filepath.Join(l.Cache, "casc"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Base(filepath.Dir(path)), "resume") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".part" && ext != ".json" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		key := strings.TrimSuffix(path, filepath.Ext(path))
		pair := pairs[key]
		if pair == nil {
			pair = &resumePair{}
			pairs[key] = pair
		}
		pair.paths = append(pair.paths, path)
		if info.ModTime().After(pair.latest) {
			pair.latest = info.ModTime()
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, pair := range pairs {
		if ttl > 0 && now.Sub(pair.latest) < ttl {
			continue
		}
		for _, path := range pair.paths {
			if err := ensureChild(l.Cache, path); err != nil {
				return err
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func (l Layout) pruneUnreferencedCASCObjects() error {
	buildsRoot := filepath.Join(l.Cache, "casc", "builds")
	objectsRoot := filepath.Join(l.Cache, "casc", "objects")
	locksRoot := filepath.Join(l.Cache, "casc", "object_locks")
	if entries, err := os.ReadDir(locksRoot); err == nil && len(entries) > 0 {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	referenced := make(map[string]struct{})
	err := filepath.WalkDir(buildsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "cache_integrity.json" {
			return nil
		}
		data, err := resource.ReadFile(path)
		if err != nil {
			return err
		}
		var integrity map[string]string
		if err := json.Unmarshal(data, &integrity); err != nil {
			return err
		}
		for name := range integrity {
			objectPath := name
			if !filepath.IsAbs(objectPath) {
				objectPath = filepath.Join(filepath.Dir(path), filepath.FromSlash(name))
			}
			rel, err := filepath.Rel(objectsRoot, objectPath)
			if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				referenced[filepath.Clean(objectPath)] = struct{}{}
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var remove []string
	var orphanSidecars []string
	err = filepath.WalkDir(objectsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".sha256") {
			if _, statErr := os.Stat(strings.TrimSuffix(path, ".sha256")); errors.Is(statErr, os.ErrNotExist) {
				orphanSidecars = append(orphanSidecars, path)
			} else if statErr != nil {
				return statErr
			}
			return nil
		}
		if _, ok := referenced[filepath.Clean(path)]; !ok {
			remove = append(remove, path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, path := range remove {
		if err := ensureChild(l.Cache, path); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Remove(path + ".sha256"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, path := range orphanSidecars {
		if err := ensureChild(l.Cache, path); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
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
			data, err := resource.ReadFile(filepath.Join(l.Builds, ref+".json"))
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
