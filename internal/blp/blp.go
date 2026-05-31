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

	"github.com/HugoSmits86/nativewebp"
)

const blpMagic = 0x32504C42 // BLP2

type BLPImage struct {
	Encoding      uint8
	AlphaDepth    uint8
	AlphaEncoding uint8
	HasMipmaps    uint8
	Width         int
	Height        int
	MapOffsets    [16]uint32
	MapSizes      [16]uint32
	MapCount      int
	Palette       [256][4]byte
	rawData       []byte
}

func Decode(data []byte) (*BLPImage, error) {
	if len(data) < 148 {
		return nil, fmt.Errorf("BLP data too short")
	}

	magic := binary.LittleEndian.Uint32(data)
	if magic != blpMagic {
		return nil, fmt.Errorf("invalid BLP magic: 0x%X", magic)
	}

	img := &BLPImage{
		Encoding: data[8],
	}

	img.AlphaDepth = data[9]
	img.AlphaEncoding = data[10]
	img.HasMipmaps = data[11]
	img.Width = int(binary.LittleEndian.Uint32(data[12:]))
	img.Height = int(binary.LittleEndian.Uint32(data[16:]))

	for i := 0; i < 16; i++ {
		img.MapOffsets[i] = binary.LittleEndian.Uint32(data[20+i*4:])
		img.MapSizes[i] = binary.LittleEndian.Uint32(data[84+i*4:])
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
	var err error

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
	case 2: // DXT compressed
		pixels, err = img.decodeDXT(raw, width, height)
		if err != nil {
			return nil, 0, 0, err
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

func (img *BLPImage) decodeDXT(raw []byte, width, height int) ([]byte, error) {
	pixels := make([]byte, width*height*4)
	flags := dxt1
	if img.AlphaDepth > 1 {
		if img.AlphaEncoding == 7 {
			flags = dxt5
		} else {
			flags = dxt3
		}
	}
	blockBytes := 8
	if flags != dxt1 {
		blockBytes = 16
	}
	pos := 0
	for y := 0; y < height; y += 4 {
		for x := 0; x < width; x += 4 {
			if pos >= len(raw) {
				return pixels, nil
			}
			if pos+blockBytes > len(raw) {
				return nil, fmt.Errorf("truncated DXT block")
			}
			block := raw[pos : pos+blockBytes]
			img.decodeDXTBlock(pixels, width, height, x, y, block, flags)
			pos += blockBytes
		}
	}
	return pixels, nil
}

const (
	dxt1 = 1
	dxt3 = 2
	dxt5 = 4
)

func (img *BLPImage) decodeDXTBlock(dst []byte, width, height, x, y int, block []byte, flags int) {
	colourIndex := 0
	if flags == dxt3 || flags == dxt5 {
		colourIndex = 8
	}
	colours := decodeDXTColours(block[colourIndex:], flags == dxt1)
	indices := binary.LittleEndian.Uint32(block[colourIndex+4:])
	alpha := decodeDXTAlpha(block, flags)
	for py := 0; py < 4; py++ {
		for px := 0; px < 4; px++ {
			sx := x + px
			sy := y + py
			if sx >= width || sy >= height {
				continue
			}
			i := py*4 + px
			ci := (indices >> uint(2*i)) & 0x03
			src := colours[ci]
			di := 4 * (sy*width + sx)
			dst[di] = src[0]
			dst[di+1] = src[1]
			dst[di+2] = src[2]
			dst[di+3] = src[3]
			if flags == dxt3 || flags == dxt5 {
				dst[di+3] = alpha[i]
			}
		}
	}
}

func decodeDXTColours(block []byte, isDXT1 bool) [4][4]byte {
	a := binary.LittleEndian.Uint16(block[0:])
	b := binary.LittleEndian.Uint16(block[2:])
	var colours [4][4]byte
	colours[0] = rgb565(a, 255)
	colours[1] = rgb565(b, 255)
	if isDXT1 && a <= b {
		colours[2] = [4]byte{
			byte((uint16(colours[0][0]) + uint16(colours[1][0])) / 2),
			byte((uint16(colours[0][1]) + uint16(colours[1][1])) / 2),
			byte((uint16(colours[0][2]) + uint16(colours[1][2])) / 2),
			255,
		}
		colours[3] = [4]byte{0, 0, 0, 0}
	} else {
		colours[2] = [4]byte{
			byte((2*uint16(colours[0][0]) + uint16(colours[1][0])) / 3),
			byte((2*uint16(colours[0][1]) + uint16(colours[1][1])) / 3),
			byte((2*uint16(colours[0][2]) + uint16(colours[1][2])) / 3),
			255,
		}
		colours[3] = [4]byte{
			byte((uint16(colours[0][0]) + 2*uint16(colours[1][0])) / 3),
			byte((uint16(colours[0][1]) + 2*uint16(colours[1][1])) / 3),
			byte((uint16(colours[0][2]) + 2*uint16(colours[1][2])) / 3),
			255,
		}
	}
	return colours
}

func rgb565(value uint16, alpha byte) [4]byte {
	r := (value >> 11) & 0x1F
	g := (value >> 5) & 0x3F
	b := value & 0x1F
	return [4]byte{
		byte((r << 3) | (r >> 2)),
		byte((g << 2) | (g >> 4)),
		byte((b << 3) | (b >> 2)),
		alpha,
	}
}

func decodeDXTAlpha(block []byte, flags int) [16]byte {
	var alpha [16]byte
	for i := range alpha {
		alpha[i] = 255
	}
	switch flags {
	case dxt3:
		for i := 0; i < 8; i++ {
			quant := block[i]
			low := quant & 0x0F
			high := (quant & 0xF0) >> 4
			alpha[i*2] = low | (low << 4)
			alpha[i*2+1] = high | (high << 4)
		}
	case dxt5:
		a0 := block[0]
		a1 := block[1]
		table := [8]byte{a0, a1}
		if a0 <= a1 {
			for i := 1; i < 5; i++ {
				table[i+1] = byte(((5-i)*int(a0) + i*int(a1)) / 5)
			}
			table[6] = 0
			table[7] = 255
		} else {
			for i := 1; i < 7; i++ {
				table[i+1] = byte(((7-i)*int(a0) + i*int(a1)) / 7)
			}
		}
		var bits uint64
		for i := 0; i < 6; i++ {
			bits |= uint64(block[2+i]) << uint(8*i)
		}
		for i := 0; i < 16; i++ {
			alpha[i] = table[(bits>>uint(3*i))&0x07]
		}
	}
	return alpha
}

type ExportPNGResult struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
	Mipmap int    `json:"mipmap"`
	Hash   string `json:"hash"`
}

func (img *BLPImage) ExportPNG(outputPath string, mipmap int) (*ExportPNGResult, error) {
	rgba, w, h, err := img.Image(mipmap)
	if err != nil {
		return nil, err
	}

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

func (img *BLPImage) Image(mipmap int) (*image.NRGBA, int, int, error) {
	pixels, w, h, err := img.GetMipmap(mipmap)
	if err != nil {
		return nil, 0, 0, err
	}

	rgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(rgba.Pix, pixels)
	return rgba, w, h, nil
}

func (img *BLPImage) ExportWebP(outputPath string, mipmap int) (*ExportPNGResult, error) {
	rgba, w, h, err := img.Image(mipmap)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(outputPath)
	if dir != "" {
		os.MkdirAll(dir, 0755)
	}

	var buf bytes.Buffer
	if err := nativewebp.Encode(&buf, rgba, &nativewebp.Options{CompressionLevel: nativewebp.BestCompression}); err != nil {
		return nil, err
	}
	webpBytes := buf.Bytes()

	if err := os.WriteFile(outputPath, webpBytes, 0644); err != nil {
		return nil, err
	}

	hash := md5.Sum(webpBytes)
	return &ExportPNGResult{
		Path:   outputPath,
		Width:  w,
		Height: h,
		Format: "webp",
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
