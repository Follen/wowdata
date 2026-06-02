package export

import (
	"crypto/sha256"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

type ExportResult struct {
	OK        bool   `json:"ok"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	URI       string `json:"uri"`
	MimeType  string `json:"mimeType"`
	Size      int64  `json:"size"`
	Hash      string `json:"sha256"`
	Overwrite bool   `json:"overwrite"`
}

func ExportFile(data []byte, outputPath string) (*ExportResult, error) {
	if outputPath == "" {
		return nil, fmt.Errorf("output path is required")
	}

	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return nil, fmt.Errorf("resolve output path: %w", err)
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	// Check if exists
	overwrite := false
	if _, err := os.Stat(absPath); err == nil {
		overwrite = true
	}

	if err := os.WriteFile(absPath, data, 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	hash := sha256.Sum256(data)

	return &ExportResult{
		OK:        true,
		Name:      filepath.Base(absPath),
		Path:      absPath,
		URI:       FileURI(absPath),
		MimeType:  mimeTypeForPath(absPath),
		Size:      int64(len(data)),
		Hash:      fmt.Sprintf("%x", hash),
		Overwrite: overwrite,
	}, nil
}

func FileURI(path string) string {
	slash := filepath.ToSlash(path)
	if len(slash) >= 2 && slash[1] == ':' {
		return "file:///" + slash
	}
	if strings.HasPrefix(slash, "/") {
		return "file://" + slash
	}
	return "file:///" + slash
}

func mimeTypeForPath(path string) string {
	if typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); typ != "" {
		if before, _, ok := strings.Cut(typ, ";"); ok {
			return strings.TrimSpace(before)
		}
		return typ
	}
	return "application/octet-stream"
}
