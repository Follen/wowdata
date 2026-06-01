package casc

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCASCLocalInitReadsBuildInfo(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "Data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	buildInfo := strings.Join([]string{
		"Product!STRING:0|Branch!STRING:0|Version!STRING:0|Build Key!HEX:16|CDN Key!HEX:16",
		"wow|retail|12.0.1.12345|buildkey1234567890abcdef12345678|cdnkey1234567890abcdef1234567890",
		"unknown|branch|1.0|badbuild|badcdn",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, ".build.info"), []byte(buildInfo), 0644); err != nil {
		t.Fatal(err)
	}

	local := NewCASCLocal(dir)
	if err := local.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(local.Builds) != 1 {
		t.Fatalf("build count = %d", len(local.Builds))
	}
	if local.Builds[0].Product != "wow" || local.Builds[0].Branch != "retail" {
		t.Fatalf("build = %#v", local.Builds[0])
	}
	if local.Builds[0].BuildConfig != "buildkey1234567890abcdef12345678" || local.Builds[0].CDNConfig != "cdnkey1234567890abcdef1234567890" {
		t.Fatalf("config keys = %q/%q", local.Builds[0].BuildConfig, local.Builds[0].CDNConfig)
	}
	products := local.GetProductList()
	if len(products) != 1 || products[0].BuildIndex != 0 || !strings.Contains(products[0].Label, "World of Warcraft") {
		t.Fatalf("products = %#v", products)
	}
}

func mustHexBytes(value string) []byte {
	data, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	return data
}

func TestCASCLocalParseIndexAndReadData(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "Data", "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	encodingKey := "00112233445566778899aabbccddeeff"
	indexData := buildLocalIndexFixture(encodingKey[:18], 0, 64, 0x1e+5)
	indexPath := filepath.Join(dataDir, "test.idx")
	if err := os.WriteFile(indexPath, indexData, 0644); err != nil {
		t.Fatal(err)
	}
	archive := make([]byte, 64+0x1e+5)
	copy(archive[64+0x1e:], []byte("BLTE!"))
	if err := os.WriteFile(filepath.Join(dataDir, "data.000"), archive, 0644); err != nil {
		t.Fatal(err)
	}

	local := NewCASCLocal(dir)
	if err := local.ParseIndex(indexPath); err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	raw, err := local.ReadEncodingData(encodingKey)
	if err != nil {
		t.Fatalf("ReadEncodingData: %v", err)
	}
	if string(raw) != "BLTE!" {
		t.Fatalf("raw = %q", raw)
	}
}

func TestCASCLocalReadFileDataDecodesBLTE(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "Data", "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	encodingKey := "00112233445566778899aabbccddeeff"
	indexData := buildLocalIndexFixture(encodingKey[:18], 0, 64, 0x1e+len(buildCascTestBLTE([]byte("WDC5 payload"))))
	indexPath := filepath.Join(dataDir, "test.idx")
	if err := os.WriteFile(indexPath, indexData, 0644); err != nil {
		t.Fatal(err)
	}
	archive := make([]byte, 64+0x1e+len(buildCascTestBLTE([]byte("WDC5 payload"))))
	copy(archive[64+0x1e:], buildCascTestBLTE([]byte("WDC5 payload")))
	if err := os.WriteFile(filepath.Join(dataDir, "data.000"), archive, 0644); err != nil {
		t.Fatal(err)
	}

	local := NewCASCLocal(dir)
	local.RootEntries[10] = []RootEntry{{TypeIndex: 0, ContentKey: "content"}}
	local.RootTypes = append(local.RootTypes, RootType{LocaleFlags: LocaleEnUS})
	local.EncodingEntries["content"] = EncodingEntry{Key: encodingKey}
	local.Locale = LocaleEnUS
	if err := local.ParseIndex(indexPath); err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	data, err := local.ReadFileData(10)
	if err != nil {
		t.Fatalf("ReadFileData: %v", err)
	}
	if string(data) != "WDC5 payload" {
		t.Fatalf("data = %q", data)
	}
}

func buildLocalIndexFixture(keyPrefix string, archiveIndex int, offset int, size int) []byte {
	data := make([]byte, 0x20+18)
	binary.LittleEndian.PutUint32(data[0:], 0)
	binary.LittleEndian.PutUint32(data[0x10:], 18)
	copy(data[0x18:], mustHexBytes(keyPrefix))
	idxLow := uint32(offset)
	idxHigh := byte(archiveIndex >> 2)
	idxLow |= uint32(archiveIndex&0x3) << 30
	data[0x18+9] = idxHigh
	binary.BigEndian.PutUint32(data[0x18+10:], idxLow)
	binary.LittleEndian.PutUint32(data[0x18+14:], uint32(size))
	return data
}
