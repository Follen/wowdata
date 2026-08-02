package storage

import (
	"os"
	"path/filepath"
	"testing"
)

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
	if config.CacheMaxBytes != DefaultCacheMaxBytes || config.Workers != DefaultWorkers {
		t.Fatalf("unexpected defaults: %#v", config)
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
