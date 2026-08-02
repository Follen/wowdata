package casc

type SourceKind string

const (
	SourceLocal  SourceKind = "local"
	SourceRemote SourceKind = "remote"
)

func (k SourceKind) Valid() bool {
	return k == SourceLocal || k == SourceRemote
}

type Product struct {
	Label          string   `json:"label"`
	BuildIndex     int      `json:"buildIndex"`
	Product        string   `json:"product"`
	Region         string   `json:"region,omitempty"`
	Version        string   `json:"version,omitempty"`
	BuildID        string   `json:"buildId,omitempty"`
	BuildConfigKey string   `json:"buildConfigKey,omitempty"`
	CDNConfigKey   string   `json:"cdnConfigKey,omitempty"`
	Branch         string   `json:"branch,omitempty"`
	Locales        []string `json:"locales,omitempty"`
}

type Context struct {
	Source    SourceKind `json:"source"`
	Path      string     `json:"path,omitempty"`
	Region    string     `json:"region"`
	Product   string     `json:"product,omitempty"`
	BuildKey  string     `json:"buildKey,omitempty"`
	BuildName string     `json:"buildName,omitempty"`
}
