package diagnostics

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type CASCInfo struct {
	Source             string `json:"source"`
	Region             string `json:"region"`
	Product            string `json:"product"`
	Locale             string `json:"locale,omitempty"`
	BuildName          string `json:"buildName,omitempty"`
	BuildKey           string `json:"buildKey,omitempty"`
	CachePath          string `json:"cachePath"`
	CDNHost            string `json:"cdnHost,omitempty"`
	ArchiveCount       int    `json:"archiveCount,omitempty"`
	RootEntryCount     int    `json:"rootEntryCount,omitempty"`
	EncodingEntryCount int    `json:"encodingEntryCount,omitempty"`
	TACTKeyCount       int    `json:"tactKeyCount,omitempty"`
}

type CASCProducts struct {
	Source   string    `json:"source"`
	Products []Product `json:"products"`
}

type Product struct {
	Label          string   `json:"label"`
	BuildIndex     int      `json:"buildIndex"`
	Product        string   `json:"product"`
	Region         string   `json:"region,omitempty"`
	Version        string   `json:"version,omitempty"`
	BuildID        string   `json:"buildId,omitempty"`
	BuildConfigKey string   `json:"buildConfigKey,omitempty"`
	CDNConfigKey   string   `json:"cdnConfigKey,omitempty"`
	Branch         string   `json:"branch,omitempty"`
	Locales        []string `json:"locales,omitempty"`
}

type CASCDiagnose struct {
	OK     bool            `json:"ok"`
	Source string          `json:"source"`
	Checks []DiagnoseCheck `json:"checks"`
}

type DiagnoseCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok", "error", "unknown"
	Detail string `json:"detail,omitempty"`
}

type DiagnosticsService struct {
	info     CASCInfo
	products CASCProducts
}

func NewDiagnosticsService() *DiagnosticsService {
	return &DiagnosticsService{}
}

func (ds *DiagnosticsService) SetInfo(info CASCInfo) {
	ds.info = info
}

func (ds *DiagnosticsService) SetProducts(products CASCProducts) {
	ds.products = products
}

func (ds *DiagnosticsService) GetInfo() CASCInfo { return ds.info }

func (ds *DiagnosticsService) GetProducts() CASCProducts { return ds.products }

func (ds *DiagnosticsService) Diagnose() CASCDiagnose {
	d := CASCDiagnose{
		OK:     true,
		Source: ds.info.Source,
	}

	checks := []DiagnoseCheck{
		{Name: "source", Status: "ok", Detail: ds.info.Source},
		{Name: "region", Status: "ok", Detail: ds.info.Region},
		{Name: "product", Status: "ok", Detail: ds.info.Product},
		{Name: "cache_path", Status: "ok", Detail: ds.info.CachePath},
		{Name: "go_runtime", Status: "ok", Detail: runtime.Version()},
	}

	// Check cache path exists
	if ds.info.CachePath != "" {
		if _, err := os.Stat(filepath.ToSlash(ds.info.CachePath)); os.IsNotExist(err) {
			checks = append(checks, DiagnoseCheck{
				Name: "cache_dir", Status: "error",
				Detail: "cache directory does not exist: " + ds.info.CachePath,
			})
			d.OK = false
		} else {
			checks = append(checks, DiagnoseCheck{
				Name: "cache_dir", Status: "ok",
				Detail: "cache directory exists",
			})
		}
	}

	if ds.info.BuildKey == "" {
		checks = append(checks, DiagnoseCheck{
			Name: "build_key", Status: "unknown",
			Detail: "no build selected; run a data command with a complete target or profile",
		})
	} else {
		checks = append(checks, DiagnoseCheck{
			Name: "build_key", Status: "ok", Detail: ds.info.BuildKey,
		})
	}

	appendCountCheck := func(name string, count int) {
		if count <= 0 {
			checks = append(checks, DiagnoseCheck{Name: name, Status: "unknown", Detail: "not loaded"})
			return
		}
		checks = append(checks, DiagnoseCheck{Name: name, Status: "ok", Detail: fmt.Sprintf("%d", count)})
	}
	appendCountCheck("archives", ds.info.ArchiveCount)
	appendCountCheck("root", ds.info.RootEntryCount)
	appendCountCheck("encoding", ds.info.EncodingEntryCount)
	appendCountCheck("tact_keys", ds.info.TACTKeyCount)

	d.Checks = checks
	return d
}

func VerifyFileHash(path string, expectedHash string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]) == expectedHash, nil
}
