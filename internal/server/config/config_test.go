package config

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultPrepareMatrixHasRequestedFiveCNZhCNTargetsAndExcludesExtras(t *testing.T) {
	cfg := Default()
	targets := cfg.Prepare.Targets
	expected := expectedPrepareTargets()
	if len(targets) != len(expected) {
		t.Fatalf("targets = %d, want %d", len(targets), len(expected))
	}
	assertNoDuplicateTargets(t, targets)
	assertNoUnsupportedNonCNTitan(t, targets)
	assertNoBetaTargets(t, targets)
	assertNoClassicEraTargets(t, targets)
	assertOnlyCNCDNTargets(t, targets)
	assertOnlyZhCNTargets(t, targets)
	if t.Failed() {
		return
	}
	if !reflect.DeepEqual(targets, expected) {
		t.Fatalf("targets = %#v, want %#v", targets, expected)
	}
}

func TestSpecDocumentsRequestedFiveTargetDefaultPrepareMatrix(t *testing.T) {
	data, err := os.ReadFile("../../../docs/superpowers/specs/2026-06-02-wowdata-http-mcp-optimal-server-design.md")
	if err != nil {
		t.Fatalf("read design spec: %v", err)
	}
	spec := string(data)
	if !strings.Contains(spec, "Total default prepare targets: 5.") {
		t.Fatal("design spec must document 5 default prepare targets")
	}
	matrixStart := strings.Index(spec, "The server discovers and prepares the latest configured builds for this default matrix:")
	if matrixStart < 0 {
		t.Fatal("design spec default prepare matrix section not found")
	}
	codeStart := strings.Index(spec[matrixStart:], "```text")
	if codeStart < 0 {
		t.Fatal("design spec default prepare matrix code block not found")
	}
	codeStart += matrixStart
	codeEnd := strings.Index(spec[codeStart+len("```text"):], "```")
	if codeEnd < 0 {
		t.Fatal("design spec default prepare matrix code block is unterminated")
	}
	matrix := spec[codeStart : codeStart+len("```text")+codeEnd]
	if strings.Contains(matrix, "Beta:\n") || strings.Contains(matrix, "wowxptr") {
		t.Fatal("design spec must not include Beta/wowxptr in the default prepare matrix")
	}
	for _, forbidden := range []string{"US /", "EU", "KR", "TW", "Classic Era"} {
		if strings.Contains(matrix, forbidden) {
			t.Fatalf("design spec matrix still includes unrequested target marker %q", forbidden)
		}
	}
	if strings.Contains(matrix, "enUS") {
		t.Fatal("design spec matrix must not include enUS in the default prepare matrix")
	}
	for _, required := range []string{
		"Retail zhCN",
		"Classic zhCN",
		"Classic Titan zhCN",
		"Retail PTR zhCN",
		"Classic PTR zhCN",
	} {
		if !strings.Contains(matrix, required) {
			t.Fatalf("design spec matrix missing required target %q", required)
		}
	}
}

func TestSpecDocumentsIncrementalRefreshAndLayeredCacheManagement(t *testing.T) {
	data, err := os.ReadFile("../../../docs/superpowers/specs/2026-06-02-wowdata-http-mcp-optimal-server-design.md")
	if err != nil {
		t.Fatalf("read design spec: %v", err)
	}
	spec := string(data)
	for _, required := range []string{
		"DB2 fingerprint",
		"encoding key",
		"Only DB2 tables whose fingerprint changes are marked dirty",
		"completion cleanup",
		"DB2/listfile",
		"WAL checkpoint",
		"storage usage by layer",
	} {
		if !strings.Contains(spec, required) {
			t.Fatalf("design spec missing incremental/cache requirement %q", required)
		}
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

func TestDefaultCASCDiskCacheLimitPrunesToLowerTarget(t *testing.T) {
	cache := Default().Cache
	if cache.CASCDiskLimitMB != 61440 {
		t.Fatalf("CASC disk limit MB = %d, want 61440", cache.CASCDiskLimitMB)
	}
	if cache.CASCDiskTargetMB != 49152 {
		t.Fatalf("CASC disk target MB = %d, want 49152", cache.CASCDiskTargetMB)
	}
	if cache.CASCDiskTargetMB >= cache.CASCDiskLimitMB {
		t.Fatalf("CASC disk target MB must be below limit: target=%d limit=%d", cache.CASCDiskTargetMB, cache.CASCDiskLimitMB)
	}
}

func TestLimitsAcceptLegacyRemoteYAMLNames(t *testing.T) {
	var cfg Config
	cfg.Limits = LimitsConfig{MemorySoftLimitMB: 4096, MemoryHardLimitMB: 8192}
	if err := yaml.Unmarshal([]byte(`
limits:
  max_concurrent_prepares: 3
  max_concurrent_materializations: 4
  max_concurrent_queries: 5
`), &cfg); err != nil {
		t.Fatalf("unmarshal legacy limits: %v", err)
	}
	if cfg.Limits.MaxParallelContextPrepares != 3 {
		t.Fatalf("context prepares = %d, want 3 from max_concurrent_prepares", cfg.Limits.MaxParallelContextPrepares)
	}
	if cfg.Limits.MaxParallelTableMaterializations != 4 {
		t.Fatalf("table materializations = %d, want 4 from max_concurrent_materializations", cfg.Limits.MaxParallelTableMaterializations)
	}
	if cfg.Limits.MaxParallelQueries != 5 {
		t.Fatalf("queries = %d, want 5 from max_concurrent_queries", cfg.Limits.MaxParallelQueries)
	}
	if cfg.Limits.MemorySoftLimitMB != 4096 || cfg.Limits.MemoryHardLimitMB != 8192 {
		t.Fatalf("memory limits = %d/%d, want preserved 4096/8192", cfg.Limits.MemorySoftLimitMB, cfg.Limits.MemoryHardLimitMB)
	}
}

func TestLimitsLegacyRemoteYAMLNamesOverrideDefaults(t *testing.T) {
	cfg := Default()
	if err := yaml.Unmarshal([]byte(`
limits:
  max_concurrent_prepares: 1
  max_concurrent_materializations: 3
  max_concurrent_queries: 8
`), &cfg); err != nil {
		t.Fatalf("unmarshal legacy limits into defaults: %v", err)
	}
	if cfg.Limits.MaxParallelContextPrepares != 1 {
		t.Fatalf("context prepares = %d, want 1 from max_concurrent_prepares", cfg.Limits.MaxParallelContextPrepares)
	}
	if cfg.Limits.MaxParallelTableMaterializations != 3 {
		t.Fatalf("table materializations = %d, want 3 from max_concurrent_materializations", cfg.Limits.MaxParallelTableMaterializations)
	}
	if cfg.Limits.MaxParallelQueries != 8 {
		t.Fatalf("queries = %d, want 8 from max_concurrent_queries", cfg.Limits.MaxParallelQueries)
	}
	if cfg.Limits.MemorySoftLimitMB != 4096 || cfg.Limits.MemoryHardLimitMB != 8192 {
		t.Fatalf("memory limits = %d/%d, want preserved defaults 4096/8192", cfg.Limits.MemorySoftLimitMB, cfg.Limits.MemoryHardLimitMB)
	}
}

func TestDefaultPrepareDefaultTablesUseFullManifestSentinel(t *testing.T) {
	tables := Default().Prepare.DefaultTables
	if !reflect.DeepEqual(tables, []string{"*"}) {
		t.Fatalf("Default().Prepare.DefaultTables = %#v, want full manifest sentinel", tables)
	}
}

func TestConfigStructsHaveSnakeCaseYAMLTags(t *testing.T) {
	assertYAMLTags(t, reflect.TypeOf(Config{}), map[string]string{
		"Server":    "server",
		"Cache":     "cache",
		"Artifacts": "artifacts",
		"Prepare":   "prepare",
		"Limits":    "limits",
	})
	assertYAMLTags(t, reflect.TypeOf(ServerConfig{}), map[string]string{
		"Host":                 "host",
		"Port":                 "port",
		"BaseURL":              "base_url",
		"EnableUpdateFixtures": "enable_update_fixtures",
	})
	assertYAMLTags(t, reflect.TypeOf(CacheConfig{}), map[string]string{
		"Root":             "root",
		"MetadataDB":       "metadata_db",
		"RawDir":           "raw_dir",
		"DB2Dir":           "db2_dir",
		"DuckDBPath":       "duckdb_path",
		"CASCDiskLimitMB":  "casc_disk_limit_mb",
		"CASCDiskTargetMB": "casc_disk_target_mb",
	})
	assertYAMLTags(t, reflect.TypeOf(ArtifactsConfig{}), map[string]string{
		"Root":    "root",
		"BaseURL": "base_url",
	})
	assertYAMLTags(t, reflect.TypeOf(PrepareConfig{}), map[string]string{
		"Targets":       "targets",
		"DefaultTables": "default_tables",
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

func TestExampleYAMLPrepareDefaultTablesMatchDefault(t *testing.T) {
	var example struct {
		Prepare PrepareConfig `yaml:"prepare"`
	}
	readExampleYAML(t, &example)
	if !reflect.DeepEqual(example.Prepare.DefaultTables, Default().Prepare.DefaultTables) {
		t.Fatalf("example default tables = %#v, want %#v", example.Prepare.DefaultTables, Default().Prepare.DefaultTables)
	}
}

func TestDeploymentYAMLConfigsAreSplitForRemoteAndWSL(t *testing.T) {
	remote := readDeploymentYAML(t, "../../../config/http-mcp.remote.yaml")
	if remote.Server.BaseURL != "http://211.154.18.253:11223/wowdata" {
		t.Fatalf("remote base_url = %q", remote.Server.BaseURL)
	}
	if !reflect.DeepEqual(remote.Prepare.Targets, Default().Prepare.Targets) {
		t.Fatalf("remote targets = %#v, want default targets", remote.Prepare.Targets)
	}
	if !reflect.DeepEqual(remote.Prepare.DefaultTables, []string{"*"}) {
		t.Fatalf("remote default_tables = %#v, want full manifest sentinel", remote.Prepare.DefaultTables)
	}

	wsl := readDeploymentYAML(t, "../../../config/http-mcp.wsl.yaml")
	if wsl.Server.BaseURL != "http://127.0.0.1:19788" {
		t.Fatalf("wsl base_url = %q", wsl.Server.BaseURL)
	}
	if !reflect.DeepEqual(wsl.Prepare.Targets, Default().Prepare.Targets) {
		t.Fatalf("wsl targets = %#v, want default targets", wsl.Prepare.Targets)
	}
	if wsl.Limits.MaxParallelContextPrepares != 5 {
		t.Fatalf("wsl context prepares = %d, want 5", wsl.Limits.MaxParallelContextPrepares)
	}
	if wsl.Limits.MaxParallelTableMaterializations != 16 {
		t.Fatalf("wsl table materializations = %d, want 16", wsl.Limits.MaxParallelTableMaterializations)
	}
	if wsl.Limits.MaxParallelQueries != 64 {
		t.Fatalf("wsl queries = %d, want 64", wsl.Limits.MaxParallelQueries)
	}
	if !reflect.DeepEqual(wsl.Prepare.DefaultTables, []string{"*"}) {
		t.Fatalf("wsl default_tables = %#v, want full manifest sentinel", wsl.Prepare.DefaultTables)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func assertNoUnsupportedNonCNTitan(t *testing.T, targets []PrepareTarget) {
	t.Helper()
	for _, target := range targets {
		if target.Product == "wow_classic_titan" && target.Region != "cn" {
			t.Errorf("unsupported non-CN Classic Titan target remains: %s/%s/%s (%s)", target.Region, target.Product, target.Locale, target.Label)
		}
	}
}

func assertNoBetaTargets(t *testing.T, targets []PrepareTarget) {
	t.Helper()
	for _, target := range targets {
		if target.Product == "wowxptr" {
			t.Errorf("Beta target should not be in default prepare for now: %s/%s/%s (%s)", target.Region, target.Product, target.Locale, target.Label)
		}
	}
}

func assertNoClassicEraTargets(t *testing.T, targets []PrepareTarget) {
	t.Helper()
	for _, target := range targets {
		if target.Product == "wow_classic_era" {
			t.Errorf("Classic Era target should not be in default prepare: %s/%s/%s (%s)", target.Region, target.Product, target.Locale, target.Label)
		}
	}
}

func assertOnlyCNCDNTargets(t *testing.T, targets []PrepareTarget) {
	t.Helper()
	for _, target := range targets {
		if target.Region != "cn" {
			t.Errorf("target must use CN CDN region: %s/%s/%s (%s)", target.Region, target.Product, target.Locale, target.Label)
		}
	}
}

func assertOnlyZhCNTargets(t *testing.T, targets []PrepareTarget) {
	t.Helper()
	for _, target := range targets {
		if target.Locale != "zhCN" {
			t.Errorf("target must use zhCN locale: %s/%s/%s (%s)", target.Region, target.Product, target.Locale, target.Label)
		}
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

func readDeploymentYAML(t *testing.T, path string) Config {
	t.Helper()
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read deployment YAML %s: %v", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse deployment YAML %s: %v", path, err)
	}
	return cfg
}

func expectedPrepareTargets() []PrepareTarget {
	return []PrepareTarget{
		{Label: "CN Retail", Region: "cn", Product: "wow", Locale: "zhCN"},
		{Label: "CN Classic", Region: "cn", Product: "wow_classic", Locale: "zhCN"},
		{Label: "CN Classic Titan", Region: "cn", Product: "wow_classic_titan", Locale: "zhCN"},
		{Label: "CN Retail PTR zhCN", Region: "cn", Product: "wowt", Locale: "zhCN"},
		{Label: "CN Classic PTR zhCN", Region: "cn", Product: "wow_classic_ptr", Locale: "zhCN"},
	}
}
