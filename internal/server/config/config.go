package config

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Cache     CacheConfig     `yaml:"cache"`
	Artifacts ArtifactsConfig `yaml:"artifacts"`
	Prepare   PrepareConfig   `yaml:"prepare"`
	Limits    LimitsConfig    `yaml:"limits"`
}

type ServerConfig struct {
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"base_url"`
}

type CacheConfig struct {
	Root       string `yaml:"root"`
	MetadataDB string `yaml:"metadata_db"`
	RawDir     string `yaml:"raw_dir"`
	DB2Dir     string `yaml:"db2_dir"`
	DuckDBPath string `yaml:"duckdb_path"`
}

type ArtifactsConfig struct {
	Root string `yaml:"root"`
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
			Root:       "/var/lib/wowdata/cache",
			MetadataDB: "/var/lib/wowdata/cache/metadata.sqlite",
			RawDir:     "/var/lib/wowdata/cache/raw",
			DB2Dir:     "/var/lib/wowdata/cache/db2",
			DuckDBPath: "/var/lib/wowdata/cache/duckdb/wowdata.duckdb",
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
	return []string{
		"SpellName",
		"Spell",
		"SpellEffect",
		"SpellMisc",
		"Item",
		"ItemSparse",
		"ItemEffect",
		"ItemModifiedAppearance",
		"ItemAppearance",
		"ItemDisplayInfo",
		"TextureFileData",
		"ModelFileData",
		"CreatureDisplayInfo",
		"CreatureModelData",
		"HouseDecor",
	}
}

func defaultTargets() []PrepareTarget {
	return []PrepareTarget{
		{Label: "CN Retail", Region: "cn", Product: "wow", Locale: "zhCN"},
		{Label: "US Retail", Region: "us", Product: "wow", Locale: "enUS"},
		{Label: "EU Retail", Region: "eu", Product: "wow", Locale: "enUS"},
		{Label: "KR Retail", Region: "kr", Product: "wow", Locale: "koKR"},
		{Label: "TW Retail", Region: "tw", Product: "wow", Locale: "zhTW"},

		{Label: "CN PTR", Region: "cn", Product: "wowt", Locale: "zhCN"},
		{Label: "US PTR", Region: "us", Product: "wowt", Locale: "enUS"},
		{Label: "EU PTR", Region: "eu", Product: "wowt", Locale: "enUS"},

		{Label: "US Beta", Region: "us", Product: "wowxptr", Locale: "enUS"},
		{Label: "EU Beta", Region: "eu", Product: "wowxptr", Locale: "enUS"},
		{Label: "KR Beta", Region: "kr", Product: "wowxptr", Locale: "koKR"},
		{Label: "TW Beta", Region: "tw", Product: "wowxptr", Locale: "zhTW"},

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
	}
}
