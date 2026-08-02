package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultCacheMaxBytes int64 = 20 * 1024 * 1024 * 1024
	DefaultWorkers             = 4
)

type Layout struct {
	Root     string `json:"root"`
	Bin      string `json:"bin"`
	Config   string `json:"config"`
	Profiles string `json:"profiles"`
	Builds   string `json:"builds"`
	Cache    string `json:"cache"`
	State    string `json:"state"`
	Temp     string `json:"tmp"`
	Locks    string `json:"locks"`
}

type Config struct {
	Schema        string `json:"schema"`
	CacheMaxBytes int64  `json:"cacheMaxBytes"`
	Workers       int    `json:"downloadWorkers"`
}

type Target struct {
	Source  string `json:"source"`
	Path    string `json:"path,omitempty"`
	Region  string `json:"region"`
	Product string `json:"product"`
	Build   string `json:"build"`
	Locale  string `json:"locale"`
}

func (t Target) MissingFields() []string {
	missing := make([]string, 0, 6)
	if strings.TrimSpace(t.Source) == "" {
		missing = append(missing, "source")
	}
	if t.Source == "local" && strings.TrimSpace(t.Path) == "" {
		missing = append(missing, "path")
	}
	if strings.TrimSpace(t.Region) == "" {
		missing = append(missing, "region")
	}
	if strings.TrimSpace(t.Product) == "" {
		missing = append(missing, "product")
	}
	if strings.TrimSpace(t.Build) == "" {
		missing = append(missing, "build")
	}
	if strings.TrimSpace(t.Locale) == "" {
		missing = append(missing, "locale")
	}
	return missing
}

func (t Target) Validate() error {
	if missing := t.MissingFields(); len(missing) > 0 {
		return fmt.Errorf("target fields are required: %s", strings.Join(missing, ","))
	}
	if t.Source != "remote" && t.Source != "local" {
		return fmt.Errorf("source must be remote or local")
	}
	return nil
}

type Profile struct {
	Schema         string   `json:"schema"`
	Name           string   `json:"name"`
	Target         Target   `json:"target"`
	ResolvedBuild  string   `json:"resolvedBuild,omitempty"`
	BuildConfigKey string   `json:"buildConfigKey,omitempty"`
	CDNConfigKey   string   `json:"cdnConfigKey,omitempty"`
	RecentBuilds   []string `json:"recentBuilds,omitempty"`
	UpdatedAt      string   `json:"updatedAt"`
}

type BuildSnapshot struct {
	Schema         string `json:"schema"`
	Source         string `json:"source"`
	Path           string `json:"path,omitempty"`
	Region         string `json:"region"`
	Product        string `json:"product"`
	Locale         string `json:"locale"`
	Version        string `json:"version"`
	BuildID        string `json:"buildId,omitempty"`
	BuildConfigKey string `json:"buildConfigKey"`
	CDNConfigKey   string `json:"cdnConfigKey"`
	CreatedAt      string `json:"createdAt"`
	LastUsedAt     string `json:"lastUsedAt"`
}

func Resolve(root string) (Layout, error) {
	if strings.TrimSpace(root) == "" {
		root = strings.TrimSpace(os.Getenv("WOWDATA_HOME"))
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Layout{}, fmt.Errorf("resolve user home: %w", err)
		}
		root = filepath.Join(home, ".wowdata")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, fmt.Errorf("resolve wowdata home: %w", err)
	}
	return Layout{
		Root:     abs,
		Bin:      filepath.Join(abs, "bin"),
		Config:   filepath.Join(abs, "config"),
		Profiles: filepath.Join(abs, "profiles"),
		Builds:   filepath.Join(abs, "builds"),
		Cache:    filepath.Join(abs, "cache"),
		State:    filepath.Join(abs, "state"),
		Temp:     filepath.Join(abs, "tmp"),
		Locks:    filepath.Join(abs, "locks"),
	}, nil
}

func (l Layout) Ensure() error {
	dirs := []string{
		l.Root, l.Bin, l.Config, l.Profiles, l.Builds, l.Cache,
		filepath.Join(l.Cache, "casc"), filepath.Join(l.Cache, "dbd"),
		filepath.Join(l.Cache, "listfile"), filepath.Join(l.Cache, "tact"),
		filepath.Join(l.Cache, "manifests"), l.State, l.Temp, l.Locks,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

func (l Layout) ConfigPath() string { return filepath.Join(l.Config, "config.json") }

func (l Layout) LoadConfig() (Config, error) {
	config := Config{Schema: "wowdata.config.v1", CacheMaxBytes: DefaultCacheMaxBytes, Workers: DefaultWorkers}
	data, err := os.ReadFile(l.ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if config.CacheMaxBytes <= 0 {
		config.CacheMaxBytes = DefaultCacheMaxBytes
	}
	if config.Workers <= 0 {
		config.Workers = DefaultWorkers
	}
	config.Schema = "wowdata.config.v1"
	return config, nil
}

func (l Layout) SaveConfig(config Config) error {
	config.Schema = "wowdata.config.v1"
	if config.CacheMaxBytes <= 0 {
		return fmt.Errorf("cacheMaxBytes must be positive")
	}
	if config.Workers <= 0 {
		return fmt.Errorf("downloadWorkers must be positive")
	}
	return AtomicWriteJSON(l.ConfigPath(), config, 0o644)
}

func (l Layout) ProfilePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("invalid profile name %q", name)
	}
	return filepath.Join(l.Profiles, name+".json"), nil
}

func (l Layout) LoadProfile(name string) (Profile, error) {
	path, err := l.ProfilePath(name)
	if err != nil {
		return Profile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return Profile{}, fmt.Errorf("parse profile %s: %w", name, err)
	}
	if profile.Name != name {
		return Profile{}, fmt.Errorf("profile name mismatch: file=%s content=%s", name, profile.Name)
	}
	return profile, nil
}

func (l Layout) SaveProfile(profile Profile) error {
	path, err := l.ProfilePath(profile.Name)
	if err != nil {
		return err
	}
	profile.Schema = "wowdata.profile.v1"
	profile.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return AtomicWriteJSON(path, profile, 0o644)
}

func (l Layout) ListProfiles() ([]Profile, error) {
	entries, err := os.ReadDir(l.Profiles)
	if errors.Is(err, os.ErrNotExist) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	profiles := make([]Profile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		profile, err := l.LoadProfile(name)
		if err != nil {
			continue
		}
		profiles = append(profiles, profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

func (l Layout) SaveBuild(snapshot BuildSnapshot) (string, error) {
	key := strings.TrimSpace(snapshot.BuildConfigKey)
	if key == "" {
		key = sanitizeName(snapshot.Product + "-" + snapshot.Version)
	}
	name := sanitizeName(key + "-" + snapshot.Locale)
	path := filepath.Join(l.Builds, name+".json")
	now := time.Now().UTC().Format(time.RFC3339)
	if data, err := os.ReadFile(path); err == nil {
		var previous BuildSnapshot
		if json.Unmarshal(data, &previous) == nil && previous.CreatedAt != "" {
			snapshot.CreatedAt = previous.CreatedAt
		}
	}
	if snapshot.CreatedAt == "" {
		snapshot.CreatedAt = now
	}
	snapshot.Schema = "wowdata.build.v1"
	snapshot.LastUsedAt = now
	return name, AtomicWriteJSON(path, snapshot, 0o644)
}

func AtomicWriteJSON(path string, value any, perm fs.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return AtomicWriteFile(path, data, perm)
}

func AtomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".wowdata-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tmpPath, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func DirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return total, err
}

func sanitizeName(value string) string {
	value = strings.TrimSpace(value)
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	return strings.Trim(out.String(), "-.")
}
