package listfile

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestLoadListfile(t *testing.T) {
	data := "1;world/art/test.blp\n2;sound/music/main.mp3\n3;interface/icons/sword.blp\n"
	lf := New()
	if err := lf.Load(strings.NewReader(data)); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !lf.IsLoaded() {
		t.Fatal("not loaded")
	}
}

func TestGetByID(t *testing.T) {
	data := "1;world/art/test.blp\n2;sound/music/main.mp3\n"
	lf := New()
	lf.Load(strings.NewReader(data))

	if name := lf.GetByID(1); name != "world/art/test.blp" {
		t.Fatalf("got %q", name)
	}
	if name := lf.GetByID(999); name != "" {
		t.Fatal("expected empty for unknown ID")
	}
}

func TestGetByFilename(t *testing.T) {
	data := "1;world/art/test.blp\n2;sound/music/main.mp3\n"
	lf := New()
	lf.Load(strings.NewReader(data))

	if id := lf.GetByFilename("world/art/test.blp"); id != 1 {
		t.Fatalf("got %d", id)
	}
	if id := lf.GetByFilename("WORLD/ART/TEST.BLP"); id != 1 {
		t.Fatalf("case-insensitive: got %d", id)
	}
}

func TestGetFilenamesByExtension(t *testing.T) {
	data := "1;world/art/test.blp\n2;sound/music/main.mp3\n3;interface/icons/sword.blp\n"
	lf := New()
	lf.Load(strings.NewReader(data))

	blps := lf.GetFilenamesByExtension(".blp")
	if len(blps) != 2 {
		t.Fatalf("got %d blp files", len(blps))
	}

	mp3s := lf.GetFilenamesByExtension("mp3")
	if len(mp3s) != 1 {
		t.Fatalf("got %d mp3 files", len(mp3s))
	}
}

func TestGetFilteredEntries(t *testing.T) {
	data := "1;world/art/test.blp\n2;sound/music/main.mp3\n3;interface/icons/sword.blp\n"
	lf := New()
	lf.Load(strings.NewReader(data))

	results := lf.GetFilteredEntries("sword")
	if len(results) != 1 {
		t.Fatalf("got %d results for 'sword'", len(results))
	}

	results = lf.GetFilteredEntries("WORLD")
	if len(results) != 1 {
		t.Fatalf("got %d results for 'WORLD'", len(results))
	}
}

func TestFilterIDsPreservesLegacyOrder(t *testing.T) {
	data := "3;z/test.blp\n1;a/test.blp\n2;b/skip.blp\n"
	lf := New()
	lf.Load(strings.NewReader(data))
	lf.FilterIDs(map[uint32]bool{1: true, 3: true})

	results := lf.GetFilteredEntriesOrdered("test")
	if len(results) != 2 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].FileDataID != 3 || results[1].FileDataID != 1 {
		t.Fatalf("order = %#v", results)
	}
	if lf.ExistsByID(2) {
		t.Fatal("filtered ID should not exist")
	}
}

func TestAddEntry(t *testing.T) {
	lf := New()
	lf.AddEntry(100, "my/custom/file.blp")

	if !lf.ExistsByID(100) {
		t.Fatal("entry not found after add")
	}
	if id := lf.GetByFilename("my/custom/file.blp"); id != 100 {
		t.Fatalf("got ID %d", id)
	}
}

func TestLoadSkipsInvalid(t *testing.T) {
	data := "# comment\n1;valid.txt\nbadline\n2;another.txt\n"
	lf := New()
	lf.Load(strings.NewReader(data))
	if lf.GetByID(2) != "another.txt" {
		t.Fatal("should parse valid lines")
	}
	if lf.ExistsByID(3) {
		t.Fatal("bad line should not create entry")
	}
}

func TestGetAll(t *testing.T) {
	data := "1;a.txt\n2;b.txt\n"
	lf := New()
	lf.Load(strings.NewReader(data))
	all := lf.GetAll()
	if len(all) != 2 {
		t.Fatalf("got %d entries", len(all))
	}
}

func TestLoadBinaryDir(t *testing.T) {
	dir := writeBinaryListfileFixture(t)
	lf := New()
	if err := lf.LoadBinaryDir(dir); err != nil {
		t.Fatalf("LoadBinaryDir: %v", err)
	}

	if got := lf.GetByID(10); got != "interface/icons/sword.blp" {
		t.Fatalf("GetByID(10) = %q", got)
	}
	if got := lf.GetByID(11); got != "world/wmo/city.m2" {
		t.Fatalf("GetByID(11) = %q", got)
	}
	if got := lf.GetByFilename("Interface\\Icons\\Sword.blp"); got != 10 {
		t.Fatalf("slash/case filename lookup = %d", got)
	}
	if got := lf.GetByFilename("world/wmo/city.mdl"); got != 11 {
		t.Fatalf("mdl fallback lookup = %d", got)
	}
	if got := lf.GetByFilename("large/b.bin"); got != 21 {
		t.Fatalf("large dir hash lookup = %d", got)
	}
	if !lf.ExistsByID(20) {
		t.Fatal("binary id should exist")
	}

	results := lf.GetFilteredEntries("icons")
	if len(results) != 1 || results[10] != "interface/icons/sword.blp" {
		t.Fatalf("filtered entries = %#v", results)
	}

	blps := lf.GetFilenamesByExtension(".blp")
	if len(blps) != 1 || blps[0] != "interface/icons/sword.blp [10]" {
		t.Fatalf("extension results = %#v", blps)
	}
}

func TestXXHash64MatchesKnownVector(t *testing.T) {
	cases := map[string]uint64{
		"":          0xef46db3751d8e999,
		"world":     0xe778fbfe66ee51ef,
		"sword.blp": 0xfaec7ac6ab1c65e5,
	}
	for input, want := range cases {
		if got := XXHash64String(input); got != want {
			t.Fatalf("XXHash64String(%q) = %#x, want %#x", input, got, want)
		}
	}
}

func writeBinaryListfileFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entries := []struct {
		id   uint32
		name string
		pf   byte
	}{
		{10, "interface/icons/sword.blp", 0},
		{11, "world/wmo/city.m2", 1},
		{20, "large/a.bin", 0},
		{21, "large/b.bin", 0},
	}

	stringData := bytes.Buffer{}
	offsets := map[uint32]uint32{}
	for _, entry := range entries {
		if entry.pf != 0 {
			continue
		}
		offsets[entry.id] = uint32(stringData.Len())
		stringData.WriteString(entry.name)
		stringData.WriteByte(0)
	}
	if err := os.WriteFile(filepath.Join(dir, componentStrings), stringData.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	modelData := bytes.Buffer{}
	modelData.Write(make([]byte, 4))
	for _, entry := range entries {
		if entry.pf != 1 {
			continue
		}
		offsets[entry.id] = uint32(modelData.Len())
		modelData.WriteString(entry.name)
		modelData.WriteByte(0)
	}
	if err := os.WriteFile(filepath.Join(dir, componentPFModels), modelData.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{componentPFTextures, componentPFSounds, componentPFVideos, componentPFText, componentPFFonts} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{0, 0, 0, 0}, 0644); err != nil {
			t.Fatal(err)
		}
	}

	index := bytes.Buffer{}
	for _, entry := range entries {
		writeBE32(&index, entry.id)
		writeBE32(&index, offsets[entry.id])
		index.WriteByte(entry.pf)
	}
	if err := os.WriteFile(filepath.Join(dir, componentIDIndex), index.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	tree := buildTreeFixture()
	if err := os.WriteFile(filepath.Join(dir, componentTreeNodes), tree, 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func buildTreeFixture() []byte {
	rootChildren := []child{{"interface", 69}, {"large", 106}, {"world", 151}}
	sortChildren(rootChildren)

	buf := bytes.Buffer{}
	writeNodeHeader(&buf, uint32(len(rootChildren)), 0, false)
	for _, c := range rootChildren {
		writeBE64(&buf, XXHash64String(c.name))
		writeBE32(&buf, c.offset)
	}
	writeNodeHeader(&buf, 1, 0, false)
	writeBE64(&buf, XXHash64String("icons"))
	writeBE32(&buf, 90)
	writeNodeHeader(&buf, 0, 1, false)
	writeSmallFile(&buf, "sword.blp", 10)
	writeNodeHeader(&buf, 0, 2, true)
	writeLargeFile(&buf, "a.bin", 20)
	writeLargeFile(&buf, "b.bin", 21)
	writeNodeHeader(&buf, 1, 0, false)
	writeBE64(&buf, XXHash64String("wmo"))
	writeBE32(&buf, 172)
	writeNodeHeader(&buf, 0, 1, false)
	writeSmallFile(&buf, "city.m2", 11)
	return buf.Bytes()
}

type child struct {
	name   string
	offset uint32
}

func sortChildren(children []child) {
	sort.Slice(children, func(i, j int) bool {
		return XXHash64String(children[i].name) < XXHash64String(children[j].name)
	})
}

func writeNodeHeader(buf *bytes.Buffer, childCount, fileCount uint32, large bool) {
	writeBE32(buf, childCount)
	writeBE32(buf, fileCount)
	if large {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
}

func writeSmallFile(buf *bytes.Buffer, name string, id uint32) {
	_ = binary.Write(buf, binary.BigEndian, uint16(len(name)))
	buf.WriteString(name)
	writeBE32(buf, id)
}

func writeLargeFile(buf *bytes.Buffer, name string, id uint32) {
	writeBE64(buf, XXHash64String(name))
	writeBE32(buf, id)
}

func writeBE32(buf *bytes.Buffer, value uint32) {
	_ = binary.Write(buf, binary.BigEndian, value)
}

func writeBE64(buf *bytes.Buffer, value uint64) {
	_ = binary.Write(buf, binary.BigEndian, value)
}
