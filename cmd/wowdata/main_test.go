package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"wowdata/internal/app"
	"wowdata/internal/casc"
)

func TestWarmupOptionsIncludeLocale(t *testing.T) {
	cmd := app.NewRootCommandWithService(nil)
	if err := cmd.PersistentFlags().Set("locale", "zhCN"); err != nil {
		t.Fatalf("set locale flag: %v", err)
	}

	opts := warmupOptionsFromCommand(cmd)

	if opts.Locale != "zhCN" {
		t.Fatalf("locale = %q, want zhCN", opts.Locale)
	}
}

func TestResolveCacheRootUsesExplicitPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-cache")

	got := resolveCacheRoot(dir, func() (string, error) {
		return filepath.Join(t.TempDir(), "wowdata.exe"), nil
	})

	if got != dir {
		t.Fatalf("cache root = %q, want explicit path %q", got, dir)
	}
}

func TestResolveCacheRootDefaultsNextToExecutable(t *testing.T) {
	exeDir := t.TempDir()
	exe := filepath.Join(exeDir, "wowdata.exe")

	got := resolveCacheRoot("", func() (string, error) {
		return exe, nil
	})

	want := filepath.Join(exeDir, "cache")
	if got != want {
		t.Fatalf("cache root = %q, want %q", got, want)
	}
}

func TestApplyWarmupLocale(t *testing.T) {
	source := casc.NewCASCSource()

	if err := applyWarmupLocale(source, "zhCN"); err != nil {
		t.Fatalf("applyWarmupLocale: %v", err)
	}
	if source.Locale != casc.LocaleZhCN {
		t.Fatalf("source locale = %s, want zhCN", source.Locale.Name())
	}
}

func TestApplyWarmupLocaleRejectsUnknownLocale(t *testing.T) {
	source := casc.NewCASCSource()

	if err := applyWarmupLocale(source, "badLOCALE"); err == nil {
		t.Fatal("expected invalid locale error")
	}
}

func TestRootEntryMapIncludesAllPreloadInput(t *testing.T) {
	source := casc.NewCASCSource()
	source.Locale = casc.LocaleEnUS
	source.RootTypes = []casc.RootType{
		{LocaleFlags: casc.LocaleEnUS},
		{LocaleFlags: casc.LocaleZhCN},
		{LocaleFlags: casc.LocaleEnUS, ContentFlags: casc.ContentLowViolence},
	}
	source.RootEntries[100] = map[int]string{0: "en-us"}
	source.RootEntries[200] = map[int]string{1: "zh-cn"}
	source.RootEntries[300] = map[int]string{2: "low-violence"}

	got := rootEntryMap(source)
	for _, fdid := range []uint32{100, 200, 300} {
		if !got[fdid] {
			t.Fatalf("rootEntryMap omitted root fileDataID %d: %#v", fdid, got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("rootEntryMap size = %d, want 3", len(got))
	}

	valid := validRootEntries(source)
	if valid[200] || valid[300] {
		t.Fatalf("test setup expected locale-valid filtering to omit 200 and 300: %#v", valid)
	}
}

func TestWarmupResultAddsStableFields(t *testing.T) {
	result := map[string]interface{}{
		"source":    "local",
		"region":    "cn",
		"product":   "wow",
		"buildName": "12.0.5.67823",
		"warm": map[string]interface{}{
			"listfile":    false,
			"dbdManifest": false,
			"tables":      []string{},
		},
	}

	addWarmupSuccessFields(result, 0, "12.0.5.67823")

	if result["success"] != true || result["message"] != "warmup 完成" || result["buildIndex"] != 0 {
		t.Fatalf("missing stable warmup fields: %#v", result)
	}
	warmed, ok := result["warmed"].(map[string]interface{})
	if !ok {
		t.Fatalf("warmed missing or wrong type: %#v", result["warmed"])
	}
	if !reflect.DeepEqual(warmed["tables"], []string{}) || warmed["listfile"] != false || warmed["dbdManifest"] != false {
		t.Fatalf("warmed did not mirror warm options: %#v", warmed)
	}
}

func TestWarmupResultNormalizesNilTablesToEmptySlice(t *testing.T) {
	result := map[string]interface{}{
		"warm": map[string]interface{}{
			"listfile":    false,
			"dbdManifest": false,
			"tables":      []string(nil),
		},
	}

	addWarmupSuccessFields(result, 0, "build")

	warmed := result["warmed"].(map[string]interface{})
	if !reflect.DeepEqual(warmed["tables"], []string{}) {
		t.Fatalf("tables = %#v, want empty slice", warmed["tables"])
	}
}

func TestRuntimeHTTPWarmupGateRejectsConcurrentWarmup(t *testing.T) {
	rt := NewRuntime()

	release, err := rt.beginHTTPWarmup()
	if err != nil {
		t.Fatalf("ungated warmup should not be rejected: %v", err)
	}
	release()

	rt.enableHTTPWarmupGate()
	release, err = rt.beginHTTPWarmup()
	if err != nil {
		t.Fatalf("begin first HTTP warmup: %v", err)
	}

	_, err = rt.beginHTTPWarmup()
	if err == nil {
		t.Fatal("expected concurrent HTTP warmup to be rejected")
	}
	step, ok := err.(warmupStepError)
	if !ok || step.Code != "warmup_in_progress" {
		t.Fatalf("error = %#v, want warmup_in_progress step error", err)
	}

	release()
	release, err = rt.beginHTTPWarmup()
	if err != nil {
		t.Fatalf("begin warmup after release: %v", err)
	}
	release()
}

func validRootEntries(source *casc.CASCSource) map[uint32]bool {
	out := map[uint32]bool{}
	for _, fdid := range source.GetValidRootEntries() {
		out[fdid] = true
	}
	return out
}
