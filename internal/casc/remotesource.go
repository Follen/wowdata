package casc

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const (
	cdnHostTemplate  = "https://%s.patch.battle.net:1119/"
	cdnHostTemplateChina = "https://cn.patch.battle.net:1119/"
)

type CASCRemote struct {
	*CASCSource
	Region    string
	Host      string
	Builds    []VersionEntry
	Build     *VersionEntry
	Cache     *DataCache
}

func NewCASCRemote(region string) *CASCRemote {
	r := &CASCRemote{
		CASCSource: NewCASCSource(),
		Region:     region,
	}
	if region == "cn" {
		r.Host = cdnHostTemplateChina
	} else {
		r.Host = fmt.Sprintf(cdnHostTemplate, region)
	}
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
	var allBuilds []VersionEntry
	for _, product := range defaultProducts {
		config, err := r.getVersionConfig(product)
		if err != nil {
			continue
		}
		for i := range config {
			if config[i].Region == r.Region {
				config[i].Product = product
				allBuilds = append(allBuilds, config[i])
			}
		}
	}
	r.Builds = allBuilds
	return nil
}

var defaultProducts = []string{"wow", "wow_classic", "wow_classic_era", "wow_classic_ptr", "wow_beta", "wowt", "wowz"}

func (r *CASCRemote) getVersionConfig(product string) ([]VersionEntry, error) {
	url := r.Host + product + "/versions"
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

func (r *CASCRemote) Preload(buildIndex int) error {
	if buildIndex < 0 || buildIndex >= len(r.Builds) {
		return fmt.Errorf("build index %d out of range", buildIndex)
	}
	r.Build = &r.Builds[buildIndex]

	// Initialize cache
	r.Cache = NewDataCache(filepath.Join("cache"), r.Build.BuildConfig)

	// Load configs
	cdnCfg, err := r.GetConfig(r.Host + r.Build.Product + "/config/" + FormatCDNKey(r.Build.CDNConfig))
	if err != nil {
		return fmt.Errorf("cdn config: %w", err)
	}
	r.SetCDNConfig(cdnCfg)

	buildCfg, err := r.GetConfig(r.Host + r.Build.Product + "/config/" + FormatCDNKey(r.Build.BuildConfig))
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}
	r.SetBuildConfig(buildCfg)

	// Load archives
	archiveKeys := strings.Split(r.CDNConfig["archives"], " ")
	for _, key := range archiveKeys {
		if key == "" {
			continue
		}
		data, err := r.downloadArchiveIndex(key)
		if err != nil {
			continue
		}
		r.ParseArchiveIndex(data, key)
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
		products = append(products, Product{
			Label:      build.Product + " " + build.VersionsName,
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

func httpRange(url string, start, end int) ([]byte, error) {
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
