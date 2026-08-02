package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"wowdata/internal/app"
	"wowdata/internal/casc"
	"wowdata/internal/storage"
)

func TestResolveTargetRequiresEveryField(t *testing.T) {
	cmd := app.NewRootCommandWithService(nil)
	_, err := resolveTarget(cmd, mustTestLayout(t))
	targetErr, ok := err.(targetRequiredError)
	if !ok {
		t.Fatalf("error = %#v, want targetRequiredError", err)
	}
	want := []string{"source", "region", "product", "build", "locale"}
	if !reflect.DeepEqual(targetErr.Missing, want) {
		t.Fatalf("missing = %#v, want %#v", targetErr.Missing, want)
	}
}

func TestResolveTargetAcceptsCompleteFlags(t *testing.T) {
	cmd := app.NewRootCommandWithService(nil)
	for name, value := range map[string]string{"source": "remote", "region": "cn", "product": "wow", "build": "latest", "locale": "zhCN"} {
		if err := cmd.PersistentFlags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := resolveTarget(cmd, mustTestLayout(t))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target.Product != "wow" || resolved.Target.Build != "latest" {
		t.Fatalf("target = %#v", resolved.Target)
	}
}

func TestResolveTargetLoadsExplicitProfile(t *testing.T) {
	layout := mustTestLayout(t)
	profile := storage.Profile{Name: "retail-cn", Target: storage.Target{Source: "remote", Region: "cn", Product: "wow", Build: "latest", Locale: "zhCN"}}
	if err := layout.SaveProfile(profile); err != nil {
		t.Fatal(err)
	}
	cmd := app.NewRootCommandWithService(nil)
	if err := cmd.PersistentFlags().Set("profile", "retail-cn"); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveTarget(cmd, layout)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProfileName != "retail-cn" || resolved.Target != profile.Target {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestBuildIndexBySelectionSupportsLatestBuildIDAndConfigKey(t *testing.T) {
	builds := []casc.VersionEntry{{Product: "wow", VersionsName: "12.0.7.68887", BuildConfig: "build-key"}}
	for _, selection := range []string{"latest", "68887", "12.0.7.68887", "build-key"} {
		if got := buildIndexBySelection(builds, "wow", selection); got != 0 {
			t.Fatalf("selection %q = %d, want 0", selection, got)
		}
	}
}

func mustTestLayout(t *testing.T) storage.Layout {
	t.Helper()
	layout, err := storage.Resolve(filepath.Join(t.TempDir(), ".wowdata"))
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	return layout
}
