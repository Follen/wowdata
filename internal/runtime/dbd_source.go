package runtime

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type HTTPDBDSource struct {
	cacheDir string
	urls     []string
	client   *http.Client
}

func NewHTTPDBDSource(cacheDir string, urls []string) *HTTPDBDSource {
	return &HTTPDBDSource{cacheDir: cacheDir, urls: urls, client: http.DefaultClient}
}

func (s *HTTPDBDSource) Definition(tableName string) (string, error) {
	cachePath := filepath.Join(s.cacheDir, tableName+".dbd")
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		return string(data), nil
	}
	var lastErr error
	for _, tmpl := range s.urls {
		url := fmt.Sprintf(tmpl, tableName)
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
		if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
			return "", err
		}
		_ = os.WriteFile(cachePath, body, 0644)
		return string(body), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no DBD URLs configured")
	}
	return "", lastErr
}
