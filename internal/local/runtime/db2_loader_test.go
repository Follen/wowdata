package runtime

import (
	"strings"
	"testing"

	"wowdata/internal/shared/db2"
	"wowdata/internal/shared/dbd"
)

type loaderFileReader struct {
	files map[uint32][]byte
}

func (r loaderFileReader) ReadFileData(fileDataID uint32) ([]byte, error) {
	return r.files[fileDataID], nil
}

type loaderDBDSource struct {
	defs map[string]string
}

func (s loaderDBDSource) Definition(tableName string) (string, error) {
	return s.defs[tableName], nil
}

func TestDB2LoaderLoadsWDCIntoStore(t *testing.T) {
	manifest, err := dbd.ParseManifest(strings.NewReader(`[{"tableName":"TestTable","db2FileDataID":99}]`))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	loader := NewDB2Loader(manifest, loaderDBDSource{defs: map[string]string{
		"TestTable": `
COLUMNS
int ID
int Value

BUILD 1.0.0.1
$id$ID<u32>
Value<u32>
`,
	}}, loaderFileReader{files: map[uint32][]byte{
		99: db2.BuildMinimalWDC2ForTest(),
	}}, "1.0.0.1")

	store := NewMemoryDB2Store()
	if err := loader.LoadTable(store, "TestTable"); err != nil {
		t.Fatalf("LoadTable: %v", err)
	}
	rows, err := store.Rows("TestTable", []uint32{1}, nil, "", 1)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != 1 || rows[0]["Value"] != uint32(100) {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

func TestSchemaForRuntimeAddsSyntheticID(t *testing.T) {
	schema := schemaForRuntime([]db2.SchemaField{{Name: "Name", Type: db2.FieldString}})
	if len(schema) != 2 || schema[0].Name != "ID" || schema[0].Type != "dbFieldUInt32" || schema[1].Name != "Name" {
		t.Fatalf("schema = %#v, want synthetic ID then Name", schema)
	}
}
