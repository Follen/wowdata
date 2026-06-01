package runtime

import (
	"path/filepath"
	"strings"
)

type RemoteContextKey struct {
	Region    string
	Product   string
	BuildKey  string
	Locale    string
	CacheRoot string
}

func (k RemoteContextKey) String() string {
	return strings.Join([]string{"remote", k.Region, k.Product, k.BuildKey, k.Locale, cleanKeyPath(k.CacheRoot)}, "\x00")
}

type LocalContextKey struct {
	Path     string
	Product  string
	BuildKey string
	Locale   string
}

func (k LocalContextKey) String() string {
	return strings.Join([]string{"local", cleanKeyPath(k.Path), k.Product, k.BuildKey, k.Locale}, "\x00")
}

func cleanKeyPath(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return filepath.Clean(value)
}
