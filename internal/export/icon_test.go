package export

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func buildTestBLP(width, height int) []byte {
	data := make([]byte, 148+16*8+width*height*4)
	binary.LittleEndian.PutUint32(data, 0x32504C42) // BLP2
	data[4] = 3 // encoding raw BGRA
	data[7] = 1 // hasMipmaps
	binary.LittleEndian.PutUint32(data[8:], uint32(width))
	binary.LittleEndian.PutUint32(data[12:], uint32(height))
	binary.LittleEndian.PutUint32(data[16:], uint32(148+16*8)) // mapOffset[0]
	binary.LittleEndian.PutUint32(data[80:], uint32(width*height*4)) // mapSize[0]
	// Fill resolution-dependent area
	for i := 0; i < 256; i++ {
		ofs := 148 + i*4
		if ofs+4 <= len(data) {
			data[ofs] = 0xBB
			data[ofs+1] = 0xCC
			data[ofs+2] = 0xAA
			data[ofs+3] = 0xFF
		}
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
