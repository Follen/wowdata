package config

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Cache     CacheConfig     `yaml:"cache"`
	Artifacts ArtifactsConfig `yaml:"artifacts"`
	Prepare   PrepareConfig   `yaml:"prepare"`
	Limits    LimitsConfig    `yaml:"limits"`
}

type ServerConfig struct {
	Host                 string `yaml:"host"`
	Port                 int    `yaml:"port"`
	BaseURL              string `yaml:"base_url"`
	EnableUpdateFixtures bool   `yaml:"enable_update_fixtures"`
}

type CacheConfig struct {
	Root             string `yaml:"root"`
	MetadataDB       string `yaml:"metadata_db"`
	RawDir           string `yaml:"raw_dir"`
	DB2Dir           string `yaml:"db2_dir"`
	DuckDBPath       string `yaml:"duckdb_path"`
	CASCDiskLimitMB  int64  `yaml:"casc_disk_limit_mb"`
	CASCDiskTargetMB int64  `yaml:"casc_disk_target_mb"`
}

type ArtifactsConfig struct {
	Root    string `yaml:"root"`
	BaseURL string `yaml:"base_url"`
}

type PrepareConfig struct {
	Targets       []PrepareTarget `yaml:"targets"`
	DefaultTables []string        `yaml:"default_tables"`
}

type PrepareTarget struct {
	Label   string `yaml:"label"`
	Region  string `yaml:"region"`
	Product string `yaml:"product"`
	Locale  string `yaml:"locale"`
	Strict  bool   `yaml:"strict"`
}

type LimitsConfig struct {
	MaxParallelContextPrepares       int `yaml:"max_parallel_context_prepares"`
	MaxParallelTableMaterializations int `yaml:"max_parallel_table_materializations"`
	MaxParallelDownloads             int `yaml:"max_parallel_downloads"`
	MaxParallelQueries               int `yaml:"max_parallel_queries"`
	MemorySoftLimitMB                int `yaml:"memory_soft_limit_mb"`
	MemoryHardLimitMB                int `yaml:"memory_hard_limit_mb"`
}

func (c *LimitsConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	existing := *c
	type rawLimits LimitsConfig
	var raw struct {
		rawLimits `yaml:",inline"`

		MaxConcurrentPrepares         *int `yaml:"max_concurrent_prepares"`
		MaxConcurrentMaterializations *int `yaml:"max_concurrent_materializations"`
		MaxConcurrentQueries          *int `yaml:"max_concurrent_queries"`
	}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	parsed := LimitsConfig(raw.rawLimits)
	*c = existing
	if parsed.MaxParallelContextPrepares != 0 {
		c.MaxParallelContextPrepares = parsed.MaxParallelContextPrepares
	}
	if parsed.MaxParallelTableMaterializations != 0 {
		c.MaxParallelTableMaterializations = parsed.MaxParallelTableMaterializations
	}
	if parsed.MaxParallelDownloads != 0 {
		c.MaxParallelDownloads = parsed.MaxParallelDownloads
	}
	if parsed.MaxParallelQueries != 0 {
		c.MaxParallelQueries = parsed.MaxParallelQueries
	}
	if parsed.MemorySoftLimitMB != 0 {
		c.MemorySoftLimitMB = parsed.MemorySoftLimitMB
	}
	if parsed.MemoryHardLimitMB != 0 {
		c.MemoryHardLimitMB = parsed.MemoryHardLimitMB
	}
	if raw.MaxConcurrentPrepares != nil {
		c.MaxParallelContextPrepares = *raw.MaxConcurrentPrepares
	}
	if raw.MaxConcurrentMaterializations != nil {
		c.MaxParallelTableMaterializations = *raw.MaxConcurrentMaterializations
	}
	if raw.MaxConcurrentQueries != nil {
		c.MaxParallelQueries = *raw.MaxConcurrentQueries
	}
	return nil
}

func Default() Config {
	return Config{
		Server: ServerConfig{Host: "0.0.0.0", Port: 9788},
		Cache: CacheConfig{
			Root:             "/var/lib/wowdata/cache",
			MetadataDB:       "/var/lib/wowdata/cache/metadata.sqlite",
			RawDir:           "/var/lib/wowdata/cache/raw",
			DB2Dir:           "/var/lib/wowdata/cache/db2",
			DuckDBPath:       "/var/lib/wowdata/cache/duckdb/wowdata.duckdb",
			CASCDiskLimitMB:  61440,
			CASCDiskTargetMB: 49152,
		},
		Artifacts: ArtifactsConfig{Root: "/var/lib/wowdata/artifacts"},
		Prepare:   PrepareConfig{Targets: defaultTargets(), DefaultTables: defaultTables()},
		Limits: LimitsConfig{
			MaxParallelContextPrepares:       2,
			MaxParallelTableMaterializations: 2,
			MaxParallelDownloads:             16,
			MaxParallelQueries:               16,
			MemorySoftLimitMB:                4096,
			MemoryHardLimitMB:                8192,
		},
	}
}

func defaultTables() []string {
	return []string{"*"}
}

func defaultTargets() []PrepareTarget {
	return []PrepareTarget{
		{Label: "CN Retail", Region: "cn", Product: "wow", Locale: "zhCN"},
		{Label: "CN Classic", Region: "cn", Product: "wow_classic", Locale: "zhCN"},
		{Label: "CN Classic Titan", Region: "cn", Product: "wow_classic_titan", Locale: "zhCN"},
		{Label: "CN Retail PTR zhCN", Region: "cn", Product: "wowt", Locale: "zhCN"},
		{Label: "CN Classic PTR zhCN", Region: "cn", Product: "wow_classic_ptr", Locale: "zhCN"},
	}
}
