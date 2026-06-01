package parquet

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	parquetgo "github.com/parquet-go/parquet-go"
)

func TestMetadataMatchesFingerprintFields(t *testing.T) {
	want := testMetadata()
	if !want.Matches(want) {
		t.Fatal("metadata should match itself")
	}

	cases := map[string]func(Metadata) Metadata{
		"DBDDefinitionHash": func(m Metadata) Metadata {
			m.DBDDefinitionHash = "different"
			return m
		},
		"DecoderVersion": func(m Metadata) Metadata {
			m.DecoderVersion = "different"
			return m
		},
		"MaterializerVersion": func(m Metadata) Metadata {
			m.MaterializerVersion = "different"
			return m
		},
		"DB2FileDataID": func(m Metadata) Metadata {
			m.DB2FileDataID++
			return m
		},
		"BuildKey": func(m Metadata) Metadata {
			m.BuildKey = "different"
			return m
		},
		"Locale": func(m Metadata) Metadata {
			m.Locale = "enUS"
			return m
		},
		"Table": func(m Metadata) Metadata {
			m.Table = "ItemSparse"
			return m
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got := mutate(want)
			if got.Matches(want) {
				t.Fatalf("metadata mismatch for %s accepted: got=%#v want=%#v", name, got, want)
			}
		})
	}
}

func TestPathForIncludesContextAndTable(t *testing.T) {
	root := filepath.Join("cache-root")
	got := filepath.ToSlash(PathFor(root, testMetadata()))

	for _, want := range []string{"cache-root", "db2", "cn", "wow", "build-key", "zhCN", "SpellName.parquet"} {
		if !strings.Contains(got, want) {
			t.Fatalf("PathFor = %q, want segment %q", got, want)
		}
	}
}

func TestWriteMetadataFileWritesRealParquetAndValidateReadsFooter(t *testing.T) {
	root := t.TempDir()
	meta := testMetadata()
	path := PathFor(root, meta)

	if err := WriteMetadataFile(path, meta); err != nil {
		t.Fatalf("WriteMetadataFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat parquet: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("parquet file is empty")
	}
	if err := os.Remove(path + ".metadata.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remove companion metadata file: %v", err)
	}

	got, err := ReadMetadata(path)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if !got.Matches(meta) {
		t.Fatalf("ReadMetadata mismatch: got=%#v want=%#v", got, meta)
	}
	if _, err := ValidateExisting(path, meta); err != nil {
		t.Fatalf("ValidateExisting: %v", err)
	}
}

func TestWriteRowsFileWritesDB2RowsAndMetadataFooter(t *testing.T) {
	root := t.TempDir()
	meta := testMetadata()
	path := PathFor(root, meta)
	rows := []map[string]interface{}{
		{"ID": uint32(123), "Name_lang": "Fireball"},
	}

	if err := WriteRowsFile(path, meta, []Field{
		{Name: "ID", Type: "uint32"},
		{Name: "Name_lang", Type: "string"},
	}, rows); err != nil {
		t.Fatalf("WriteRowsFile: %v", err)
	}
	if _, err := ValidateExisting(path, meta); err != nil {
		t.Fatalf("ValidateExisting: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open parquet: %v", err)
	}
	defer file.Close()
	type spellNameRow struct {
		ID       int32  `parquet:"ID"`
		NameLang string `parquet:"Name_lang"`
	}
	reader := parquetgo.NewGenericReader[spellNameRow](file)
	got := make([]spellNameRow, 1)
	n, err := reader.Read(got)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read parquet rows: %v", err)
	}
	if n != 1 {
		t.Fatalf("read rows = %d, want 1", n)
	}
	if got[0].ID != 123 {
		t.Fatalf("ID = %#v", got[0].ID)
	}
	if got[0].NameLang != "Fireball" {
		t.Fatalf("Name_lang = %#v", got[0].NameLang)
	}
}

func TestValidateExistingReturnsErrStaleOnFingerprintMismatch(t *testing.T) {
	root := t.TempDir()
	meta := testMetadata()
	path := PathFor(root, meta)
	if err := WriteMetadataFile(path, meta); err != nil {
		t.Fatalf("WriteMetadataFile: %v", err)
	}
	want := meta
	want.DecoderVersion = "decoder-v2"

	if _, err := ValidateExisting(path, want); !errors.Is(err, ErrStale) {
		t.Fatalf("ValidateExisting mismatch error = %v, want ErrStale", err)
	}
}

func TestWriteMetadataFileIsAtomicOnWriterFailure(t *testing.T) {
	root := t.TempDir()
	meta := testMetadata()
	path := PathFor(root, meta)
	if err := WriteMetadataFile(path, meta); err != nil {
		t.Fatalf("WriteMetadataFile valid: %v", err)
	}

	fail := errors.New("forced writer failure")
	if err := WriteMetadataFile(path, meta, WithWriterForTest(func(*os.File, Metadata) error {
		return fail
	})); !errors.Is(err, fail) {
		t.Fatalf("WriteMetadataFile forced error = %v, want %v", err, fail)
	}

	if _, err := ValidateExisting(path, meta); err != nil {
		t.Fatalf("existing parquet should remain valid after failed write: %v", err)
	}
}

func testMetadata() Metadata {
	return Metadata{
		Region:              "cn",
		Product:             "wow",
		BuildKey:            "build-key",
		Locale:              "zhCN",
		Table:               "SpellName",
		DB2FileDataID:       123,
		DBDDefinitionHash:   "dbd-hash",
		DecoderVersion:      "decoder-v1",
		MaterializerVersion: "materializer-v1",
	}
}
