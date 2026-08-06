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
	"wowdata/internal/resource"
	"wowdata/internal/storage"
)

type HTTPListfileSource struct {
	cacheDir       string
	urls           []string
	binaryURLTmpls []string
	client         *http.Client
	textOnly       bool
	workers        int
}

func NewHTTPListfileSource(cacheDir string, urls []string) *HTTPListfileSource {
	return &HTTPListfileSource{cacheDir: cacheDir, urls: urls, client: casc.InstrumentHTTPClient(http.DefaultClient), workers: storage.DefaultWorkers}
}

func (s *HTTPListfileSource) WithBinaryURLs(urls []string) *HTTPListfileSource {
	s.binaryURLTmpls = urls
	return s
}

func (s *HTTPListfileSource) WithTextOnly() *HTTPListfileSource {
	s.textOnly = true
	return s
}

func (s *HTTPListfileSource) WithWorkers(workers int) *HTTPListfileSource {
	if workers > 0 {
		s.workers = workers
	}
	return s
}

func (s *HTTPListfileSource) Listfile() (*listfile.Listfile, error) {
	lf, _, err := s.ListfileWithStage(0)
	return lf, err
}

func (s *HTTPListfileSource) ListfileWithStage(parent resource.StageID) (*listfile.Listfile, resource.StageID, error) {
	if s.textOnly {
		return s.textListfileWithStage(parent, 0, resource.StageRelationHard, "text-only")
	}
	binaryStage := resource.StartStage("listfile-fetch", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveInitial, Condition: "binary", DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
	before := casc.SnapshotHTTPMetrics()
	if lf, err := s.binaryListfile(); err == nil {
		recordListfileStageNetwork(binaryStage, before)
		resource.FinishStage(binaryStage, nil)
		parseStage := resource.StartStage("listfile-parse", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveInitial, Condition: "binary", DependsOn: []resource.StageDependency{resource.Dependency(binaryStage, resource.StageRelationHard)}})
		resource.FinishStage(parseStage, nil)
		return lf, parseStage, nil
	} else {
		recordListfileStageNetwork(binaryStage, before)
		resource.FinishStage(binaryStage, err)
	}
	return s.textListfileWithStage(parent, binaryStage, resource.StageRelationFallback, "binary-failed")
}

func (s *HTTPListfileSource) textListfileWithStage(parent, dependency resource.StageID, relation, condition string) (*listfile.Listfile, resource.StageID, error) {
	if dependency == 0 {
		dependency = parent
	}
	fetchStage := resource.StartStage("listfile-fetch", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveInitial, Condition: condition, DependsOn: []resource.StageDependency{resource.Dependency(dependency, relation)}})
	before := casc.SnapshotHTTPMetrics()
	lf, err := s.textListfile()
	recordListfileStageNetwork(fetchStage, before)
	resource.FinishStage(fetchStage, err)
	if err != nil {
		return nil, fetchStage, err
	}
	parseStage := resource.StartStage("listfile-parse", resource.StageOptions{ParentID: parent, Wave: resource.StageWaveInitial, Condition: condition, DependsOn: []resource.StageDependency{resource.Dependency(fetchStage, resource.StageRelationHard)}})
	resource.FinishStage(parseStage, nil)
	return lf, parseStage, nil
}

func recordListfileStageNetwork(stage resource.StageID, before casc.HTTPMetrics) {
	after := casc.SnapshotHTTPMetrics()
	unique := after.UniquePayloadBytes - before.UniquePayloadBytes
	transferred := after.ResponseBytes - before.ResponseBytes
	if unique > 0 || transferred > 0 {
		resource.RecordStageNetwork(stage, uint64(max(unique, 0)), uint64(max(transferred, 0)))
	}
}

func (s *HTTPListfileSource) textListfile() (*listfile.Listfile, error) {
	cachePath := filepath.Join(s.cacheDir, "community-listfile.csv")
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		return parseListfile(data)
	}
	var lastErr error
	for _, url := range s.urls {
		body, err := downloadListfileURLWithWorkers(s.client, url, s.workers)
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
		_ = storage.AtomicWriteFile(cachePath, body, 0o644)
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
	workerCount := s.workers
	if workerCount <= 0 {
		workerCount = storage.DefaultWorkers
	}
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
		body, err := downloadListfileURLWithWorkers(s.client, url, s.workers)
		if err != nil {
			lastErr = err
			continue
		}
		if err := os.MkdirAll(s.cacheDir, 0755); err != nil {
			return err
		}
		return storage.AtomicWriteFile(filepath.Join(s.cacheDir, component), body, 0o644)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no binary listfile URLs configured")
	}
	return fmt.Errorf("download binary listfile component %s: %w", component, lastErr)
}

func downloadListfileURL(client *http.Client, url string) ([]byte, error) {
	return downloadListfileURLWithWorkers(client, url, storage.DefaultWorkers)
}

func downloadListfileURLWithWorkers(client *http.Client, url string, workers int) ([]byte, error) {
	if body, err := casc.DownloadHTTPConcurrentWithWorkers(url, workers); err == nil {
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
