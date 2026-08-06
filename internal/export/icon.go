package export

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"wowdata/internal/blp"
	"wowdata/internal/resource"
)

type IconExportResult struct {
	OK       bool   `json:"ok"`
	Status   string `json:"status"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	URI      string `json:"uri"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Format   string `json:"format"`
	MimeType string `json:"mimeType"`
	Mipmap   int    `json:"mipmap"`
	Mask     int    `json:"mask,omitempty"`
	Hash     string `json:"hash"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

type RenderedIcon struct {
	Data   []byte
	Width  int
	Height int
	SHA256 string
}

func RenderIconPNG(data []byte, mipmap int) (*RenderedIcon, error) {
	img, err := blp.Decode(data)
	if err != nil {
		return nil, err
	}
	rgba, width, height, err := img.Image(mipmap)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := png.Encode(&output, rgba); err != nil {
		return nil, err
	}
	rendered := append([]byte(nil), output.Bytes()...)
	resource.RecordImageEncode("png", width*height, len(rendered))
	hash := resource.SumSHA256(rendered)
	return &RenderedIcon{Data: rendered, Width: width, Height: height, SHA256: fmt.Sprintf("%x", hash)}, nil
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
		OK:       true,
		Status:   "extracted",
		Name:     filepath.Base(resultPath),
		Path:     resultPath,
		URI:      FileURI(resultPath),
		Width:    exportResult.Width,
		Height:   exportResult.Height,
		Format:   format,
		MimeType: mimeTypeForPath(resultPath),
		Mipmap:   mipmap,
		Mask:     mask,
	}
	if fileData, err := resource.ReadFile(outputPath); err == nil {
		h := resource.SumSHA256(fileData)
		result.Hash = fmt.Sprintf("%x", h)
		result.SHA256 = result.Hash
		result.Size = int64(len(fileData))
	}

	return result, nil
}
