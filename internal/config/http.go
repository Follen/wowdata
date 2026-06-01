package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type HTTPConfig struct {
	Server    HTTPServerConfig    `yaml:"server"`
	Defaults  HTTPDefaultsConfig  `yaml:"defaults"`
	Contexts  HTTPContextsConfig  `yaml:"contexts"`
	Cache     HTTPCacheConfig     `yaml:"cache"`
	Artifacts HTTPArtifactsConfig `yaml:"artifacts"`
	Prepare   HTTPPrepareConfig   `yaml:"prepare"`
	Refresh   HTTPRefreshConfig   `yaml:"refresh"`
	Limits    HTTPLimitsConfig    `yaml:"limits"`
	Tools     HTTPToolsConfig     `yaml:"tools"`
}

type HTTPServerConfig struct {
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"base_url"`
}

type HTTPDefaultsConfig struct {
	Region  string `yaml:"region"`
	Product string `yaml:"product"`
	Locale  string `yaml:"locale"`
}

type HTTPContextsConfig struct {
	MaxContexts int                 `yaml:"max_contexts"`
	Pinned      []HTTPPinnedContext `yaml:"pinned"`
}

type HTTPPinnedContext struct {
	Region  string `yaml:"region"`
	Product string `yaml:"product"`
	Locale  string `yaml:"locale"`
	Label   string `yaml:"label"`
}

type HTTPCacheConfig struct {
	Root       string `yaml:"root"`
	MetadataDB string `yaml:"metadata_db"`
	RawDir     string `yaml:"raw_dir"`
	DB2Dir     string `yaml:"db2_dir"`
	DuckDBPath string `yaml:"duckdb_path"`
}

type HTTPArtifactsConfig struct {
	Root           string `yaml:"root"`
	BaseURL        string `yaml:"base_url"`
	RetentionHours int    `yaml:"retention_hours"`
}

type HTTPPrepareConfig struct {
	Lazy           bool     `yaml:"lazy"`
	PrewarmOnStart bool     `yaml:"prewarm_on_start"`
	DBDManifest    bool     `yaml:"dbd_manifest"`
	Listfile       bool     `yaml:"listfile"`
	DefaultTables  []string `yaml:"default_tables"`
}

type HTTPRefreshConfig struct {
	ProductCheckIntervalMinutes int  `yaml:"product_check_interval_minutes"`
	AutoPrepareNewBuilds        bool `yaml:"auto_prepare_new_builds"`
	KeepBuildsPerProduct        int  `yaml:"keep_builds_per_product"`
	PruneOnStart                bool `yaml:"prune_on_start"`
	MaxCacheGB                  int  `yaml:"max_cache_gb"`
}

type HTTPLimitsConfig struct {
	MaxConcurrentPrepares         int `yaml:"max_concurrent_prepares"`
	MaxConcurrentMaterializations int `yaml:"max_concurrent_materializations"`
	MaxConcurrentQueries          int `yaml:"max_concurrent_queries"`
	RequestTimeoutSeconds         int `yaml:"request_timeout_seconds"`
	MaterializeTimeoutSeconds     int `yaml:"materialize_timeout_seconds"`
}

type HTTPToolsConfig struct {
	ExposeAdminTools bool `yaml:"expose_admin_tools"`
	ExposeDebugTools bool `yaml:"expose_debug_tools"`
}

func DefaultHTTPConfig() HTTPConfig {
	return HTTPConfig{
		Server:   HTTPServerConfig{Host: "127.0.0.1", Port: 9788},
		Defaults: HTTPDefaultsConfig{Region: "cn", Product: "wow", Locale: "zhCN"},
		Contexts: HTTPContextsConfig{MaxContexts: 4, Pinned: []HTTPPinnedContext{
			{Region: "cn", Product: "wow", Locale: "zhCN", Label: "CN Retail"},
			{Region: "cn", Product: "wowt", Locale: "zhCN", Label: "CN PTR"},
			{Region: "cn", Product: "wow_classic", Locale: "zhCN", Label: "CN Classic"},
			{Region: "cn", Product: "wow_classic_titan", Locale: "zhCN", Label: "CN Titan"},
		}},
		Cache: HTTPCacheConfig{
			Root:       "/opt/wowdata/cache",
			MetadataDB: "/opt/wowdata/cache/metadata.sqlite",
			RawDir:     "/opt/wowdata/cache/raw",
			DB2Dir:     "/opt/wowdata/cache/db2",
			DuckDBPath: "/opt/wowdata/cache/duckdb/wowdata.duckdb",
		},
		Artifacts: HTTPArtifactsConfig{Root: "/opt/wowdata/output", RetentionHours: 24},
		Prepare: HTTPPrepareConfig{Lazy: true, PrewarmOnStart: true, DBDManifest: true, DefaultTables: []string{
			"SpellName", "Spell", "SpellEffect", "SpellMisc", "SpellCastTimes", "SpellDuration", "SpellRange",
			"JournalEncounterSection",
			"Item", "ItemSparse", "ItemEffect", "ItemModifiedAppearance", "ItemAppearance", "ItemDisplayInfo", "ItemDisplayInfoMaterialRes",
			"ModelFileData", "TextureFileData", "ComponentModelFileData", "HelmetGeosetData",
			"CreatureDisplayInfo", "CreatureModelData", "CreatureDisplayInfoGeosetData",
			"HouseDecor",
		}},
		Refresh: HTTPRefreshConfig{ProductCheckIntervalMinutes: 30, AutoPrepareNewBuilds: true, KeepBuildsPerProduct: 2, PruneOnStart: true, MaxCacheGB: 80},
		Limits:  HTTPLimitsConfig{MaxConcurrentPrepares: 1, MaxConcurrentMaterializations: 2, MaxConcurrentQueries: 8, RequestTimeoutSeconds: 120, MaterializeTimeoutSeconds: 600},
	}
}

func LoadHTTPConfig(path string) (HTTPConfig, error) {
	cfg := DefaultHTTPConfig()
	if path == "" {
		return cfg, cfg.Validate()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func (c HTTPConfig) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if c.Defaults.Region == "" || c.Defaults.Product == "" || c.Defaults.Locale == "" {
		return fmt.Errorf("defaults.region, defaults.product, and defaults.locale are required")
	}
	if c.Contexts.MaxContexts < 1 {
		return fmt.Errorf("contexts.max_contexts must be at least 1")
	}
	if c.Cache.Root == "" {
		return fmt.Errorf("cache.root is required")
	}
	if c.Cache.MetadataDB == "" {
		return fmt.Errorf("cache.metadata_db is required")
	}
	if c.Cache.RawDir == "" {
		return fmt.Errorf("cache.raw_dir is required")
	}
	if c.Cache.DB2Dir == "" {
		return fmt.Errorf("cache.db2_dir is required")
	}
	if c.Cache.DuckDBPath == "" {
		return fmt.Errorf("cache.duckdb_path is required")
	}
	if c.Artifacts.Root == "" {
		return fmt.Errorf("artifacts.root is required")
	}
	if c.Artifacts.RetentionHours < 1 {
		return fmt.Errorf("artifacts.retention_hours must be at least 1")
	}
	if c.Refresh.ProductCheckIntervalMinutes < 1 {
		return fmt.Errorf("refresh.product_check_interval_minutes must be at least 1")
	}
	if c.Refresh.KeepBuildsPerProduct < 1 {
		return fmt.Errorf("refresh.keep_builds_per_product must be at least 1")
	}
	if c.Refresh.MaxCacheGB < 1 {
		return fmt.Errorf("refresh.max_cache_gb must be at least 1")
	}
	if c.Limits.MaxConcurrentPrepares < 1 || c.Limits.MaxConcurrentMaterializations < 1 || c.Limits.MaxConcurrentQueries < 1 {
		return fmt.Errorf("limits concurrency values must be at least 1")
	}
	if c.Limits.RequestTimeoutSeconds < 1 {
		return fmt.Errorf("limits.request_timeout_seconds must be at least 1")
	}
	if c.Limits.MaterializeTimeoutSeconds < 1 {
		return fmt.Errorf("limits.materialize_timeout_seconds must be at least 1")
	}
	return nil
}
