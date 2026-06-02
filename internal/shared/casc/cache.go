package casc

import "path/filepath"

type CachePaths struct {
	Root string `json:"root"`
}

func NewCachePaths(base string, buildKey string) CachePaths {
	return CachePaths{Root: filepath.ToSlash(filepath.Join(base, "casc", buildKey))}
}
