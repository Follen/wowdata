package video

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func buildMinimalAVI() []byte {
	buf := new(bytes.Buffer)

	// RIFF header
	buf.Write([]byte("RIFF"))
	riffSizePos := buf.Len()
	binary.Write(buf, binary.LittleEndian, uint32(0)) // placeholder
	buf.Write([]byte("AVI "))

	// LIST hdrl
	buf.Write([]byte("LIST"))
	hdrlSizePos := buf.Len()
	binary.Write(buf, binary.LittleEndian, uint32(0)) // placeholder
	buf.Write([]byte("hdrl"))

	// avih
	buf.Write([]byte("avih"))
	binary.Write(buf, binary.LittleEndian, uint32(56))
	avih := make([]byte, 56)
	binary.LittleEndian.PutUint32(avih[24:], 33333) // usPerFrame (~30fps)
	binary.LittleEndian.PutUint32(avih[48:], 1)      // streams count
	buf.Write(avih)

	// Fixup hdrl size
	hdrlEnd := buf.Len()
	binary.LittleEndian.PutUint32(buf.Bytes()[hdrlSizePos:], uint32(hdrlEnd-hdrlSizePos-4))

	// LIST strl
	buf.Write([]byte("LIST"))
	strlSizePos := buf.Len()
	binary.Write(buf, binary.LittleEndian, uint32(0)) // placeholder
	buf.Write([]byte("strl"))

	// strf
	buf.Write([]byte("strf"))
	binary.Write(buf, binary.LittleEndian, uint32(40))
	strf := make([]byte, 40)
	binary.LittleEndian.PutUint32(strf[4:], 1920)  // width
	binary.LittleEndian.PutUint32(strf[8:], 1080)  // height
	copy(strf[16:], "VP90")
	buf.Write(strf)

	// Fixup strl size
	strlEnd := buf.Len()
	binary.LittleEndian.PutUint32(buf.Bytes()[strlSizePos:], uint32(strlEnd-strlSizePos-4))

	// movi
	buf.Write([]byte("movi"))
	moviSizePos := buf.Len()
	binary.Write(buf, binary.LittleEndian, uint32(0)) // placeholder

	// Frame 1: keyframe
	keyframeData := make([]byte, 100)
	binary.Write(buf, binary.LittleEndian, [4]byte{'0', '0', 'd', 'b'})
	binary.Write(buf, binary.LittleEndian, uint32(100))
	buf.Write(keyframeData)

	// Word align: 100 + 8 = 108, which is even, no padding needed

	// Frame 2: delta
	deltaData := make([]byte, 80)
	binary.Write(buf, binary.LittleEndian, [4]byte{'0', '0', 'd', 'c'})
	binary.Write(buf, binary.LittleEndian, uint32(80))
	buf.Write(deltaData)

	// Fixup movi size
	moviEnd := buf.Len()
	binary.LittleEndian.PutUint32(buf.Bytes()[moviSizePos:], uint32(moviEnd-moviSizePos-4))

	// Fixup RIFF size
	result := buf.Bytes()
	binary.LittleEndian.PutUint32(result[riffSizePos:], uint32(len(result)-8))

	return result
}

func TestParseHeader(t *testing.T) {
	data := buildMinimalAVI()
	d := NewVP9AVIDemuxer(data)

	if err := d.ParseHeader(); err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}

	w, h := d.GetDimensions()
	if w != 1920 || h != 1080 {
		t.Fatalf("dimensions = %dx%d", w, h)
	}
	if d.FrameRate() < 29.9 || d.FrameRate() > 30.1 {
		t.Fatalf("frameRate = %f", d.FrameRate())
	}
}

func TestExtractFrames(t *testing.T) {
	data := buildMinimalAVI()
	d := NewVP9AVIDemuxer(data)
	d.ParseHeader()

	frames, err := d.ExtractFrames()
	if err != nil {
		t.Fatalf("ExtractFrames: %v", err)
	}

	if len(frames) != 2 {
		t.Fatalf("frame count = %d, want 2", len(frames))
	}
	if frames[0].Type != "key" {
		t.Fatalf("frame 0 type = %s, want key", frames[0].Type)
	}
	if frames[1].Type != "delta" {
		t.Fatalf("frame 1 type = %s, want delta", frames[1].Type)
	}
}

func TestInvalidHeader(t *testing.T) {
	d := NewVP9AVIDemuxer([]byte("not an avi file at all"))
	if err := d.ParseHeader(); err == nil {
		t.Fatal("expected error for invalid data")
	}
}

func TestFindChunk(t *testing.T) {
	data := []byte("xxxxxxxxRIFFyyyy")
	pos := FindChunk(data, "RIFF")
	if pos != 8 {
		t.Fatalf("FindChunk = %d, want 8", pos)
	}
	if FindChunk(data, "NOPE") != -1 {
		t.Fatal("expected -1 for missing chunk")
	}
}
