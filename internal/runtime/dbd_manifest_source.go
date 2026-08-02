package runtime

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"wowdata/internal/dbd"
	"wowdata/internal/storage"
)

type HTTPDBDManifestSource struct {
	cacheDir string
	urls     []string
	client   *http.Client
}

func NewHTTPDBDManifestSource(cacheDir string, urls []string) *HTTPDBDManifestSource {
	return &HTTPDBDManifestSource{cacheDir: cacheDir, urls: urls, client: http.DefaultClient}
}

func (s *HTTPDBDManifestSource) Manifest() (*dbd.Manifest, error) {
	cachePath := filepath.Join(s.cacheDir, "dbd-manifest.json")
	var lastErr error
	for _, url := range s.urls {
		resp, err := s.client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			continue
		}
		manifest, err := dbd.ParseManifest(strings.NewReader(string(body)))
		if err != nil {
			lastErr = err
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
			return nil, err
		}
		_ = storage.AtomicWriteFile(cachePath, body, 0o644)
		return manifest, nil
	}
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		return dbd.ParseManifest(strings.NewReader(string(data)))
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DBD manifest URLs configured")
	}
	return nil, lastErr
}
