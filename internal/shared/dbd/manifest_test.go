package dbd

import (
	"strings"
	"testing"
)

func TestManifestParseAndLookup(t *testing.T) {
	raw := `[
		{"tableName":"SpellName","db2FileDataID":123},
		{"tableName":"Item","db2FileDataID":456}
	]`

	manifest, err := ParseManifest(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	id, ok := manifest.GetByTableName("SpellName")
	if !ok || id != 123 {
		t.Fatalf("SpellName id = %d %v", id, ok)
	}
	name, ok := manifest.GetByID(456)
	if !ok || name != "Item" {
		t.Fatalf("456 name = %q %v", name, ok)
	}
}

func TestManifestRejectsEmptyMappings(t *testing.T) {
	_, err := ParseManifest(strings.NewReader(`[]`))
	if err == nil {
		t.Fatal("expected empty manifest error")
	}
}
