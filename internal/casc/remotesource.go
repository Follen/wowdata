package casc

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wowdata/internal/blte"
)

const (
	cdnHostTemplate      = "http://%s.patch.battle.net:1119/"
	cdnHostTemplateChina = "http://cn.patch.battle.net:1119/"
	rangeChunkSize       = 1 << 20
	rangeWorkerCount     = 16
	httpDownloadAttempts = 3
)

var downloadProgressWriter io.Writer = os.Stderr

func SetDownloadProgressWriterForTest(w io.Writer) {
	downloadProgressWriter = w
}

func DownloadProgressWriterForTest() io.Writer {
	return downloadProgressWriter
}

func ReportDownloadProgress(url, method string, bytes, workers int, duration time.Duration) {
	if downloadProgressWriter == nil {
		return
	}
	if workers > 0 {
		fmt.Fprintf(downloadProgressWriter, "download method=%s url=%s bytes=%d workers=%d duration=%s\n", method, url, bytes, workers, duration.Round(time.Millisecond))
		return
	}
	fmt.Fprintf(downloadProgressWriter, "download method=%s url=%s bytes=%d duration=%s\n", method, url, bytes, duration.Round(time.Millisecond))
}

type CASCRemote struct {
	*CASCSource
	Region    string
	Host      string
	PatchHost string
	Builds    []VersionEntry
	Build     *VersionEntry
	Server    *VersionEntry
	Cache     *DataCache
	CacheRoot string
	fetchFull func(cdnFile string) ([]byte, error)

	fetchPartial func(cdnFile string, offset, length int) ([]byte, error)
}

func NewCASCRemote(region string) *CASCRemote {
	r := &CASCRemote{
		CASCSource: NewCASCSource(),
		Region:     region,
	}
	if region == "cn" {
		r.PatchHost = cdnHostTemplateChina
	} else {
		r.PatchHost = fmt.Sprintf(cdnHostTemplate, region)
	}
	r.Host = r.PatchHost
	return r
}

func (r *CASCRemote) GetBuildName() string {
	if r.Build != nil {
		return r.Build.VersionsName
	}
	return ""
}

func (r *CASCRemote) GetBuildKey() string {
	if r.Build != nil {
		return r.Build.BuildConfig
	}
	return ""
}

func (r *CASCRemote) Init() error {
	// Fetch version configs for all products
	builds := make([]VersionEntry, len(defaultProducts))
	var failures []string
	for productIndex, product := range defaultProducts {
		config, err := r.getVersionConfig(product)
		if err != nil {
			failures = append(failures, product+": "+err.Error())
			continue
		}
		for i := range config {
			if config[i].Region == r.Region {
				config[i].Product = product
				builds[productIndex] = config[i]
				break
			}
		}
	}
	r.Builds = builds
	loaded := 0
	for _, build := range builds {
		if build.Product != "" {
			loaded++
		}
	}
	if loaded == 0 {
		if len(failures) > 0 {
			return fmt.Errorf("no remote builds loaded for region %s: %s", r.Region, strings.Join(failures, "; "))
		}
		return fmt.Errorf("no remote builds loaded for region %s", r.Region)
	}
	return nil
}

var defaultProducts = []string{"wow", "wowt", "wowxptr", "wow_classic", "wow_classic_titan", "wow_classic_era"}

func (r *CASCRemote) getVersionConfig(product string) ([]VersionEntry, error) {
	url := r.PatchHost + product + "/versions"
	resp, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParseVersionConfig(string(body)), nil
}

func (r *CASCRemote) GetConfig(url string) (map[string]string, error) {
	resp, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParseCDNConfig(string(body))
}

func (r *CASCRemote) GetDataFile(cdnFile string) ([]byte, error) {
	url := r.Host + "data/" + cdnFile
	if data, err := DownloadHTTPConcurrent(url); err == nil {
		return data, nil
	}
	resp, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (r *CASCRemote) GetDataFilePartial(cdnFile string, offset, length int) ([]byte, error) {
	url := r.Host + "data/" + cdnFile
	data, err := httpRange(url, offset, offset+length-1)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func DecodeCASCData(data []byte) ([]byte, error) {
	reader, err := blte.NewReader(data)
	if err != nil {
		return nil, err
	}
	return reader.ReadAll()
}

func DecodeCASCDataPartial(data []byte) ([]byte, error) {
	reader, err := blte.NewPartialReader(data)
	if err != nil {
		return nil, err
	}
	return reader.ReadAll()
}

func (r *CASCRemote) ReadFileData(fileDataID uint32) ([]byte, error) {
	_, encodingKey, err := r.ResolveFileKeys(fileDataID)
	if err != nil {
		return nil, err
	}
	return r.ReadEncodingData(encodingKey)
}

func (r *CASCRemote) ReadFileDataPartial(fileDataID uint32) ([]byte, error) {
	_, encodingKey, err := r.ResolveFileKeys(fileDataID)
	if err != nil {
		return nil, err
	}
	return r.ReadEncodingDataPartial(encodingKey)
}

func (r *CASCRemote) ReadEncodingData(encodingKey string) ([]byte, error) {
	return r.readEncodingData(encodingKey, false)
}

func (r *CASCRemote) ReadEncodingDataPartial(encodingKey string) ([]byte, error) {
	return r.readEncodingData(encodingKey, true)
}

func (r *CASCRemote) readEncodingData(encodingKey string, partial bool) ([]byte, error) {
	if r.Cache != nil {
		if data, err := r.Cache.GetFile(encodingKey, filepath.Join(r.Cache.Root(), "data")); err == nil && len(data) > 0 {
			if partial {
				return DecodeCASCDataPartial(data)
			}
			return DecodeCASCData(data)
		}
	}

	var raw []byte
	var err error
	if archive, ok := r.Archives[encodingKey]; ok {
		raw, err = r.getDataFilePartial(FormatCDNKey(archive.Key), int(archive.Offset), int(archive.Size))
	} else {
		raw, err = r.getDataFile(FormatCDNKey(encodingKey))
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty CASC data for encoding key: %s", encodingKey)
	}
	var decoded []byte
	if partial {
		decoded, err = DecodeCASCDataPartial(raw)
	} else {
		decoded, err = DecodeCASCData(raw)
	}
	if err != nil {
		return nil, err
	}
	if r.Cache != nil {
		_ = r.Cache.StoreFile(encodingKey, raw, filepath.Join(r.Cache.Root(), "data"))
	}
	return decoded, nil
}

func (r *CASCRemote) getDataFile(cdnFile string) ([]byte, error) {
	if r.fetchFull != nil {
		return r.fetchFull(cdnFile)
	}
	return r.GetDataFile(cdnFile)
}

func (r *CASCRemote) getDataFilePartial(cdnFile string, offset, length int) ([]byte, error) {
	if r.fetchPartial != nil {
		return r.fetchPartial(cdnFile, offset, length)
	}
	return r.GetDataFilePartial(cdnFile, offset, length)
}

func (r *CASCRemote) Preload(buildIndex int) error {
	if buildIndex < 0 || buildIndex >= len(r.Builds) {
		return fmt.Errorf("build index %d out of range", buildIndex)
	}
	r.Build = &r.Builds[buildIndex]

	// Initialize cache
	cacheRoot := r.CacheRoot
	if cacheRoot == "" {
		cacheRoot = "cache"
	}
	r.Cache = NewDataCache(cacheRoot, r.Build.BuildConfig)

	if err := r.loadServerConfig(); err != nil {
		return fmt.Errorf("server config: %w", err)
	}
	if r.Server != nil {
		r.Host = r.bestCDNHost(r.Server)
	}

	// Load configs
	cdnCfg, err := r.GetConfig(r.Host + "config/" + FormatCDNKey(r.Build.CDNConfig))
	if err != nil {
		return fmt.Errorf("cdn config: %w", err)
	}
	r.SetCDNConfig(cdnCfg)

	buildCfg, err := r.GetConfig(r.Host + "config/" + FormatCDNKey(r.Build.BuildConfig))
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}
	r.SetBuildConfig(buildCfg)

	if err := r.loadArchives(); err != nil {
		return fmt.Errorf("archives: %w", err)
	}

	// Load encoding
	if err := r.loadEncoding(); err != nil {
		return fmt.Errorf("encoding: %w", err)
	}

	// Load root
	if err := r.loadRoot(); err != nil {
		return fmt.Errorf("root: %w", err)
	}

	return nil
}

func (r *CASCRemote) loadArchives() error {
	archiveKeys := strings.Fields(r.CDNConfig["archives"])
	if len(archiveKeys) == 0 {
		return nil
	}
	const workers = 32
	jobs := make(chan string)
	var mu sync.Mutex
	var failures []string
	var wg sync.WaitGroup

	workerCount := workers
	if len(archiveKeys) < workerCount {
		workerCount = len(archiveKeys)
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range jobs {
				data, err := r.downloadArchiveIndex(key)
				mu.Lock()
				if err != nil {
					failures = append(failures, key+": "+err.Error())
				} else {
					r.ParseArchiveIndex(data, key)
				}
				mu.Unlock()
			}
		}()
	}
	for _, key := range archiveKeys {
		jobs <- key
	}
	close(jobs)
	wg.Wait()
	if len(failures) == len(archiveKeys) {
		return fmt.Errorf("all archive indexes failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (r *CASCRemote) loadServerConfig() error {
	if r.Build == nil {
		return fmt.Errorf("build is not selected")
	}
	url := r.PatchHost + r.Build.Product + "/cdns"
	resp, err := httpGet(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	servers := ParseVersionConfig(string(body))
	for i := range servers {
		if servers[i].Name == r.Region || servers[i].Product == r.Region {
			r.Server = &servers[i]
			return nil
		}
	}
	return fmt.Errorf("CDN config does not contain entry for region %s", r.Region)
}

func (r *CASCRemote) bestCDNHost(server *VersionEntry) string {
	if server == nil {
		return r.Host
	}
	host := ""
	for _, h := range strings.Fields(server.Hosts) {
		host = h
		break
	}
	if host == "" {
		return r.Host
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}
	host = strings.TrimRight(host, "/") + "/"
	path := strings.Trim(server.Path, "/")
	if path != "" {
		host += path + "/"
	}
	return host
}

func (r *CASCRemote) downloadArchiveIndex(key string) ([]byte, error) {
	fileName := key + ".index"
	data, err := r.Cache.GetFile(fileName, "")
	if err == nil && len(data) > 0 {
		return data, nil
	}
	cdnKey := FormatCDNKey(key) + ".index"
	data, err = r.GetDataFile(cdnKey)
	if err != nil {
		return nil, err
	}
	r.Cache.StoreFile(fileName, data, "")
	return data, nil
}

func (r *CASCRemote) loadEncoding() error {
	encKeys := strings.Split(r.BuildConfig["encoding"], " ")
	if len(encKeys) < 2 {
		return fmt.Errorf("encoding key not found in build config")
	}
	encKey := encKeys[1]

	data, err := r.getConfigFileWithCache("build_encoding", encKey)
	if err != nil {
		return err
	}
	return r.ParseEncodingFile(data)
}

func (r *CASCRemote) loadRoot() error {
	rootKey, ok := r.EncodingKeys[r.BuildConfig["root"]]
	if !ok {
		return fmt.Errorf("no encoding entry found for root key")
	}

	data, err := r.getConfigFileWithCache("build_root", rootKey)
	if err != nil {
		return err
	}
	_, err = r.ParseRootFile(data)
	return err
}

func (r *CASCRemote) getConfigFileWithCache(name, key string) ([]byte, error) {
	data, err := r.Cache.GetFile(name, "")
	if err == nil && len(data) > 0 {
		return data, nil
	}
	data, err = r.GetDataFile(FormatCDNKey(key))
	if err != nil {
		return nil, err
	}
	r.Cache.StoreFile(name, data, "")
	return data, nil
}

func (r *CASCRemote) GetProductList() []Product {
	var products []Product
	for i, build := range r.Builds {
		if build.Product == "" {
			continue
		}
		label := fmt.Sprintf("%s %s", knownProductTitle(build.Product), build.VersionsName)
		products = append(products, Product{
			Label:      strings.TrimSpace(label),
			BuildIndex: i,
		})
	}
	return products
}

var httpClient = &http.Client{Timeout: 120 * time.Second}

func httpGet(url string) (*http.Response, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return resp, nil
}

// DownloadHTTPConcurrent downloads a URL using HTTP range chunks when the
// remote advertises byte ranges and the file is large enough. Small files or
// servers without range support return an error so callers can use a normal
// GET fallback.
func DownloadHTTPConcurrent(url string) ([]byte, error) {
	return httpGetConcurrent(url)
}

func httpGetConcurrent(url string) ([]byte, error) {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d head from %s", resp.StatusCode, url)
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Accept-Ranges")), "bytes") {
		return nil, fmt.Errorf("server does not advertise range support")
	}
	size := int(resp.ContentLength)
	if size <= rangeChunkSize {
		return nil, fmt.Errorf("file is below concurrent download threshold")
	}
	return httpRangeConcurrent(url, 0, size-1)
}

func httpRange(url string, start, end int) ([]byte, error) {
	length := end - start + 1
	if length > rangeChunkSize {
		return httpRangeConcurrent(url, start, end)
	}
	return httpRangeSingle(url, start, end)
}

func httpRangeSingle(url string, start, end int) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < httpDownloadAttempts; attempt++ {
		data, err := httpRangeSingleOnce(url, start, end)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if attempt+1 < httpDownloadAttempts {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}
	return nil, lastErr
}

func httpRangeSingleOnce(url string, start, end int) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d range request", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func httpRangeConcurrent(url string, start, end int) ([]byte, error) {
	startedAt := time.Now()
	length := end - start + 1
	if length <= 0 {
		return nil, fmt.Errorf("invalid range %d-%d", start, end)
	}
	out := make([]byte, length)
	type chunk struct {
		start int
		end   int
	}
	jobs := make(chan chunk)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	workers := rangeWorkerCount
	chunkCount := (length + rangeChunkSize - 1) / rangeChunkSize
	if chunkCount < workers {
		workers = chunkCount
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				select {
				case <-errCh:
					return
				default:
				}
				data, err := httpRangeSingle(url, job.start, job.end)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				offset := job.start - start
				copy(out[offset:offset+len(data)], data)
			}
		}()
	}

	for chunkStart := start; chunkStart <= end; chunkStart += rangeChunkSize {
		chunkEnd := chunkStart + rangeChunkSize - 1
		if chunkEnd > end {
			chunkEnd = end
		}
		select {
		case err := <-errCh:
			close(jobs)
			wg.Wait()
			return nil, err
		case jobs <- chunk{start: chunkStart, end: chunkEnd}:
		}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errCh:
		return nil, err
	default:
		ReportDownloadProgress(url, "range", length, workers, time.Since(startedAt))
		return out, nil
	}
}
