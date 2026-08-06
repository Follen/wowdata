package casc

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
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
	if len(local.Builds) != 2 {
		t.Fatalf("build count = %d", len(local.Builds))
	}
	if local.Builds[0].Product != "wow" || local.Builds[0].Branch != "retail" {
		t.Fatalf("build = %#v", local.Builds[0])
	}
	if local.Builds[0].BuildConfig != "buildkey1234567890abcdef12345678" || local.Builds[0].CDNConfig != "cdnkey1234567890abcdef1234567890" {
		t.Fatalf("config keys = %q/%q", local.Builds[0].BuildConfig, local.Builds[0].CDNConfig)
	}
	products := local.GetProductList()
	if len(products) != 2 || products[0].BuildIndex != 0 || !strings.Contains(products[0].Label, "World of Warcraft") {
		t.Fatalf("products = %#v", products)
	}
	if products[1].Product != "unknown" || !strings.Contains(products[1].Label, "unknown") {
		t.Fatalf("unknown product should be preserved: %#v", products[1])
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

func TestCASCLocalReadEncodingDataIndexesOnDemand(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "Data", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const wanted = "aabbccddeeff00112233445566778899"
	const unrelated = "00112233445566778899aabbccddeeff"
	if err := os.WriteFile(filepath.Join(dataDir, "00.idx"), buildLocalIndexFixture(unrelated[:18], 0, 16, 0x1e+3), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "01.idx"), buildLocalIndexFixture(wanted[:18], 1, 32, 0x1e+5), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := make([]byte, 32+0x1e+5)
	copy(archive[32+0x1e:], []byte("BLTE!"))
	if err := os.WriteFile(filepath.Join(dataDir, "data.001"), archive, 0o644); err != nil {
		t.Fatal(err)
	}

	local := NewCASCLocal(dir)
	if err := local.discoverIndexPaths(); err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{strings.ToUpper(wanted), wanted} {
		got, err := local.ReadEncodingData(key)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if string(got) != "BLTE!" {
			t.Fatalf("read %d = %q", i, got)
		}
	}
	if len(local.LocalIndexes) != 1 {
		t.Fatalf("retained local indexes = %d, want only requested key", len(local.LocalIndexes))
	}
	if _, ok := local.LocalIndexes[unrelated[:18]]; ok {
		t.Fatal("unrelated local index entry was retained")
	}
}

func TestCASCLocalReadFileDataDecodesBLTE(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "Data", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const encodingKey = "00112233445566778899aabbccddeeff"
	const contentKey = "ffeeddccbbaa99887766554433221100"
	payload := []byte("WDC5 decoded payload")
	raw := buildCascTestBLTE(payload)
	indexData := buildLocalIndexFixture(encodingKey[:18], 0, 64, 0x1e+len(raw))
	indexPath := filepath.Join(dataDir, "test.idx")
	if err := os.WriteFile(indexPath, indexData, 0o644); err != nil {
		t.Fatal(err)
	}
	archive := make([]byte, 64+0x1e+len(raw))
	copy(archive[64+0x1e:], raw)
	if err := os.WriteFile(filepath.Join(dataDir, "data.000"), archive, 0o644); err != nil {
		t.Fatal(err)
	}

	local := NewCASCLocal(dir)
	local.RootTypes = []RootType{{LocaleFlags: LocaleZhCN}}
	local.RootEntries[123] = []RootEntry{{ContentKey: contentKey}}
	local.EncodingEntries[contentKey] = EncodingEntry{Key: encodingKey}
	if err := local.ParseIndex(indexPath); err != nil {
		t.Fatal(err)
	}
	for name, read := range map[string]func(uint32) ([]byte, error){
		"full":    local.ReadFileData,
		"partial": local.ReadFileDataPartial,
	} {
		got, err := read(123)
		if err != nil {
			t.Fatalf("%s read: %v", name, err)
		}
		if string(got) != string(payload) {
			t.Fatalf("%s read = %q, want %q", name, got, payload)
		}
	}
}

func TestCASCLocalLoadMetadataSkipsIndexesRootAndEncoding(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "Data", "config")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildKey := "00112233445566778899aabbccddeeff"
	cdnKey := "ffeeddccbbaa99887766554433221100"
	buildInfo := "Product!STRING:0|Branch!STRING:0|Version!STRING:0|Build Key!HEX:16|CDN Key!HEX:16\n" +
		"wow|retail|12.0.7.68974|" + buildKey + "|" + cdnKey + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".build.info"), []byte(buildInfo), 0o644); err != nil {
		t.Fatal(err)
	}
	local := NewCASCLocal(dir)
	if err := os.MkdirAll(filepath.Dir(local.FormatConfigPath(buildKey)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(local.FormatConfigPath(cdnKey)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local.FormatConfigPath(buildKey), []byte("# Build config\nroot = abcdef\nencoding = a b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local.FormatConfigPath(cdnKey), []byte("# CDN config\narchives = archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := local.Init(); err != nil {
		t.Fatal(err)
	}
	if err := local.LoadMetadata(0); err != nil {
		t.Fatal(err)
	}
	if local.Build == nil || local.GetBuildName() != "12.0.7.68974" {
		t.Fatalf("build not selected: %#v", local.Build)
	}
	if len(local.LocalIndexes) != 0 || len(local.EncodingEntries) != 0 || len(local.RootEntries) != 0 {
		t.Fatalf("metadata-only loaded data indexes=%d encoding=%d root=%d", len(local.LocalIndexes), len(local.EncodingEntries), len(local.RootEntries))
	}
}

func TestCASCLocalLoadMetadataAllowsMissingCDNConfig(t *testing.T) {
	dir := t.TempDir()
	buildKey := "00112233445566778899aabbccddeeff"
	cdnKey := "ffeeddccbbaa99887766554433221100"
	buildInfo := "Product!STRING:0|Branch!STRING:0|Version!STRING:0|Build Key!HEX:16|CDN Key!HEX:16\n" +
		"wow|retail|12.0.7.68974|" + buildKey + "|" + cdnKey + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".build.info"), []byte(buildInfo), 0o644); err != nil {
		t.Fatal(err)
	}
	local := NewCASCLocal(dir)
	if err := os.MkdirAll(filepath.Dir(local.FormatConfigPath(buildKey)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local.FormatConfigPath(buildKey), []byte("# Build config\nroot = abcdef\nencoding = a b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := local.Init(); err != nil {
		t.Fatal(err)
	}
	if err := local.LoadMetadata(0); err != nil {
		t.Fatalf("metadata-only load with absent CDN config: %v", err)
	}
	if local.Build == nil || local.GetBuildName() != "12.0.7.68974" {
		t.Fatalf("build not selected: %#v", local.Build)
	}
}

func TestCASCLocalLoadSelectedResolvesOnlyRequestedFile(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "Data", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		buildKey          = "01010101010101010101010101010101"
		cdnKey            = "02020202020202020202020202020202"
		encodingKey       = "11111111111111111111111111111111"
		rootContentKey    = "22222222222222222222222222222222"
		rootEncodingKey   = "33333333333333333333333333333333"
		targetContentKey  = "44444444444444444444444444444444"
		targetEncodingKey = "55555555555555555555555555555555"
		secondContentKey  = "66666666666666666666666666666666"
		secondEncodingKey = "77777777777777777777777777777777"
		targetID          = uint32(123)
		secondID          = uint32(456)
	)
	buildInfo := "Product!STRING:0|Branch!STRING:0|Version!STRING:0|Build Key!HEX:16|CDN Key!HEX:16\n" +
		"wow|retail|12.0.7.68974|" + buildKey + "|" + cdnKey + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".build.info"), []byte(buildInfo), 0o644); err != nil {
		t.Fatal(err)
	}
	local := NewCASCLocal(dir)
	for key, config := range map[string]string{
		buildKey: "# Build config\nroot = " + rootContentKey + "\nencoding = ignored " + encodingKey + "\n",
		cdnKey:   "# CDN config\narchives = 00000000000000000000000000000000\n",
	} {
		path := local.FormatConfigPath(key)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	encodingRaw := buildEncodingData(map[string]struct {
		eKey string
		size int64
	}{
		rootContentKey:   {eKey: rootEncodingKey, size: 68},
		targetContentKey: {eKey: targetEncodingKey, size: 7},
		secondContentKey: {eKey: secondEncodingKey, size: 14},
	})
	rootRaw := make([]byte, 4+8+8+48)
	binary.LittleEndian.PutUint32(rootRaw, 2)
	binary.LittleEndian.PutUint32(rootRaw[8:], uint32(LocaleZhCN))
	binary.LittleEndian.PutUint32(rootRaw[12:], targetID)
	binary.LittleEndian.PutUint32(rootRaw[16:], secondID-targetID-1)
	copy(rootRaw[20:], mustHexBytes(targetContentKey))
	copy(rootRaw[44:], mustHexBytes(secondContentKey))
	objects := []struct {
		key    string
		offset int
		data   []byte
	}{
		{key: encodingKey, offset: 0, data: buildLocalRangedBLTE(encodingRaw)},
		{key: rootEncodingKey, offset: 4096, data: buildLocalRangedBLTE(rootRaw)},
		{key: targetEncodingKey, offset: 8192, data: buildCascTestBLTE([]byte("payload"))},
		{key: secondEncodingKey, offset: 12288, data: buildCascTestBLTE([]byte("second-payload"))},
	}
	archive := make([]byte, 16384)
	for index, object := range objects {
		copy(archive[object.offset+0x1e:], object.data)
		indexData := buildLocalIndexFixture(object.key[:18], 0, object.offset, 0x1e+len(object.data))
		if err := os.WriteFile(filepath.Join(dataDir, fmt.Sprintf("%02d.idx", index)), indexData, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dataDir, "data.000"), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := local.Init(); err != nil {
		t.Fatal(err)
	}
	if err := local.LoadSelected(0, []uint32{targetID}); err != nil {
		t.Fatal(err)
	}
	if len(local.RootEntries) != 1 || len(local.EncodingEntries) != 2 {
		t.Fatalf("selected metadata root=%#v encoding=%#v", local.RootEntries, local.EncodingEntries)
	}
	if len(local.LocalIndexes) != 2 {
		t.Fatalf("metadata index probes = %d, want encoding and root only", len(local.LocalIndexes))
	}
	payload, err := local.ReadFileData(targetID)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "payload" || len(local.LocalIndexes) != 3 {
		t.Fatalf("payload=%q retained indexes=%d", payload, len(local.LocalIndexes))
	}
	if err := local.EnsureFiles([]uint32{targetID, secondID, secondID}); err != nil {
		t.Fatal(err)
	}
	if len(local.RootEntries) != 2 || len(local.EncodingEntries) != 3 {
		t.Fatalf("incremental metadata root=%#v encoding=%#v", local.RootEntries, local.EncodingEntries)
	}
	secondPayload, err := local.ReadFileData(secondID)
	if err != nil {
		t.Fatal(err)
	}
	if string(secondPayload) != "second-payload" || len(local.LocalIndexes) != 4 {
		t.Fatalf("second payload=%q retained indexes=%d", secondPayload, len(local.LocalIndexes))
	}
}

func buildLocalRangedBLTE(payload []byte) []byte {
	raw := append([]byte{0x4e}, payload...)
	headerSize := 12 + 24
	data := make([]byte, headerSize, headerSize+len(raw))
	copy(data, "BLTE")
	binary.BigEndian.PutUint32(data[4:], uint32(headerSize))
	data[8], data[11] = 0x0f, 1
	binary.BigEndian.PutUint32(data[12:], uint32(len(raw)))
	binary.BigEndian.PutUint32(data[16:], uint32(len(payload)))
	hash := md5.Sum(raw)
	copy(data[20:], hash[:])
	return append(data, raw...)
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
