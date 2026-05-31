package export

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func buildTestBLP(width, height int) []byte {
	dataOfs := 148 + 256*4
	data := make([]byte, dataOfs+width*height*4)
	binary.LittleEndian.PutUint32(data, 0x32504C42) // BLP2
	data[4] = 3                                     // encoding raw BGRA
	data[7] = 1                                     // hasMipmaps
	binary.LittleEndian.PutUint32(data[8:], 1)      // preferredFormat
	binary.LittleEndian.PutUint32(data[12:], uint32(width))
	binary.LittleEndian.PutUint32(data[16:], uint32(height))
	binary.LittleEndian.PutUint32(data[20:], uint32(dataOfs))        // mapOffset[0]
	binary.LittleEndian.PutUint32(data[84:], uint32(width*height*4)) // mapSize[0]
	// Fill resolution-dependent area
	for ofs := dataOfs; ofs < len(data); ofs += 4 {
		data[ofs] = 0xBB
		data[ofs+1] = 0xCC
		data[ofs+2] = 0xAA
		data[ofs+3] = 0xFF
	}
	return data
}

func TestExportIcon(t *testing.T) {
	blpData := buildTestBLP(4, 4)
	dir := t.TempDir()
	out := filepath.Join(dir, "icon.png")

	result, err := ExportIcon(blpData, out, "png", 0)
	if err != nil {
		t.Fatalf("ExportIcon: %v", err)
	}
	if !result.OK {
		t.Fatal("result not OK")
	}
	if result.Width != 4 {
		t.Fatalf("width = %d", result.Width)
	}

	// Verify file exists
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output file missing: %v", err)
	}
}

func TestExportIconWebP(t *testing.T) {
	blpData := buildTestBLP(4, 4)
	dir := t.TempDir()
	out := filepath.Join(dir, "icon.webp")

	result, err := ExportIcon(blpData, out, "webp", 0)
	if err != nil {
		t.Fatalf("ExportIcon webp: %v", err)
	}
	if result.Format != "webp" {
		t.Fatalf("format = %s", result.Format)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output file missing: %v", err)
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		t.Fatalf("output is not a WebP RIFF file: %x", data[:min(len(data), 12)])
	}
}

func containsChunk(data []byte, chunk string) bool {
	for i := 0; i+len(chunk) <= len(data); i++ {
		if string(data[i:i+len(chunk)]) == chunk {
			return true
		}
	}
	return false
}
