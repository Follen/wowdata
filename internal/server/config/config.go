package config

type Config struct {
	Server  ServerConfig
	Cache   CacheConfig
	Prepare PrepareConfig
	Limits  LimitsConfig
}

type ServerConfig struct {
	Host    string
	Port    int
	BaseURL string
}

type CacheConfig struct {
	Root       string
	MetadataDB string
	RawDir     string
	DB2Dir     string
	DuckDBPath string
}

type PrepareConfig struct {
	Targets []PrepareTarget
}

type PrepareTarget struct {
	Label   string
	Region  string
	Product string
	Locale  string
	Strict  bool
}

type LimitsConfig struct {
	MaxParallelContextPrepares       int
	MaxParallelTableMaterializations int
	MaxParallelDownloads             int
	MaxParallelQueries               int
	MemorySoftLimitMB                int
	MemoryHardLimitMB                int
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
		Prepare: PrepareConfig{Targets: defaultTargets()},
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
