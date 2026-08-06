package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	want := []string{"region", "product", "build", "locale"}
	if !reflect.DeepEqual(targetErr.Missing, want) {
		t.Fatalf("missing = %#v, want %#v", targetErr.Missing, want)
	}
}

func TestResolveTargetAutoSelectsMatchingLocalBuild(t *testing.T) {
	layout := mustTestLayout(t)
	gameDir := t.TempDir()
	buildInfo := "Product!STRING:0|Branch!STRING:0|Version!STRING:0|Build Key!HEX:16|CDN Key!HEX:16\n" +
		"wow|retail|12.0.7.68974|buildkey|cdnkey\n"
	if err := os.WriteFile(filepath.Join(gameDir, ".build.info"), []byte(buildInfo), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := layout.SaveEnv(storage.EnvConfig{GameDir: gameDir}); err != nil {
		t.Fatal(err)
	}
	cmd := app.NewRootCommandWithService(nil)
	for name, value := range map[string]string{"region": "cn", "product": "wow", "build": "12.0.7.68974", "locale": "zhCN"} {
		if err := cmd.PersistentFlags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := resolveTarget(cmd, layout)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target.Source != "local" || filepath.Clean(resolved.Target.Path) != filepath.Clean(gameDir) || !resolved.AutoSource {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveTargetAutoFallsBackToRemoteWhenBuildMissingLocally(t *testing.T) {
	layout := mustTestLayout(t)
	gameDir := t.TempDir()
	buildInfo := "Product!STRING:0|Branch!STRING:0|Version!STRING:0|Build Key!HEX:16|CDN Key!HEX:16\n" +
		"wow|retail|12.0.6.68000|buildkey|cdnkey\n"
	if err := os.WriteFile(filepath.Join(gameDir, ".build.info"), []byte(buildInfo), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := layout.SaveEnv(storage.EnvConfig{GameDir: gameDir}); err != nil {
		t.Fatal(err)
	}
	cmd := app.NewRootCommandWithService(nil)
	for name, value := range map[string]string{"region": "cn", "product": "wow", "build": "12.0.7.68974", "locale": "zhCN"} {
		if err := cmd.PersistentFlags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := resolveTarget(cmd, layout)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target.Source != "remote" || resolved.Target.Path != "" || !resolved.AutoSource {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveTargetExplicitRemoteIgnoresLocalEnv(t *testing.T) {
	layout := mustTestLayout(t)
	if err := layout.SaveEnv(storage.EnvConfig{GameDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	cmd := app.NewRootCommandWithService(nil)
	for name, value := range map[string]string{"source": "remote", "region": "cn", "product": "wow", "build": "latest", "locale": "zhCN"} {
		if err := cmd.PersistentFlags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := resolveTarget(cmd, layout)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target.Source != "remote" || resolved.AutoSource {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveTargetExplicitLocalUsesEnvPath(t *testing.T) {
	layout := mustTestLayout(t)
	gameDir := t.TempDir()
	if err := layout.SaveEnv(storage.EnvConfig{GameDir: gameDir}); err != nil {
		t.Fatal(err)
	}
	cmd := app.NewRootCommandWithService(nil)
	for name, value := range map[string]string{"source": "local", "product": "wow", "build": "latest", "locale": "zhCN"} {
		if err := cmd.PersistentFlags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	resolved, err := resolveTarget(cmd, layout)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target.Source != "local" || filepath.Clean(resolved.Target.Path) != filepath.Clean(gameDir) {
		t.Fatalf("resolved = %#v", resolved)
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

func TestMergeCommandDependenciesCapturesFileDataID(t *testing.T) {
	cmd := app.NewRootCommandWithService(nil)
	leaf, args, err := cmd.Find([]string{"icon", "export", "--file-data-id", "123", "--output", "icon.png"})
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	opts := warmupOptions{}
	mergeCommandDependencies(leaf, leaf.Flags().Args(), &opts)
	if !reflect.DeepEqual(opts.FileDataIDs, []uint32{123}) {
		t.Fatalf("file data IDs = %#v", opts.FileDataIDs)
	}
}

func TestMergeCommandDependenciesLoadsListfileOnlyForNameQueries(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{args: []string{"file", "lookup", "--file-data-id", "123"}, want: true},
		{args: []string{"file", "search", "--query", "interface/icons"}, want: true},
		{args: []string{"file", "extension", "--extension", "blp"}, want: true},
		{args: []string{"file", "get", "--filename", "interface/icons/test.blp"}, want: true},
		{args: []string{"file", "get", "--file-data-id", "123"}, want: false},
		{args: []string{"file", "encoding", "--file-data-id", "123"}, want: false},
		{args: []string{"icon", "export", "--file-data-id", "123"}, want: false},
		{args: []string{"item", "get", "--item-id", "123"}, want: false},
		{args: []string{"creature", "model", "--file-data-id", "123"}, want: false},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, "_"), func(t *testing.T) {
			root := app.NewRootCommandWithService(nil)
			leaf, args, err := root.Find(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if err := leaf.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			opts := warmupOptions{}
			mergeCommandDependencies(leaf, leaf.Flags().Args(), &opts)
			if opts.WarmListfile != test.want {
				t.Fatalf("WarmListfile = %t, want %t", opts.WarmListfile, test.want)
			}
		})
	}
}

func TestMergeCommandDependenciesLoadsTACTKeysOnlyForDiagnose(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{args: []string{"casc", "diagnose"}, want: true},
		{args: []string{"file", "encoding", "--file-data-id", "123"}, want: false},
	} {
		t.Run(strings.Join(test.args, "_"), func(t *testing.T) {
			root := app.NewRootCommandWithService(nil)
			leaf, args, err := root.Find(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if err := leaf.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			opts := warmupOptions{}
			mergeCommandDependencies(leaf, leaf.Flags().Args(), &opts)
			if opts.WarmTACTKeys != test.want {
				t.Fatalf("WarmTACTKeys = %t, want %t", opts.WarmTACTKeys, test.want)
			}
		})
	}
}

func TestMergeCommandDependenciesUsesMetadataOnlyForCASCInspection(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{args: []string{"casc", "info"}, want: true},
		{args: []string{"casc", "diagnose"}, want: true},
		{args: []string{"warmup"}, want: false},
		{args: []string{"file", "encoding", "--file-data-id", "123"}, want: false},
	} {
		t.Run(strings.Join(test.args, "_"), func(t *testing.T) {
			root := app.NewRootCommandWithService(nil)
			leaf, args, err := root.Find(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if err := leaf.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			opts := warmupOptions{}
			mergeCommandDependencies(leaf, leaf.Flags().Args(), &opts)
			if opts.MetadataOnly != test.want {
				t.Fatalf("MetadataOnly = %t, want %t", opts.MetadataOnly, test.want)
			}
		})
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
