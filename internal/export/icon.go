package export

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"wowdata/internal/blp"
)

type IconExportResult struct {
	OK     bool   `json:"ok"`
	Status string `json:"status"`
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
	Mipmap int    `json:"mipmap"`
	Mask   int    `json:"mask,omitempty"`
	Hash   string `json:"hash"`
}

func ExportIcon(data []byte, outputPath, format string, mipmap int) (*IconExportResult, error) {
	return ExportIconWithOptions(data, outputPath, format, mipmap, 15)
}

func ExportIconWithOptions(data []byte, outputPath, format string, mipmap, mask int) (*IconExportResult, error) {
	img, err := blp.Decode(data)
	if err != nil {
		return nil, err
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "png"
	}

	dir := filepath.Dir(outputPath)
	if dir != "" {
		os.MkdirAll(dir, 0755)
	}

	var exportResult *blp.ExportPNGResult
	switch format {
	case "png":
		exportResult, err = img.ExportPNG(outputPath, mipmap)
	case "webp":
		exportResult, err = img.ExportWebP(outputPath, mipmap)
	default:
		return nil, fmt.Errorf("unsupported icon format: %s", format)
	}
	if err != nil {
		return nil, err
	}
	resultPath := exportResult.Path
	if abs, err := filepath.Abs(resultPath); err == nil {
		resultPath = abs
	}

	result := &IconExportResult{
		OK:     true,
		Status: "extracted",
		Path:   resultPath,
		Width:  exportResult.Width,
		Height: exportResult.Height,
		Format: format,
		Mipmap: mipmap,
		Mask:   mask,
	}
	// Verify hash
	if fileData, err := os.ReadFile(outputPath); err == nil {
		h := md5.Sum(fileData)
		result.Hash = hex.EncodeToString(h[:])
	}

	return result, nil
}
