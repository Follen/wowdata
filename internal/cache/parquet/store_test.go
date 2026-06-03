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

func TestWriteRowsFileSplitsLargeTablesIntoBoundedRowGroups(t *testing.T) {
	root := t.TempDir()
	meta := testMetadata()
	path := PathFor(root, meta)
	rows := make([]map[string]interface{}, 1200)
	for i := range rows {
		rows[i] = map[string]interface{}{"ID": int32(i + 1)}
	}

	if err := WriteRowsFile(path, meta, []Field{{Name: "ID", Type: "int32"}}, rows); err != nil {
		t.Fatalf("WriteRowsFile: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open parquet: %v", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat parquet: %v", err)
	}
	pf, err := parquetgo.OpenFile(file, info.Size())
	if err != nil {
		t.Fatalf("open parquet file: %v", err)
	}
	assertBoundedDB2RowGroups(t, pf, 3)
}

func TestWriteRowSourceFileSplitsLargeTablesIntoBoundedRowGroups(t *testing.T) {
	root := t.TempDir()
	meta := testMetadata()
	path := PathFor(root, meta)
	source := &countingRowSource{total: 1200}

	rowCount, err := WriteRowSourceFile(path, meta, []Field{{Name: "ID", Type: "int32"}}, source)
	if err != nil {
		t.Fatalf("WriteRowSourceFile: %v", err)
	}
	if rowCount != 1200 {
		t.Fatalf("row count = %d, want 1200", rowCount)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open parquet: %v", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat parquet: %v", err)
	}
	pf, err := parquetgo.OpenFile(file, info.Size())
	if err != nil {
		t.Fatalf("open parquet file: %v", err)
	}
	assertBoundedDB2RowGroups(t, pf, 3)
}

func TestWriteRowsFileAcceptsRuntimeDB2FieldTypes(t *testing.T) {
	root := t.TempDir()
	meta := testMetadata()
	path := PathFor(root, meta)
	rows := []map[string]interface{}{
		{"ID": uint32(123), "Name_lang": "Fireball", "Parent": int32(7)},
	}

	if err := WriteRowsFile(path, meta, []Field{
		{Name: "ID", Type: "dbFieldUInt32"},
		{Name: "Name_lang", Type: "dbFieldString"},
		{Name: "Parent", Type: "dbFieldNonInlineID"},
	}, rows); err != nil {
		t.Fatalf("WriteRowsFile: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open parquet: %v", err)
	}
	defer file.Close()
	type runtimeTypeRow struct {
		ID       uint32 `parquet:"ID"`
		NameLang string `parquet:"Name_lang"`
		Parent   int32  `parquet:"Parent"`
	}
	reader := parquetgo.NewGenericReader[runtimeTypeRow](file)
	got := make([]runtimeTypeRow, 1)
	n, err := reader.Read(got)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read parquet rows: %v", err)
	}
	if n != 1 {
		t.Fatalf("read rows = %d, want 1", n)
	}
	if got[0].ID != 123 || got[0].NameLang != "Fireball" || got[0].Parent != 7 {
		t.Fatalf("row = %#v", got[0])
	}
}

func TestWriteRowsFileExpandsDB2ArrayFields(t *testing.T) {
	root := t.TempDir()
	meta := testMetadata()
	path := PathFor(root, meta)
	rows := []map[string]interface{}{
		{
			"EffectMiscValue": []interface{}{int32(11), int32(22)},
			"ParamLabel":      []interface{}{"Points", "Accuracy"},
		},
	}

	if err := WriteRowsFile(path, meta, []Field{
		{Name: "EffectMiscValue", Type: "dbFieldInt32", ArrayLen: 2},
		{Name: "ParamLabel", Type: "dbFieldString", ArrayLen: 2},
	}, rows); err != nil {
		t.Fatalf("WriteRowsFile: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open parquet: %v", err)
	}
	defer file.Close()
	type arrayRow struct {
		EffectMiscValue0 int32  `parquet:"EffectMiscValue_0"`
		EffectMiscValue1 int32  `parquet:"EffectMiscValue_1"`
		ParamLabel0      string `parquet:"ParamLabel_0"`
		ParamLabel1      string `parquet:"ParamLabel_1"`
	}
	reader := parquetgo.NewGenericReader[arrayRow](file)
	got := make([]arrayRow, 1)
	n, err := reader.Read(got)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read parquet rows: %v", err)
	}
	if n != 1 {
		t.Fatalf("read rows = %d, want 1", n)
	}
	if got[0].EffectMiscValue0 != 11 || got[0].EffectMiscValue1 != 22 {
		t.Fatalf("array values = %#v, want 11 and 22", got[0])
	}
	if got[0].ParamLabel0 != "Points" || got[0].ParamLabel1 != "Accuracy" {
		t.Fatalf("string array values = %#v, want Points and Accuracy", got[0])
	}
}

func TestWriteRowsFileReturnsConversionErrors(t *testing.T) {
	cases := []struct {
		name  string
		field Field
		value interface{}
		want  string
	}{
		{
			name:  "uint64 to int32 overflow",
			field: Field{Name: "Value", Type: "dbFieldInt32"},
			value: uint64(1 << 40),
			want:  "overflows int32",
		},
		{
			name:  "negative signed to uint",
			field: Field{Name: "Value", Type: "dbFieldUInt32"},
			value: int32(-1),
			want:  "cannot convert negative",
		},
		{
			name:  "unsupported numeric type",
			field: Field{Name: "Value", Type: "dbFieldInt32"},
			value: "not-a-number",
			want:  "cannot convert string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := PathFor(t.TempDir(), testMetadata())
			err := WriteRowsFile(path, testMetadata(), []Field{tc.field}, []map[string]interface{}{{"Value": tc.value}})
			if err == nil {
				t.Fatal("WriteRowsFile error = nil, want conversion error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("WriteRowsFile error = %v, want substring %q", err, tc.want)
			}
			if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("parquet path exists after conversion failure: stat err = %v", statErr)
			}
		})
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

type countingRowSource struct {
	total int
	next  int
}

func (s *countingRowSource) NextRow() (map[string]interface{}, bool, error) {
	if s.next >= s.total {
		return nil, false, nil
	}
	s.next++
	return map[string]interface{}{"ID": int32(s.next)}, true, nil
}

func (s *countingRowSource) Close() error {
	return nil
}

func assertBoundedDB2RowGroups(t *testing.T, pf *parquetgo.File, wantGroups int) {
	t.Helper()
	rowGroups := pf.RowGroups()
	if got := len(rowGroups); got != wantGroups {
		t.Fatalf("row groups = %d, want %d", got, wantGroups)
	}
	for i, rowGroup := range rowGroups {
		if got := rowGroup.NumRows(); got > db2MaxRowsPerRowGroup {
			t.Fatalf("row group %d rows = %d, want <= %d", i, got, db2MaxRowsPerRowGroup)
		}
	}
}
