package runtime

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"wowdata/internal/casc"
	"wowdata/internal/resource"
	"wowdata/internal/storage"
)

type HTTPDBDSource struct {
	cacheDir   string
	urls       []string
	client     *http.Client
	cacheFirst bool
}

func NewHTTPDBDSource(cacheDir string, urls []string) *HTTPDBDSource {
	return &HTTPDBDSource{cacheDir: cacheDir, urls: urls, client: casc.InstrumentHTTPClient(http.DefaultClient)}
}

func (s *HTTPDBDSource) WithCacheFirst() *HTTPDBDSource {
	s.cacheFirst = true
	return s
}

func (s *HTTPDBDSource) Definition(tableName string) (string, error) {
	return s.definitionWithClient(tableName, s.client)
}

func (s *HTTPDBDSource) DefinitionWithStage(tableName string, stage resource.StageID) (string, error) {
	return s.definitionWithClient(tableName, casc.InstrumentHTTPClientForStage(s.client, stage))
}

func (s *HTTPDBDSource) definitionWithClient(tableName string, client *http.Client) (string, error) {
	cachePath := filepath.Join(s.cacheDir, tableName+".dbd")
	if s.cacheFirst {
		if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
			return string(data), nil
		}
	}
	var lastErr error
	for _, tmpl := range s.urls {
		url := fmt.Sprintf(tmpl, tableName)
		for attempt := 0; attempt < 3; attempt++ {
			resp, err := client.Get(url)
			if err == nil {
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr == nil && resp.StatusCode == http.StatusOK {
					if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
						return "", err
					}
					_ = storage.AtomicWriteFile(cachePath, body, 0o644)
					return string(body), nil
				}
				if readErr != nil {
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
		return string(data), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DBD URLs configured")
	}
	return "", lastErr
}
