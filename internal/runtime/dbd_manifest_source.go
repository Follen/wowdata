package runtime

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wowdata/internal/casc"
	"wowdata/internal/dbd"
	"wowdata/internal/resource"
	"wowdata/internal/storage"
)

type HTTPDBDManifestSource struct {
	cacheDir   string
	urls       []string
	client     *http.Client
	cacheFirst bool
}

func NewHTTPDBDManifestSource(cacheDir string, urls []string) *HTTPDBDManifestSource {
	return &HTTPDBDManifestSource{cacheDir: cacheDir, urls: urls, client: casc.InstrumentHTTPClient(http.DefaultClient)}
}

func (s *HTTPDBDManifestSource) WithCacheFirst() *HTTPDBDManifestSource {
	s.cacheFirst = true
	return s
}

func (s *HTTPDBDManifestSource) WithStage(stage resource.StageID) *HTTPDBDManifestSource {
	s.client = casc.InstrumentHTTPClientForStage(s.client, stage)
	return s
}

func (s *HTTPDBDManifestSource) Manifest() (*dbd.Manifest, error) {
	cachePath := filepath.Join(s.cacheDir, "dbd-manifest.json")
	if s.cacheFirst {
		if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
			return dbd.ParseManifest(strings.NewReader(string(data)))
		}
	}
	var lastErr error
	for _, url := range s.urls {
		for attempt := 0; attempt < 3; attempt++ {
			resp, err := s.client.Get(url)
			if err == nil {
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr == nil && resp.StatusCode == http.StatusOK {
					manifest, parseErr := dbd.ParseManifest(strings.NewReader(string(body)))
					if parseErr == nil {
						if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
							return nil, err
						}
						_ = storage.AtomicWriteFile(cachePath, body, 0o644)
						return manifest, nil
					}
					lastErr = parseErr
				} else if readErr != nil {
					lastErr = readErr
				} else {
					lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
				}
			} else {
				lastErr = err
			}
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
			}
		}
	}
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		return dbd.ParseManifest(strings.NewReader(string(data)))
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DBD manifest URLs configured")
	}
	return nil, lastErr
}
