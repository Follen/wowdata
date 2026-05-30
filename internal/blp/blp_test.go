package blp

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func buildBLPHeader(encoding uint8, width, height int) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(blpMagic))
	buf.WriteByte(encoding) // encoding
	buf.WriteByte(0)        // alphaDepth
	buf.WriteByte(0)        // alphaEncoding
	buf.WriteByte(1)        // hasMipmaps
	binary.Write(buf, binary.LittleEndian, uint32(width))
	binary.Write(buf, binary.LittleEndian, uint32(height))

	// Map offsets (1 mipmap)
	binary.Write(buf, binary.LittleEndian, uint32(148+16*4)) // offset of data
	for i := 1; i < 16; i++ {
		binary.Write(buf, binary.LittleEndian, uint32(0))
	}
	// Map sizes
	binary.Write(buf, binary.LittleEndian, uint32(uint32(width*height*4)))
	for i := 1; i < 16; i++ {
		binary.Write(buf, binary.LittleEndian, uint32(0))
	}

	// Palette (256 * 4 = 1024 bytes of zeros for non-palette images)
	for i := 0; i < 256; i++ {
		binary.Write(buf, binary.LittleEndian, uint32(0xFF0000FF))
	}

	// Data
	pixelData := make([]byte, width*height*4)
	for i := 0; i < width*height*4; i += 4 {
		pixelData[i] = 0xBB    // B
		pixelData[i+1] = 0xCC  // G
		pixelData[i+2] = 0xAA  // R
		pixelData[i+3] = 0xFF  // A
	}
	buf.Write(pixelData)

	return buf.Bytes()
}

func TestDecodeBLP(t *testing.T) {
	data := buildBLPHeader(3, 4, 4)
	img, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if img.Width != 4 {
		t.Fatalf("width = %d", img.Width)
	}
	if img.Height != 4 {
		t.Fatalf("height = %d", img.Height)
	}
	if img.MapCount != 1 {
		t.Fatalf("mapCount = %d", img.MapCount)
	}
}

func TestDecodeInvalid(t *testing.T) {
	_, err := Decode([]byte{0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Fatal("expected error for invalid magic")
	}
}

func TestCheck(t *testing.T) {
	data := buildBLPHeader(3, 4, 4)
	if !Check(data) {
		t.Fatal("Check failed for valid BLP")
	}
	if Check([]byte{1, 2, 3}) {
		t.Fatal("Check should fail for short data")
	}
}

func TestExportPNG(t *testing.T) {
	data := buildBLPHeader(3, 4, 4)
	img, _ := Decode(data)

	dir := t.TempDir()
	out := filepath.Join(dir, "test.png")

	result, err := img.ExportPNG(out, 0)
	if err != nil {
		t.Fatalf("ExportPNG: %v", err)
	}
	if result.Width != 4 || result.Height != 4 {
		t.Fatalf("dimensions = %dx%d", result.Width, result.Height)
	}
	if result.Format != "png" {
		t.Fatalf("format = %s", result.Format)
	}
	if result.Hash == "" {
		t.Fatal("empty hash")
	}

	// Verify file exists
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output file not found: %v", err)
	}
}

func TestGetMipmapOutOfRange(t *testing.T) {
	data := buildBLPHeader(3, 4, 4)
	img, _ := Decode(data)

	_, _, _, err := img.GetMipmap(5)
	if err == nil {
		t.Fatal("expected error for out-of-range mipmap")
	}
}
