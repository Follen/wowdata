package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultHTTPConfig(t *testing.T) {
	cfg := DefaultHTTPConfig()
	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 9788 {
		t.Fatalf("server default = %#v", cfg.Server)
	}
	if cfg.Defaults.Region != "cn" || cfg.Defaults.Product != "wow" || cfg.Defaults.Locale != "zhCN" {
		t.Fatalf("defaults = %#v", cfg.Defaults)
	}
	if cfg.Tools.ExposeAdminTools {
		t.Fatal("admin tools must be disabled by default")
	}
}

func TestDefaultHTTPConfigPinnedContexts(t *testing.T) {
	cfg := DefaultHTTPConfig()
	want := []HTTPPinnedContext{
		{Region: "cn", Product: "wow", Locale: "zhCN", Label: "CN Retail"},
		{Region: "cn", Product: "wowt", Locale: "zhCN", Label: "CN PTR"},
		{Region: "cn", Product: "wow_classic", Locale: "zhCN", Label: "CN Classic"},
		{Region: "cn", Product: "wow_classic_titan", Locale: "zhCN", Label: "CN Titan"},
	}
	if len(cfg.Contexts.Pinned) != len(want) {
		t.Fatalf("pinned contexts = %#v, want exactly %#v", cfg.Contexts.Pinned, want)
	}
	for i := range want {
		if cfg.Contexts.Pinned[i] != want[i] {
			t.Fatalf("pinned[%d] = %#v, want %#v", i, cfg.Contexts.Pinned[i], want[i])
		}
		if cfg.Contexts.Pinned[i].Product == "wow_classic_era" {
			t.Fatal("wow_classic_era must not be pinned by default")
		}
	}
}

func TestLoadHTTPConfigFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "http-mcp.yaml")
	data := []byte("server:\n  host: 0.0.0.0\n  port: 9999\ncontexts:\n  max_contexts: 2\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadHTTPConfig(path)
	if err != nil {
		t.Fatalf("LoadHTTPConfig: %v", err)
	}
	if cfg.Server.Host != "0.0.0.0" || cfg.Server.Port != 9999 || cfg.Contexts.MaxContexts != 2 {
		t.Fatalf("cfg = %#v", cfg)
	}
	if cfg.Defaults.Locale != "zhCN" {
		t.Fatalf("default locale not retained: %#v", cfg.Defaults)
	}
	if len(cfg.Contexts.Pinned) != 4 {
		t.Fatalf("default pinned contexts not retained: %#v", cfg.Contexts.Pinned)
	}
	if len(cfg.Prepare.DefaultTables) == 0 {
		t.Fatal("default prepare tables not retained")
	}
	if !containsString(cfg.Prepare.DefaultTables, "SpellName") || !containsString(cfg.Prepare.DefaultTables, "ItemSparse") {
		t.Fatalf("default prepare tables missing expected values: %#v", cfg.Prepare.DefaultTables)
	}
}

func TestHTTPConfigRejectsInvalidPort(t *testing.T) {
	cfg := DefaultHTTPConfig()
	cfg.Server.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted port 0")
	}
}

func TestHTTPConfigRejectsMissingDefaultsAndZeroConcurrency(t *testing.T) {
	cfg := DefaultHTTPConfig()
	cfg.Defaults = HTTPDefaultsConfig{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted missing defaults")
	}

	cfg = DefaultHTTPConfig()
	cfg.Limits.MaxConcurrentQueries = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted zero concurrency")
	}
}

func TestHTTPConfigRejectsRuntimeInvalidZeroAndEmptySettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*HTTPConfig)
	}{
		{
			name: "request timeout zero",
			mutate: func(cfg *HTTPConfig) {
				cfg.Limits.RequestTimeoutSeconds = 0
			},
		},
		{
			name: "materialize timeout zero",
			mutate: func(cfg *HTTPConfig) {
				cfg.Limits.MaterializeTimeoutSeconds = 0
			},
		},
		{
			name: "refresh product check interval zero",
			mutate: func(cfg *HTTPConfig) {
				cfg.Refresh.ProductCheckIntervalMinutes = 0
			},
		},
		{
			name: "refresh keep builds zero",
			mutate: func(cfg *HTTPConfig) {
				cfg.Refresh.KeepBuildsPerProduct = 0
			},
		},
		{
			name: "refresh max cache zero",
			mutate: func(cfg *HTTPConfig) {
				cfg.Refresh.MaxCacheGB = 0
			},
		},
		{
			name: "artifact retention zero",
			mutate: func(cfg *HTTPConfig) {
				cfg.Artifacts.RetentionHours = 0
			},
		},
		{
			name: "cache root empty",
			mutate: func(cfg *HTTPConfig) {
				cfg.Cache.Root = ""
			},
		},
		{
			name: "cache metadata db empty",
			mutate: func(cfg *HTTPConfig) {
				cfg.Cache.MetadataDB = ""
			},
		},
		{
			name: "cache raw dir empty",
			mutate: func(cfg *HTTPConfig) {
				cfg.Cache.RawDir = ""
			},
		},
		{
			name: "cache db2 dir empty",
			mutate: func(cfg *HTTPConfig) {
				cfg.Cache.DB2Dir = ""
			},
		},
		{
			name: "cache duckdb path empty",
			mutate: func(cfg *HTTPConfig) {
				cfg.Cache.DuckDBPath = ""
			},
		},
		{
			name: "artifacts root empty",
			mutate: func(cfg *HTTPConfig) {
				cfg.Artifacts.Root = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultHTTPConfig()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate accepted invalid config")
			}
		})
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
