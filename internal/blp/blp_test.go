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
	binary.Write(buf, binary.LittleEndian, uint32(1)) // version
	buf.WriteByte(encoding)                           // encoding
	buf.WriteByte(0)                                  // alphaDepth
	buf.WriteByte(0)                                  // alphaEncoding
	buf.WriteByte(1)                                  // hasMipmaps
	binary.Write(buf, binary.LittleEndian, uint32(width))
	binary.Write(buf, binary.LittleEndian, uint32(height))

	// Map offsets (1 mipmap)
	binary.Write(buf, binary.LittleEndian, uint32(148+256*4)) // offset of data
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
		pixelData[i] = 0xBB   // B
		pixelData[i+1] = 0xCC // G
		pixelData[i+2] = 0xAA // R
		pixelData[i+3] = 0xFF // A
	}
	buf.Write(pixelData)

	return buf.Bytes()
}

func buildDXTBLP(width, height int, alphaDepth, alphaEncoding uint8, block []byte) []byte {
	dataOfs := 148 + 256*4
	data := make([]byte, dataOfs+len(block))
	binary.LittleEndian.PutUint32(data, blpMagic)
	binary.LittleEndian.PutUint32(data[4:], 1)
	data[8] = 2
	data[9] = alphaDepth
	data[10] = alphaEncoding
	data[11] = 1
	binary.LittleEndian.PutUint32(data[12:], uint32(width))
	binary.LittleEndian.PutUint32(data[16:], uint32(height))
	binary.LittleEndian.PutUint32(data[20:], uint32(dataOfs))
	binary.LittleEndian.PutUint32(data[84:], uint32(len(block)))
	copy(data[dataOfs:], block)
	return data
}

func TestDecodeBLP2HeaderLayout(t *testing.T) {
	img, err := Decode(buildDXTBLP(4, 4, 8, 7, make([]byte, 16)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if img.Encoding != 2 {
		t.Fatalf("encoding = %d, want 2", img.Encoding)
	}
	if img.AlphaDepth != 8 {
		t.Fatalf("alphaDepth = %d, want 8", img.AlphaDepth)
	}
	if img.AlphaEncoding != 7 {
		t.Fatalf("alphaEncoding = %d, want 7", img.AlphaEncoding)
	}
	if img.HasMipmaps != 1 {
		t.Fatalf("hasMipmaps = %d, want 1", img.HasMipmaps)
	}
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

func TestDecodeDXT1Block(t *testing.T) {
	block := make([]byte, 8)
	binary.LittleEndian.PutUint16(block[0:], 0xF800) // red
	binary.LittleEndian.PutUint16(block[2:], 0x07E0) // green

	img, err := Decode(buildDXTBLP(4, 4, 0, 0, block))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	pixels, _, _, err := img.GetMipmap(0)
	if err != nil {
		t.Fatalf("GetMipmap: %v", err)
	}
	if got := pixels[:4]; !bytes.Equal(got, []byte{255, 0, 0, 255}) {
		t.Fatalf("first pixel = %#v", got)
	}
}

func TestDecodeDXT3Alpha(t *testing.T) {
	block := make([]byte, 16)
	for i := 0; i < 8; i++ {
		block[i] = 0x0F
	}
	binary.LittleEndian.PutUint16(block[8:], 0xFFFF)
	binary.LittleEndian.PutUint16(block[10:], 0x0000)

	img, err := Decode(buildDXTBLP(4, 4, 4, 1, block))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	pixels, _, _, err := img.GetMipmap(0)
	if err != nil {
		t.Fatalf("GetMipmap: %v", err)
	}
	if pixels[3] != 0xFF || pixels[7] != 0x00 {
		t.Fatalf("alpha values = %d %d", pixels[3], pixels[7])
	}
}

func TestDecodeDXT5Alpha(t *testing.T) {
	block := make([]byte, 16)
	block[0] = 0
	block[1] = 255
	binary.LittleEndian.PutUint16(block[8:], 0xFFFF)
	binary.LittleEndian.PutUint16(block[10:], 0x0000)

	img, err := Decode(buildDXTBLP(4, 4, 8, 7, block))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	pixels, _, _, err := img.GetMipmap(0)
	if err != nil {
		t.Fatalf("GetMipmap: %v", err)
	}
	if pixels[3] != 0 {
		t.Fatalf("first alpha = %d, want 0", pixels[3])
	}
}
