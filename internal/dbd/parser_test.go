package dbd

import (
	"strings"
	"testing"
)

const sampleDBD = `
COLUMNS
int ID
float Scale
string Name
int Flags

BUILD 1.7.0.4671-1.8.0.4714
LAYOUT 0E84A21C, 35353535
$id$ID<u32>
Name
Scale<f32>

BUILD 9.2.5.44170
LAYOUT ABCDEF01
$id$ID<u32>
Name
Flags<u32>
`

func TestParseColumns(t *testing.T) {
	p, err := Parse(strings.NewReader(sampleDBD))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Columns["ID"] != "int" {
		t.Fatalf("Columns[ID] = %q", p.Columns["ID"])
	}
	if p.Columns["Name"] != "string" {
		t.Fatalf("Columns[Name] = %q", p.Columns["Name"])
	}
	if p.Columns["Scale"] != "float" {
		t.Fatalf("Columns[Scale] = %q", p.Columns["Scale"])
	}
}

func TestParseEntries(t *testing.T) {
	p, err := Parse(strings.NewReader(sampleDBD))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(p.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(p.Entries))
	}
}

func TestGetStructureByLayoutHash(t *testing.T) {
	p, err := Parse(strings.NewReader(sampleDBD))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	entry := p.GetStructure("9.2.5.44170", "ABCDEF01")
	if entry == nil {
		t.Fatal("expected entry for layout hash ABCDEF01")
	}
	hasFlags := false
	for _, f := range entry.Fields {
		if f.Name == "Flags" {
			hasFlags = true
		}
	}
	if !hasFlags {
		t.Fatal("expected Flags field in 9.x entry")
	}
}

func TestGetStructureByBuildID(t *testing.T) {
	p, err := Parse(strings.NewReader(sampleDBD))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	entry := p.GetStructure("1.7.0.4671", "")
	if entry == nil {
		t.Fatal("expected entry for build 1.7.0.4671")
	}
}

func TestGetStructureByBuildRange(t *testing.T) {
	p, err := Parse(strings.NewReader(sampleDBD))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	entry := p.GetStructure("1.7.0.4700", "")
	if entry == nil {
		t.Fatal("expected entry for build in range 1.7.0.4671-1.8.0.4714")
	}
}

func TestGetStructureMissing(t *testing.T) {
	p, err := Parse(strings.NewReader(sampleDBD))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if entry := p.GetStructure("0.1.0.0", ""); entry != nil {
		t.Fatal("expected nil for unknown build")
	}
}

func TestFieldAnnotations(t *testing.T) {
	p, err := Parse(strings.NewReader(sampleDBD))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	entry := p.GetStructure("1.7.0.4671", "")
	for _, f := range entry.Fields {
		if f.Name == "ID" {
			if !f.IsID {
				t.Fatal("ID should have isID=true")
			}
			if f.IsSigned {
				t.Fatal("ID<u32> should not be signed")
			}
			if f.Size != 32 {
				t.Fatalf("ID size = %d, want 32", f.Size)
			}
		}
	}
}

func TestEmptyColumnsError(t *testing.T) {
	_, err := Parse(strings.NewReader("garbage\nwithout\ncolumns"))
	if err == nil {
		t.Fatal("expected error for DBD without COLUMNS")
	}
}

func TestNoColumnsDefined(t *testing.T) {
	_, err := Parse(strings.NewReader("COLUMNS\n\nBUILD 1.0\nID"))
	if err == nil {
		t.Fatal("expected error when no columns defined after COLUMNS header")
	}
}

func TestMissingColumnType(t *testing.T) {
	_, err := Parse(strings.NewReader("COLUMNS\nint ID\n\nBUILD 1.0\nUnknownField"))
	if err == nil {
		t.Fatal("expected error for unknown field type")
	}
}
