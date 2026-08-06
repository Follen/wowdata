package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvConfigSupportsLocalPathAliasesAndQuotes(t *testing.T) {
	for _, test := range []struct {
		name, line string
	}{
		{name: "canonical", line: `WOWDATA_GAME_DIR="D:\Game\World of Warcraft"`},
		{name: "wow-dir-alias", line: `wowdata_wow_dir='D:\Game\World of Warcraft'`},
		{name: "local-path-alias", line: ` WOWDATA_LOCAL_PATH = D:\Game\World of Warcraft `},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseEnvConfig("# comment\n" + test.line + "\n")
			if err != nil {
				t.Fatal(err)
			}
			want, err := filepath.Abs(`D:\Game\World of Warcraft`)
			if err != nil {
				t.Fatal(err)
			}
			if got.GameDir != want {
				t.Fatalf("GameDir = %q, want %q", got.GameDir, want)
			}
		})
	}
}

func TestSaveEnvRoundTripsGameDirectory(t *testing.T) {
	layout, err := Resolve(filepath.Join(t.TempDir(), ".wowdata"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(t.TempDir(), "World of Warcraft")
	if err := layout.SaveEnv(EnvConfig{GameDir: want}); err != nil {
		t.Fatal(err)
	}
	got, err := layout.LoadEnv()
	if err != nil {
		t.Fatal(err)
	}
	if got.GameDir != want {
		t.Fatalf("GameDir = %q, want %q", got.GameDir, want)
	}
}

func TestLayoutEnsureAndConfigDefaults(t *testing.T) {
	layout, err := Resolve(filepath.Join(t.TempDir(), ".wowdata"))
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{layout.Bin, layout.Config, layout.Profiles, layout.Builds, layout.Cache, layout.State, layout.Temp, layout.Locks} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("missing directory %s: %v", dir, err)
		}
	}
	config, err := layout.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.CacheMaxBytes != DefaultCacheMaxBytes || config.Workers != 0 || config.WorkerMode != WorkerModeAuto {
		t.Fatalf("unexpected defaults: %#v", config)
	}
}

func TestLoadConfigMigratesLegacyDefaultWorkersToAuto(t *testing.T) {
	layout, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(layout.Config, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"schema":"wowdata.config.v1","cacheMaxBytes":21474836480,"downloadWorkers":4}`)
	if err := os.WriteFile(layout.ConfigPath(), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	config, err := layout.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.WorkerMode != WorkerModeAuto || config.Workers != 0 {
		t.Fatalf("legacy config = %#v", config)
	}
}

func TestLoadConfigPreservesLegacyNonDefaultWorkersAsFixed(t *testing.T) {
	layout, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(layout.Config, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"schema":"wowdata.config.v1","cacheMaxBytes":21474836480,"downloadWorkers":7}`)
	if err := os.WriteFile(layout.ConfigPath(), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	config, err := layout.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.WorkerMode != WorkerModeFixed || config.Workers != 7 {
		t.Fatalf("legacy config = %#v", config)
	}
}

func TestEnsureRebuildsOnlyLegacyCache(t *testing.T) {
	layout, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(layout.Cache, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(layout.Cache, "legacy.bin")
	if err := os.WriteFile(legacy, []byte("rebuildable"), 0644); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(layout.Profiles, "keep.json")
	if err := os.MkdirAll(layout.Profiles, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profile, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy cache was not removed: %v", err)
	}
	if data, err := os.ReadFile(profile); err != nil || string(data) != "keep" {
		t.Fatalf("profile changed: data=%q err=%v", data, err)
	}
	var format CacheFormat
	data, err := os.ReadFile(filepath.Join(layout.Cache, "format.json"))
	if err != nil || json.Unmarshal(data, &format) != nil || format.Version != CacheFormatVersion {
		t.Fatalf("cache format = %#v err=%v", format, err)
	}
	if format.ReclaimedBytes != int64(len("rebuildable")) || filepath.Clean(format.ClearedPath) != filepath.Clean(layout.Cache) {
		t.Fatalf("cache cleanup evidence = %#v", format)
	}
}

func TestProfileRoundTrip(t *testing.T) {
	layout, _ := Resolve(t.TempDir())
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	profile := Profile{Name: "retail-cn", Target: Target{Source: "remote", Region: "cn", Product: "wow", Build: "latest", Locale: "zhCN"}}
	if err := layout.SaveProfile(profile); err != nil {
		t.Fatal(err)
	}
	loaded, err := layout.LoadProfile("retail-cn")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Target != profile.Target || loaded.Schema != "wowdata.profile.v1" {
		t.Fatalf("profile mismatch: %#v", loaded)
	}
}

func TestTargetRequiresCompleteContext(t *testing.T) {
	target := Target{Source: "remote", Region: "cn", Product: "wow", Locale: "zhCN"}
	missing := target.MissingFields()
	if len(missing) != 1 || missing[0] != "build" {
		t.Fatalf("missing = %#v, want build", missing)
	}
	target.Build = "latest"
	if err := target.Validate(); err != nil {
		t.Fatalf("complete target rejected: %v", err)
	}
}
