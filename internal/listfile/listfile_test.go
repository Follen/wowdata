package listfile

import (
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
