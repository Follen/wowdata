package casc

import (
	"encoding/binary"
	"testing"

	"wowdata/internal/shared/blte"
)

func buildEncodingData(cKeys map[string]struct {
	eKey string
	size int64
}) []byte {
	data := make([]byte, 2000)
	binary.LittleEndian.PutUint16(data, encMagic)
	data[2] = 1                             // version
	data[3] = 16                            // hashSizeCKey
	data[4] = 16                            // hashSizeEKey
	binary.BigEndian.PutUint16(data[5:], 4) // cKeyPageSize = 4KB
	// eKeyPageSize at 7
	binary.BigEndian.PutUint32(data[9:], 1) // cKeyPageCount = 1
	// eKeyPageCount + unk11 at 13
	binary.BigEndian.PutUint32(data[18:], 0) // specBlockSize = 0

	// pagesStart = 22 + 0 + 1*(16+16) = 54
	pos := 54

	for cKeyHex, val := range cKeys {
		cKey, _ := hexDecode(cKeyHex)
		eKey, _ := hexDecode(val.eKey)

		if pos+1+5+16+16 >= len(data) {
			break
		}
		data[pos] = 1
		pos++
		data[pos] = byte(val.size >> 32)
		binary.BigEndian.PutUint32(data[pos+1:], uint32(val.size))
		pos += 5
		copy(data[pos:], cKey)
		pos += 16
		copy(data[pos:], eKey)
		pos += 16
	}

	return data[:pos]
}

func hexDecode(s string) ([]byte, error) {
	data := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b := uint8(0)
		for _, c := range s[i : i+2] {
			b <<= 4
			switch {
			case c >= '0' && c <= '9':
				b |= uint8(c - '0')
			case c >= 'a' && c <= 'f':
				b |= uint8(c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				b |= uint8(c - 'A' + 10)
			}
		}
		data[i/2] = b
	}
	return data, nil
}

func TestParseEncodingFile(t *testing.T) {
	c := NewCASCSource()
	cKeys := map[string]struct {
		eKey string
		size int64
	}{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": {"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 1024},
		"cccccccccccccccccccccccccccccccc": {"dddddddddddddddddddddddddddddddd", 2048},
	}
	raw := buildEncodingData(cKeys)

	if err := c.parseEncoding(raw); err != nil {
		t.Fatalf("parseEncoding: %v", err)
	}

	ek, err := c.GetEncodingKeyForContentKey("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("GetEncodingKey: %v", err)
	}
	if ek != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("encoding key = %s", ek)
	}
	if sz := c.GetEncodingSizeForContentKey("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); sz != 1024 {
		t.Fatalf("size = %d", sz)
	}
}

func TestFileExists(t *testing.T) {
	c := NewCASCSource()
	c.RootEntries[100] = []RootEntry{{TypeIndex: 0, ContentKey: "aaaa"}}
	c.RootTypes = append(c.RootTypes, RootType{LocaleFlags: LocaleEnUS, ContentFlags: 0})
	c.Locale = LocaleEnUS

	if !c.FileExists(100) {
		t.Fatal("file 100 should exist")
	}
	if c.FileExists(200) {
		t.Fatal("file 200 should not exist")
	}
}

func TestGetFile(t *testing.T) {
	c := NewCASCSource()
	c.RootEntries[100] = []RootEntry{{TypeIndex: 0, ContentKey: "contentKeyA"}}
	c.EncodingEntries["contentKeyA"] = EncodingEntry{Key: "encodingKeyB", Size: 42}
	c.Archives["encodingKeyB"] = ArchiveEntry{Key: "archive", Size: 10, Offset: 5}
	c.RootTypes = append(c.RootTypes, RootType{LocaleFlags: LocaleEnUS, ContentFlags: 0})
	c.Locale = LocaleEnUS

	ek, err := c.GetFile(100)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if ek != "encodingKeyB" {
		t.Fatalf("encoding key = %s", ek)
	}

	info, err := c.GetFileEncodingInfo(100)
	if err != nil {
		t.Fatalf("GetFileEncodingInfo: %v", err)
	}
	if info.ContentKey != "contentKeyA" || info.EncodingKey != "encodingKeyB" || info.Size != 42 {
		t.Fatalf("encoding info = %#v", info)
	}
	if info.Archive == nil || info.Archive.Key != "archive" || info.Archive.Offset != 5 || info.Archive.Length != 10 {
		t.Fatalf("archive info = %#v", info.Archive)
	}
}

func TestLocaleFilterLowViolence(t *testing.T) {
	c := NewCASCSource()
	c.RootEntries[100] = []RootEntry{
		{TypeIndex: 0, ContentKey: "keyA"},
		{TypeIndex: 1, ContentKey: "keyB"},
	}
	c.RootTypes = append(c.RootTypes,
		RootType{LocaleFlags: LocaleEnUS, ContentFlags: ContentLowViolence},
		RootType{LocaleFlags: LocaleEnUS, ContentFlags: 0},
	)
	c.Locale = LocaleEnUS

	if !c.FileExists(100) {
		t.Fatal("file 100 should exist via type 1 (non-LowViolence)")
	}
}

func TestParseArchiveIndex(t *testing.T) {
	c := NewCASCSource()
	// Build a simple archive index with 2 entries
	data := make([]byte, 12+2*24)
	// Entry 1
	copy(data[0:16], []byte{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA})
	binary.BigEndian.PutUint32(data[16:], 1024) // size
	binary.BigEndian.PutUint32(data[20:], 0)    // offset
	// Entry 2
	copy(data[24:], []byte{0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB})
	binary.BigEndian.PutUint32(data[40:], 2048)
	binary.BigEndian.PutUint32(data[44:], 0x1000)
	// Count at end
	binary.LittleEndian.PutUint32(data[len(data)-12:], 2)

	c.ParseArchiveIndex(data, "archive_key")

	if len(c.Archives) != 2 {
		t.Fatalf("archives count = %d, want 2", len(c.Archives))
	}
}

func TestFormatCDNKey(t *testing.T) {
	key := "49299eae4e3a195953764bb4adb3c91f"
	result := FormatCDNKey(key)
	expected := "49/29/49299eae4e3a195953764bb4adb3c91f"
	if result != expected {
		t.Fatalf("FormatCDNKey = %s, want %s", result, expected)
	}
}

func TestVersionConfigParsing(t *testing.T) {
	data := "Name!STRING:0|Region!STRING:0|Version!STRING:0\n"
	data += "WOW|cn|9.2.5.44170\n"
	data += "WOW|us|9.2.5.44171\n"

	entries := ParseVersionConfig(data)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Product != "WOW" {
		t.Fatal("Product mismatch")
	}
	if entries[0].Region != "cn" {
		t.Fatal("Region mismatch")
	}
	if entries[0].Version != "9.2.5.44170" {
		t.Fatal("Version mismatch")
	}
}

func TestCDNConfigParsing(t *testing.T) {
	data := "# CDN Configuration\n"
	data += "archives = archive1 archive2\n"
	data += "archive-group = wow\n"

	cfg, err := ParseCDNConfig(data)
	if err != nil {
		t.Fatalf("ParseCDNConfig: %v", err)
	}
	if cfg["archives"] != "archive1 archive2" {
		t.Fatalf("archives = %s", cfg["archives"])
	}
	if cfg["archiveGroup"] != "wow" {
		t.Fatalf("archiveGroup = %s", cfg["archiveGroup"])
	}
}

func TestCDNConfigInvalidHeader(t *testing.T) {
	_, err := ParseCDNConfig("no header\nkey = value\n")
	if err == nil {
		t.Fatal("expected error for missing header")
	}
}

var _ = blte.NewReader
