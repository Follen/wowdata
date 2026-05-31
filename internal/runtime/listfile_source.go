package runtime

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wowdata/internal/casc"
	"wowdata/internal/listfile"
)

type HTTPListfileSource struct {
	cacheDir       string
	urls           []string
	binaryURLTmpls []string
	client         *http.Client
	textOnly       bool
}

func NewHTTPListfileSource(cacheDir string, urls []string) *HTTPListfileSource {
	return &HTTPListfileSource{cacheDir: cacheDir, urls: urls, client: http.DefaultClient}
}

func (s *HTTPListfileSource) WithBinaryURLs(urls []string) *HTTPListfileSource {
	s.binaryURLTmpls = urls
	return s
}

func (s *HTTPListfileSource) WithTextOnly() *HTTPListfileSource {
	s.textOnly = true
	return s
}

func (s *HTTPListfileSource) Listfile() (*listfile.Listfile, error) {
	if s.textOnly {
		return s.textListfile()
	}
	if lf, err := s.binaryListfile(); err == nil {
		return lf, nil
	}
	return s.textListfile()
}

func (s *HTTPListfileSource) textListfile() (*listfile.Listfile, error) {
	cachePath := filepath.Join(s.cacheDir, "community-listfile.csv")
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		return parseListfile(data)
	}
	var lastErr error
	for _, url := range s.urls {
		body, err := downloadListfileURL(s.client, url)
		if err != nil {
			lastErr = err
			continue
		}
		lf, err := parseListfile(body)
		if err != nil {
			lastErr = err
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
			return nil, err
		}
		_ = os.WriteFile(cachePath, body, 0644)
		return lf, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no listfile URLs configured")
	}
	return nil, lastErr
}

func (s *HTTPListfileSource) binaryListfile() (*listfile.Listfile, error) {
	lf := listfile.New()
	if err := lf.LoadBinaryDir(s.cacheDir); err == nil {
		return lf, nil
	}
	if len(s.binaryURLTmpls) == 0 {
		return nil, fmt.Errorf("no binary listfile URLs configured")
	}
	components := []string{
		"listfile-id-index.dat",
		"listfile-strings.dat",
		"listfile-tree-nodes.dat",
		"listfile-pf-models.dat",
		"listfile-pf-textures.dat",
		"listfile-pf-sounds.dat",
		"listfile-pf-videos.dat",
		"listfile-pf-text.dat",
		"listfile-pf-fonts.dat",
	}
	jobs := make(chan string)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	workerCount := 4
	if len(components) < workerCount {
		workerCount = len(components)
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for component := range jobs {
				if err := s.downloadBinaryComponent(component); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}()
	}
	for _, component := range components {
		select {
		case err := <-errCh:
			close(jobs)
			wg.Wait()
			return nil, err
		case jobs <- component:
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errCh:
		return nil, err
	default:
	}
	lf = listfile.New()
	if err := lf.LoadBinaryDir(s.cacheDir); err != nil {
		return nil, err
	}
	return lf, nil
}

func (s *HTTPListfileSource) downloadBinaryComponent(component string) error {
	var lastErr error
	for _, tmpl := range s.binaryURLTmpls {
		url := fmt.Sprintf(tmpl, component)
		body, err := downloadListfileURL(s.client, url)
		if err != nil {
			lastErr = err
			continue
		}
		if err := os.MkdirAll(s.cacheDir, 0755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(s.cacheDir, component), body, 0644)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no binary listfile URLs configured")
	}
	return fmt.Errorf("download binary listfile component %s: %w", component, lastErr)
}

func downloadListfileURL(client *http.Client, url string) ([]byte, error) {
	if body, err := casc.DownloadHTTPConcurrent(url); err == nil {
		return body, nil
	}
	if client == nil {
		client = http.DefaultClient
	}
	startedAt := time.Now()
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	casc.ReportDownloadProgress(url, "get", len(body), 0, time.Since(startedAt))
	return body, nil
}

func parseListfile(data []byte) (*listfile.Listfile, error) {
	lf := listfile.New()
	if err := lf.Load(strings.NewReader(string(data))); err != nil {
		return nil, err
	}
	return lf, nil
}
