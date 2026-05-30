package export

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

type ExportResult struct {
	OK        bool   `json:"ok"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Hash      string `json:"sha256"`
	Overwrite bool   `json:"overwrite"`
}

func ExportFile(data []byte, outputPath string) (*ExportResult, error) {
	if outputPath == "" {
		return nil, fmt.Errorf("output path is required")
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	// Check if exists
	overwrite := false
	if _, err := os.Stat(outputPath); err == nil {
		overwrite = true
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	hash := sha256.Sum256(data)

	return &ExportResult{
		OK:        true,
		Path:      outputPath,
		Size:      int64(len(data)),
		Hash:      fmt.Sprintf("%x", hash),
		Overwrite: overwrite,
	}, nil
}
