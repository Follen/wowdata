package config

import (
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultPrepareMatrixHasTwentyThreeTargets(t *testing.T) {
	cfg := Default()
	targets := cfg.Prepare.Targets
	expected := expectedPrepareTargets()
	if len(targets) != len(expected) {
		t.Fatalf("targets = %d, want %d", len(targets), len(expected))
	}
	assertNoDuplicateTargets(t, targets)
	if !reflect.DeepEqual(targets, expected) {
		t.Fatalf("targets = %#v, want %#v", targets, expected)
	}
}

func TestDefaultResourceLimitsFitTenGBServer(t *testing.T) {
	limits := Default().Limits
	if limits.MaxParallelContextPrepares != 2 {
		t.Fatalf("context prepares = %d, want 2", limits.MaxParallelContextPrepares)
	}
	if limits.MaxParallelTableMaterializations != 2 {
		t.Fatalf("table materializations = %d, want 2", limits.MaxParallelTableMaterializations)
	}
	if limits.MaxParallelDownloads != 16 || limits.MaxParallelQueries != 16 {
		t.Fatalf("download/query limits = %d/%d, want 16/16", limits.MaxParallelDownloads, limits.MaxParallelQueries)
	}
	if limits.MemorySoftLimitMB != 4096 || limits.MemoryHardLimitMB != 8192 {
		t.Fatalf("memory limits = %d/%d, want 4096/8192", limits.MemorySoftLimitMB, limits.MemoryHardLimitMB)
	}
}

func TestConfigStructsHaveSnakeCaseYAMLTags(t *testing.T) {
	assertYAMLTags(t, reflect.TypeOf(Config{}), map[string]string{
		"Server":  "server",
		"Cache":   "cache",
		"Prepare": "prepare",
		"Limits":  "limits",
	})
	assertYAMLTags(t, reflect.TypeOf(ServerConfig{}), map[string]string{
		"Host":    "host",
		"Port":    "port",
		"BaseURL": "base_url",
	})
	assertYAMLTags(t, reflect.TypeOf(CacheConfig{}), map[string]string{
		"Root":       "root",
		"MetadataDB": "metadata_db",
		"RawDir":     "raw_dir",
		"DB2Dir":     "db2_dir",
		"DuckDBPath": "duckdb_path",
	})
	assertYAMLTags(t, reflect.TypeOf(PrepareConfig{}), map[string]string{
		"Targets": "targets",
	})
	assertYAMLTags(t, reflect.TypeOf(PrepareTarget{}), map[string]string{
		"Label":   "label",
		"Region":  "region",
		"Product": "product",
		"Locale":  "locale",
		"Strict":  "strict",
	})
	assertYAMLTags(t, reflect.TypeOf(LimitsConfig{}), map[string]string{
		"MaxParallelContextPrepares":       "max_parallel_context_prepares",
		"MaxParallelTableMaterializations": "max_parallel_table_materializations",
		"MaxParallelDownloads":             "max_parallel_downloads",
		"MaxParallelQueries":               "max_parallel_queries",
		"MemorySoftLimitMB":                "memory_soft_limit_mb",
		"MemoryHardLimitMB":                "memory_hard_limit_mb",
	})
}

func TestExampleYAMLDoesNotIncludeStaleContextPoolConfig(t *testing.T) {
	var raw map[string]any
	readExampleYAML(t, &raw)
	if _, ok := raw["contexts"]; ok {
		t.Fatal("example YAML includes stale contexts section before context pool config exists")
	}
}

func TestExampleYAMLPrepareTargetsMatchDefault(t *testing.T) {
	assertYAMLTags(t, reflect.TypeOf(Config{}), map[string]string{"Prepare": "prepare"})
	assertYAMLTags(t, reflect.TypeOf(PrepareConfig{}), map[string]string{"Targets": "targets"})

	var example struct {
		Prepare PrepareConfig `yaml:"prepare"`
	}
	readExampleYAML(t, &example)
	if !reflect.DeepEqual(example.Prepare.Targets, Default().Prepare.Targets) {
		t.Fatalf("example targets = %#v, want %#v", example.Prepare.Targets, Default().Prepare.Targets)
	}
}

func assertYAMLTags(t *testing.T, typ reflect.Type, fields map[string]string) {
	t.Helper()
	for name, want := range fields {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("%s.%s field not found", typ.Name(), name)
		}
		if got := field.Tag.Get("yaml"); got != want {
			t.Fatalf("%s.%s yaml tag = %q, want %q", typ.Name(), name, got, want)
		}
	}
}

func assertNoDuplicateTargets(t *testing.T, targets []PrepareTarget) {
	t.Helper()
	labels := map[string]bool{}
	keys := map[string]bool{}
	for _, target := range targets {
		if labels[target.Label] {
			t.Fatalf("duplicate target label %q", target.Label)
		}
		labels[target.Label] = true
		key := target.Region + "/" + target.Product + "/" + target.Locale
		if keys[key] {
			t.Fatalf("duplicate target tuple %q", key)
		}
		keys[key] = true
	}
}

func readExampleYAML(t *testing.T, out any) {
	t.Helper()
	data, err := os.ReadFile("../../../config/http-mcp.example.yaml")
	if err != nil {
		t.Fatalf("read example YAML: %v", err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		t.Fatalf("parse example YAML: %v", err)
	}
}

func expectedPrepareTargets() []PrepareTarget {
	return []PrepareTarget{
		{Label: "CN Retail", Region: "cn", Product: "wow", Locale: "zhCN"},
		{Label: "US Retail", Region: "us", Product: "wow", Locale: "enUS"},
		{Label: "EU Retail", Region: "eu", Product: "wow", Locale: "enUS"},
		{Label: "KR Retail", Region: "kr", Product: "wow", Locale: "koKR"},
		{Label: "TW Retail", Region: "tw", Product: "wow", Locale: "zhTW"},
		{Label: "CN PTR", Region: "cn", Product: "wowt", Locale: "zhCN"},
		{Label: "US PTR", Region: "us", Product: "wowt", Locale: "enUS"},
		{Label: "EU PTR", Region: "eu", Product: "wowt", Locale: "enUS"},
		{Label: "CN Classic", Region: "cn", Product: "wow_classic", Locale: "zhCN"},
		{Label: "US Classic", Region: "us", Product: "wow_classic", Locale: "enUS"},
		{Label: "EU Classic", Region: "eu", Product: "wow_classic", Locale: "enUS"},
		{Label: "KR Classic", Region: "kr", Product: "wow_classic", Locale: "koKR"},
		{Label: "TW Classic", Region: "tw", Product: "wow_classic", Locale: "zhTW"},
		{Label: "CN Classic Era", Region: "cn", Product: "wow_classic_era", Locale: "zhCN"},
		{Label: "US Classic Era", Region: "us", Product: "wow_classic_era", Locale: "enUS"},
		{Label: "EU Classic Era", Region: "eu", Product: "wow_classic_era", Locale: "enUS"},
		{Label: "KR Classic Era", Region: "kr", Product: "wow_classic_era", Locale: "koKR"},
		{Label: "TW Classic Era", Region: "tw", Product: "wow_classic_era", Locale: "zhTW"},
		{Label: "CN Classic Titan", Region: "cn", Product: "wow_classic_titan", Locale: "zhCN"},
		{Label: "US Classic Titan", Region: "us", Product: "wow_classic_titan", Locale: "enUS"},
		{Label: "EU Classic Titan", Region: "eu", Product: "wow_classic_titan", Locale: "enUS"},
		{Label: "KR Classic Titan", Region: "kr", Product: "wow_classic_titan", Locale: "koKR"},
		{Label: "TW Classic Titan", Region: "tw", Product: "wow_classic_titan", Locale: "zhTW"},
	}
}
