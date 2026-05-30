package export

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"

	"wowdata/internal/blp"
)

type IconExportResult struct {
	OK      bool   `json:"ok"`
	Path    string `json:"path"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Format  string `json:"format"`
	Mipmap  int    `json:"mipmap"`
	Hash    string `json:"hash"`
}

func ExportIcon(data []byte, outputPath, format string, mipmap int) (*IconExportResult, error) {
	img, err := blp.Decode(data)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(outputPath)
	if dir != "" {
		os.MkdirAll(dir, 0755)
	}

	pngResult, err := img.ExportPNG(outputPath, mipmap)
	if err != nil {
		return nil, err
	}

	result := &IconExportResult{
		OK:     true,
		Path:   pngResult.Path,
		Width:  pngResult.Width,
		Height: pngResult.Height,
		Format: format,
		Mipmap: mipmap,
	}

	// Verify hash
	if fileData, err := os.ReadFile(outputPath); err == nil {
		h := md5.Sum(fileData)
		result.Hash = hex.EncodeToString(h[:])
	}

	return result, nil
}
