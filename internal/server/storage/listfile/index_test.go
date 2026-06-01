package listfile

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestIndexLookupByFileDataID(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	entries := []Entry{
		{FileDataID: 12, Path: `World\Maps\Azeroth\Azeroth_33_44.adt`, Extension: ".ADT"},
		{FileDataID: 34, Path: "Interface/Icons/INV_Sword_04.blp"},
	}
	if err := ReplaceSource(ctx, db, "hash-1", entries); err != nil {
		t.Fatalf("ReplaceSource: %v", err)
	}

	got, err := LookupByFileDataID(ctx, db, 12)
	if err != nil {
		t.Fatalf("LookupByFileDataID: %v", err)
	}
	want := Entry{FileDataID: 12, Path: "world/maps/azeroth/azeroth_33_44.adt", Extension: "adt"}
	if got != want {
		t.Fatalf("LookupByFileDataID() = %#v, want %#v", got, want)
	}
}

func TestIndexLookupByFilename(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := ReplaceSource(ctx, db, "hash-1", []Entry{
		{FileDataID: 44, Path: "Creature/Murloc/Murloc.m2"},
	}); err != nil {
		t.Fatalf("ReplaceSource: %v", err)
	}

	got, err := LookupByFilename(ctx, db, `CREATURE\MURLOC\MURLOC.M2`)
	if err != nil {
		t.Fatalf("LookupByFilename: %v", err)
	}
	want := Entry{FileDataID: 44, Path: "creature/murloc/murloc.m2", Extension: "m2"}
	if got != want {
		t.Fatalf("LookupByFilename() = %#v, want %#v", got, want)
	}
}

func TestIndexPathSearchHonorsLimit(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := ReplaceSource(ctx, db, "hash-1", []Entry{
		{FileDataID: 3, Path: "World/Maps/Kalimdor/Kalimdor_2_0.adt"},
		{FileDataID: 1, Path: "World/Maps/Azeroth/Azeroth_0_0.adt"},
		{FileDataID: 2, Path: "World/Maps/Azeroth/Azeroth_1_0.adt"},
	}); err != nil {
		t.Fatalf("ReplaceSource: %v", err)
	}

	got, err := SearchPath(ctx, db, `WORLD\MAPS\AZEROTH`, 1)
	if err != nil {
		t.Fatalf("SearchPath: %v", err)
	}
	want := []Entry{{FileDataID: 1, Path: "world/maps/azeroth/azeroth_0_0.adt", Extension: "adt"}}
	if !entriesEqual(got, want) {
		t.Fatalf("SearchPath() = %#v, want %#v", got, want)
	}

	if _, err := SearchPath(ctx, db, "world", 0); err == nil {
		t.Fatal("SearchPath with invalid limit succeeded, want error")
	}
	if _, err := SearchPath(ctx, nil, "world", 1); err == nil {
		t.Fatal("SearchPath with nil db succeeded, want error")
	}
}

func TestIndexExtensionSearchHonorsLimit(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := ReplaceSource(ctx, db, "hash-1", []Entry{
		{FileDataID: 9, Path: "Interface/Icons/Spell_Fire_FlameBolt.blp"},
		{FileDataID: 7, Path: "Interface/Icons/Ability_BackStab.blp"},
		{FileDataID: 8, Path: "Sound/Music/GlueScreenMusic.mp3"},
	}); err != nil {
		t.Fatalf("ReplaceSource: %v", err)
	}

	got, err := SearchExtension(ctx, db, ".BLP", 1)
	if err != nil {
		t.Fatalf("SearchExtension: %v", err)
	}
	want := []Entry{{FileDataID: 7, Path: "interface/icons/ability_backstab.blp", Extension: "blp"}}
	if !entriesEqual(got, want) {
		t.Fatalf("SearchExtension() = %#v, want %#v", got, want)
	}

	if _, err := SearchExtension(ctx, db, "blp", -1); err == nil {
		t.Fatal("SearchExtension with invalid limit succeeded, want error")
	}
	if _, err := SearchExtension(ctx, nil, "blp", 1); err == nil {
		t.Fatal("SearchExtension with nil db succeeded, want error")
	}
}

func TestIndexSourceHashChangeReplacesRows(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := ReplaceSource(ctx, db, "hash-1", []Entry{
		{FileDataID: 1, Path: "Old/Texture.blp"},
		{FileDataID: 2, Path: "Old/Model.m2"},
	}); err != nil {
		t.Fatalf("ReplaceSource old source: %v", err)
	}
	if err := ReplaceSource(ctx, db, "hash-2", []Entry{
		{FileDataID: 3, Path: "New/Texture.blp"},
	}); err != nil {
		t.Fatalf("ReplaceSource new source: %v", err)
	}

	if _, err := LookupByFileDataID(ctx, db, 1); err == nil {
		t.Fatal("old file data ID still exists after source hash change")
	}
	got, err := SearchPath(ctx, db, "", 10)
	if err != nil {
		t.Fatalf("SearchPath all rows: %v", err)
	}
	want := []Entry{{FileDataID: 3, Path: "new/texture.blp", Extension: "blp"}}
	if !entriesEqual(got, want) {
		t.Fatalf("rows after source hash change = %#v, want %#v", got, want)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "listfile.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})
	return db
}

func entriesEqual(a, b []Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
