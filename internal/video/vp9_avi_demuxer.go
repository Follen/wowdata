package video

import (
	"encoding/binary"
	"fmt"
	"io"
)

type FrameInfo struct {
	Type      string `json:"type"`
	Timestamp float64 `json:"timestamp"`
	Duration  float64 `json:"duration"`
	Size      int    `json:"size"`
}

type VP9AVIDemuxer struct {
	data       []byte
	width      int
	height     int
	frameRate  float64
	frameCount int
}

func NewVP9AVIDemuxer(data []byte) *VP9AVIDemuxer {
	return &VP9AVIDemuxer{
		data:      data,
		frameRate: 30.0,
	}
}

func (d *VP9AVIDemuxer) ParseHeader() error {
	if len(d.data) < 12 {
		return fmt.Errorf("data too short for AVI header")
	}

	if string(d.data[0:4]) != "RIFF" {
		return fmt.Errorf("not a RIFF/AVI file")
	}
	if string(d.data[8:12]) != "AVI " {
		return fmt.Errorf("not an AVI file")
	}

	d.width = 0
	d.height = 0

	// Parse all LIST chunks recursively to find avih and strf
	d.parseListChunk(12, len(d.data))
	return nil
}

func (d *VP9AVIDemuxer) parseListChunk(start, end int) {
	pos := start
	for pos+8 <= end {
		fourCC := string(d.data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(d.data[pos+4 : pos+8]))

		switch fourCC {
		case "LIST":
			listType := ""
			if pos+8+4 <= len(d.data) {
				listType = string(d.data[pos+8 : pos+12])
			}
			if pos+12 <= end {
				subEnd := pos + 8 + size
				if subEnd > end {
					subEnd = end
				}
				_ = listType
				d.parseListChunk(pos+12, subEnd)
			}
		case "avih":
			if pos+8+32 <= len(d.data) {
				usPerFrame := binary.LittleEndian.Uint32(d.data[pos+8+24 : pos+8+28])
				if usPerFrame > 0 {
					d.frameRate = 1000000.0 / float64(usPerFrame)
				}
			}
		case "strf":
			if size >= 40 && pos+8+40 <= len(d.data) {
				d.width = int(binary.LittleEndian.Uint32(d.data[pos+8+4 : pos+8+8]))
				d.height = int(binary.LittleEndian.Uint32(d.data[pos+8+8 : pos+8+12]))
			}
		}

		skip := size
		if skip%2 == 1 {
			skip++
		}
		pos += 8 + skip
	}
}

func (d *VP9AVIDemuxer) ExtractFrames() ([]FrameInfo, error) {
	var frames []FrameInfo

	// Find movi chunk
	pos := 12
	moviFound := false
	for pos+8 <= len(d.data) {
		fourCC := string(d.data[pos : pos+4])
		size := binary.LittleEndian.Uint32(d.data[pos+4 : pos+8])

		if fourCC == "movi" {
			moviFound = true
			pos += 8
			d.parseFrameData(&frames, pos, int(size))
			break
		}

		skip := int(size)
		if skip%2 == 1 {
			skip++
		}
		pos += 8 + skip
	}

	if !moviFound {
		return nil, fmt.Errorf("movi chunk not found")
	}

	d.frameCount = len(frames)
	return frames, nil
}

func (d *VP9AVIDemuxer) parseFrameData(frames *[]FrameInfo, startPos, chunkSize int) {
	endPos := startPos + chunkSize
	if endPos > len(d.data) {
		endPos = len(d.data)
	}

	pos := startPos
	timestamp := 0.0
	frameDuration := 1.0 / d.frameRate

	for pos+8 <= endPos {
		fourCC := string(d.data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(d.data[pos+4 : pos+8]))

		if fourCC == "00dc" || fourCC == "00db" {
			fType := "delta"
			if fourCC == "00db" {
				fType = "key"
			}

			*frames = append(*frames, FrameInfo{
				Type:      fType,
				Timestamp: timestamp,
				Duration:  frameDuration,
				Size:      size,
			})
			timestamp += frameDuration
		}

		skip := size
		if skip%2 == 1 {
			skip++
		}
		pos += 8 + skip
	}
}

func (d *VP9AVIDemuxer) FrameCount() int { return d.frameCount }

func (d *VP9AVIDemuxer) GetDimensions() (int, int) { return d.width, d.height }

func (d *VP9AVIDemuxer) FrameRate() float64 { return d.frameRate }

func FindChunk(data []byte, fourCC string) int {
	target := []byte(fourCC)
	for i := 0; i <= len(data)-len(target); i++ {
		if string(data[i:i+len(target)]) == fourCC {
			return i
		}
	}
	return -1
}

func ReadAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}
