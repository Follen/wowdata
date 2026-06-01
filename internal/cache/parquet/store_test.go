package parquet

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestMetadataMatchesRequiresSameFingerprint(t *testing.T) {
	want := testMetadata()
	if !want.Matches(want) {
		t.Fatal("identical metadata did not match")
	}

	tests := []struct {
		name   string
		mutate func(*Metadata)
	}{
		{name: "region", mutate: func(m *Metadata) { m.Region = "us" }},
		{name: "product", mutate: func(m *Metadata) { m.Product = "wow_classic" }},
		{name: "build key", mutate: func(m *Metadata) { m.BuildKey = "build-b" }},
		{name: "locale", mutate: func(m *Metadata) { m.Locale = "enUS" }},
		{name: "table", mutate: func(m *Metadata) { m.Table = "ItemSparse" }},
		{name: "dbd hash", mutate: func(m *Metadata) { m.DBDDefinitionHash = "hash-b" }},
		{name: "decoder version", mutate: func(m *Metadata) { m.DecoderVersion = "decoder-b" }},
		{name: "materializer version", mutate: func(m *Metadata) { m.MaterializerVersion = "materializer-b" }},
		{name: "file data id", mutate: func(m *Metadata) { m.DB2FileDataID = 456 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := want
			tt.mutate(&got)
			if got.Matches(want) {
				t.Fatalf("metadata mismatch accepted: got=%#v want=%#v", got, want)
			}
		})
	}
}

func TestPathForIncludesContextAndTable(t *testing.T) {
	got := filepath.ToSlash(PathFor("root", Metadata{
		Region:   "cn",
		Product:  "wow",
		BuildKey: "build",
		Locale:   "zhCN",
		Table:    "SpellName",
	}))

	want := "root/db2/cn/wow/build/zhCN/SpellName.parquet"
	if got != want {
		t.Fatalf("PathFor = %q, want %q", got, want)
	}
}

func TestValidateExistingReportsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SpellName.parquet")

	err := ValidateExisting(path, testMetadata())
	if err == nil {
		t.Fatal("ValidateExisting missing file error = nil")
	}
	if errors.Is(err, ErrStale) {
		t.Fatalf("ValidateExisting missing file error = %v, want non-stale missing file error", err)
	}
}

func TestValidateExistingReportsStaleMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SpellName.parquet")
	stored := testMetadata()
	if err := WriteMetadata(path, stored); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}

	want := stored
	want.DecoderVersion = "decoder-b"
	err := ValidateExisting(path, want)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("ValidateExisting mismatch error = %v, want ErrStale", err)
	}
}

func TestValidateExistingAcceptsMatchingMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SpellName.parquet")
	want := testMetadata()
	if err := WriteMetadata(path, want); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}

	if err := ValidateExisting(path, want); err != nil {
		t.Fatalf("ValidateExisting matching metadata: %v", err)
	}
}

func testMetadata() Metadata {
	return Metadata{
		Region:              "cn",
		Product:             "wow",
		BuildKey:            "build-a",
		Locale:              "zhCN",
		Table:               "SpellName",
		DBDDefinitionHash:   "hash-a",
		DecoderVersion:      "decoder-a",
		MaterializerVersion: "materializer-a",
		DB2FileDataID:       123,
	}
}
