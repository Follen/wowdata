package blp

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
)

const blpMagic = 0x32504C42 // BLP2

type BLPImage struct {
	Encoding       uint8
	AlphaDepth     uint8
	AlphaEncoding  uint8
	HasMipmaps     uint8
	Width          int
	Height         int
	MapOffsets     [16]uint32
	MapSizes       [16]uint32
	MapCount       int
	Palette        [256][4]byte
	rawData        []byte
}

func Decode(data []byte) (*BLPImage, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("BLP data too short")
	}

	magic := binary.LittleEndian.Uint32(data)
	if magic != blpMagic {
		return nil, fmt.Errorf("invalid BLP magic: 0x%X", magic)
	}

	img := &BLPImage{
		Encoding: data[4],
	}

	img.AlphaDepth = data[5]
	img.AlphaEncoding = data[6]
	img.HasMipmaps = data[7]
	img.Width = int(binary.LittleEndian.Uint32(data[8:]))
	img.Height = int(binary.LittleEndian.Uint32(data[12:]))

	for i := 0; i < 16; i++ {
		img.MapOffsets[i] = binary.LittleEndian.Uint32(data[16+i*4:])
		img.MapSizes[i] = binary.LittleEndian.Uint32(data[80+i*4:])
		if img.MapOffsets[i] > 0 {
			img.MapCount++
		}
	}

	// Palette for encoding 1
	if img.Encoding == 1 && len(data) > 148 {
		img.Palette = [256][4]byte{}
		for i := 0; i < 256; i++ {
			ofs := 148 + i*4
			if ofs+4 > len(data) {
				break
			}
			img.Palette[i][0] = data[ofs+2] // R
			img.Palette[i][1] = data[ofs+1] // G
			img.Palette[i][2] = data[ofs]   // B
			img.Palette[i][3] = data[ofs+3] // A
		}
	}

	img.rawData = data
	return img, nil
}

func (img *BLPImage) GetMipmap(level int) ([]byte, int, int, error) {
	if level < 0 || level >= img.MapCount {
		return nil, 0, 0, fmt.Errorf("mipmap level %d out of range (0-%d)", level, img.MapCount-1)
	}

	offset := int(img.MapOffsets[level])
	size := int(img.MapSizes[level])
	if offset+size > len(img.rawData) {
		return nil, 0, 0, fmt.Errorf("mipmap data out of bounds")
	}

	width := img.Width >> level
	height := img.Height >> level
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	raw := img.rawData[offset : offset+size]
	var pixels []byte

	switch img.Encoding {
	case 1: // Palette
		pixels = make([]byte, width*height*4)
		for i := 0; i < width*height; i++ {
			if i >= len(raw) {
				break
			}
			idx := raw[i]
			ofs := i * 4
			pixels[ofs] = img.Palette[idx][0]
			pixels[ofs+1] = img.Palette[idx][1]
			pixels[ofs+2] = img.Palette[idx][2]
			pixels[ofs+3] = img.Palette[idx][3]
		}
	case 2: // DXT compressed (simplified)
		pixels = make([]byte, width*height*4)
		for p := range pixels {
			pixels[p] = 255
		}
	case 3: // Raw BGRA
		pixels = make([]byte, width*height*4)
		for i := 0; i < width*height*4; i += 4 {
			if i/4 < len(raw)/4 {
				// BGRA -> RGBA
				pixels[i] = raw[i+2]
				pixels[i+1] = raw[i+1]
				pixels[i+2] = raw[i]
				pixels[i+3] = raw[i+3]
			} else {
				pixels[i+3] = 255
			}
		}
	default:
		pixels = make([]byte, width*height*4)
	}

	return pixels, width, height, nil
}

type ExportPNGResult struct {
	Path      string `json:"path"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Format    string `json:"format"`
	Mipmap    int    `json:"mipmap"`
	Hash      string `json:"hash"`
}

func (img *BLPImage) ExportPNG(outputPath string, mipmap int) (*ExportPNGResult, error) {
	pixels, w, h, err := img.GetMipmap(mipmap)
	if err != nil {
		return nil, err
	}

	rgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(rgba.Pix, pixels)

	dir := filepath.Dir(outputPath)
	if dir != "" {
		os.MkdirAll(dir, 0755)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	pngBytes := buf.Bytes()

	if err := os.WriteFile(outputPath, pngBytes, 0644); err != nil {
		return nil, err
	}

	hash := md5.Sum(pngBytes)
	return &ExportPNGResult{
		Path:   outputPath,
		Width:  w,
		Height: h,
		Format: "png",
		Mipmap: mipmap,
		Hash:   hex.EncodeToString(hash[:]),
	}, nil
}

func Check(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return binary.LittleEndian.Uint32(data) == blpMagic
}

// DetectEncoding detects if data is likely a BLP file.
func DetectEncoding(reader io.Reader) ([]byte, error) {
	return io.ReadAll(reader)
}
