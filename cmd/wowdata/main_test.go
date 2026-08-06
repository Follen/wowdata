package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"wowdata/internal/app"
	"wowdata/internal/casc"
	"wowdata/internal/resource"
	"wowdata/internal/storage"
)

func TestRunCLIEmitsResourceMetricsWithoutCASC(t *testing.T) {
	t.Setenv("WOWDATA_HOME", filepath.Join(t.TempDir(), ".wowdata"))
	t.Setenv("WOWDATA_TIMING", "1")
	var stdout, stderr bytes.Buffer
	if exitCode := runCLI([]string{"profile", "list"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	const prefix = "resource metrics="
	var metrics resource.MetricsSnapshot
	for _, line := range strings.Split(stderr.String(), "\n") {
		if strings.HasPrefix(line, prefix) {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &metrics); err != nil {
				t.Fatal(err)
			}
		}
	}
	if metrics.Schema != resource.MetricsSchema || metrics.Work.Counters == nil || !metrics.Work.Complete || len(metrics.Work.UncoveredClasses) != 0 {
		t.Fatalf("resource metrics missing without CASC: %s", stderr.String())
	}
}

func TestRunCLIReturnsNonZeroForStructuredCommandError(t *testing.T) {
	t.Setenv("WOWDATA_HOME", filepath.Join(t.TempDir(), ".wowdata"))
	var stdout, stderr bytes.Buffer

	exitCode := runCLI([]string{"db2", "rows", "SpellName", "--id", "133"}, &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should not duplicate a structured error: %q", stderr.String())
	}
	var resp app.Response
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
	}
	if resp.OK || resp.Error == nil || resp.Error.Code != "target_required" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestRunCLIReturnsZeroForSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := runCLI([]string{"--version"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", exitCode, stderr.String())
	}
	if stdout.String() == "" {
		t.Fatal("version output is empty")
	}
}

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

func TestResolveCacheRootDefaultsToWowdataHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".wowdata")
	t.Setenv("WOWDATA_HOME", home)

	got := resolveCacheRoot("", func() (string, error) {
		return filepath.Join(t.TempDir(), "wowdata.exe"), nil
	})

	want := filepath.Join(home, "cache")
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

func TestSwitchToRemoteFallbackRequiresRegion(t *testing.T) {
	effectiveSource := "local"
	opts := warmupOptions{Source: "local", Path: `D:\Game\World of Warcraft`, Product: "wow", Build: "latest", Locale: "zhCN"}
	target := storage.Target{Source: opts.Source, Path: opts.Path, Product: opts.Product, Build: opts.Build, Locale: opts.Locale}
	result := map[string]interface{}{}

	err := switchToRemoteFallback(&effectiveSource, &opts, &target, result, "local_unavailable")
	var stepErr warmupStepError
	if !errors.As(err, &stepErr) || stepErr.Code != "target_required" {
		t.Fatalf("error = %#v, want target_required", err)
	}
	if effectiveSource != "local" || opts.Source != "local" || target.Source != "local" {
		t.Fatalf("failed fallback mutated source: effective=%q opts=%q target=%q", effectiveSource, opts.Source, target.Source)
	}
}

func TestSwitchToRemoteFallbackPublishesReason(t *testing.T) {
	effectiveSource := "local"
	opts := warmupOptions{Source: "local", Path: `D:\Game\World of Warcraft`, Region: "cn", Product: "wow", Build: "latest", Locale: "zhCN"}
	target := storage.Target{Source: opts.Source, Path: opts.Path, Region: opts.Region, Product: opts.Product, Build: opts.Build, Locale: opts.Locale}
	result := map[string]interface{}{}

	if err := switchToRemoteFallback(&effectiveSource, &opts, &target, result, "local_unavailable"); err != nil {
		t.Fatal(err)
	}
	if effectiveSource != "remote" || opts.Source != "remote" || opts.Path != "" || target.Source != "remote" || target.Path != "" {
		t.Fatalf("fallback target = effective=%q opts=%#v target=%#v", effectiveSource, opts, target)
	}
	if result["source"] != "remote" || result["fallback"] != "local_unavailable" {
		t.Fatalf("fallback result = %#v", result)
	}
}

func TestMetadataOnlyLocalFailureUsesAutoFallbackBoundary(t *testing.T) {
	dir := t.TempDir()
	buildKey := "00112233445566778899aabbccddeeff"
	cdnKey := "ffeeddccbbaa99887766554433221100"
	buildInfo := "Product!STRING:0|Branch!STRING:0|Version!STRING:0|Build Key!HEX:16|CDN Key!HEX:16\n" +
		"wow|retail|12.0.7.68974|" + buildKey + "|" + cdnKey + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".build.info"), []byte(buildInfo), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	opts := warmupOptions{
		Source: "local", Path: dir, Product: "wow", Build: "12.0.7.68974", Locale: "zhCN",
		AutoSource: true, MetadataOnly: true, CacheRoot: t.TempDir(),
	}
	_, err := rt.initialize(opts)
	var stepErr warmupStepError
	if !errors.As(err, &stepErr) || stepErr.Code != "target_required" {
		t.Fatalf("error = %#v, want fallback target_required", err)
	}
	if rt.CASC != nil {
		t.Fatal("remote source initialized before fallback target validation")
	}
}

func TestResourceExperimentOverridesAreIndependent(t *testing.T) {
	t.Setenv("WOWDATA_METADATA_WORKERS", "24")
	t.Setenv("WOWDATA_LARGE_RANGE_WORKERS", "7")
	t.Setenv("WOWDATA_RANGE_CHUNK_MIB", "4")
	t.Setenv("WOWDATA_ARCHIVE_TAIL_KIB", "32")
	remote := casc.NewCASCRemote("cn")
	if err := applyResourceExperimentOverrides(remote); err != nil {
		t.Fatal(err)
	}
	plan := remote.ResourcePlan()
	if plan.MetadataWorkers != 24 || plan.LargeRangeWorkers != 7 {
		t.Fatalf("network pools = metadata %d range %d", plan.MetadataWorkers, plan.LargeRangeWorkers)
	}
	if remote.RangeChunkSize != 4*resource.MiB {
		t.Fatalf("range chunk size = %d", remote.RangeChunkSize)
	}
	if remote.ArchiveTailProbe != 32<<10 {
		t.Fatalf("archive tail probe = %d", remote.ArchiveTailProbe)
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
	source.RootEntries[100] = []casc.RootEntry{{TypeIndex: 0, ContentKey: "en-us"}}
	source.RootEntries[200] = []casc.RootEntry{{TypeIndex: 1, ContentKey: "zh-cn"}}
	source.RootEntries[300] = []casc.RootEntry{{TypeIndex: 2, ContentKey: "low-violence"}}

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

func validRootEntries(source *casc.CASCSource) map[uint32]bool {
	out := map[uint32]bool{}
	for _, fdid := range source.GetValidRootEntries() {
		out[fdid] = true
	}
	return out
}
