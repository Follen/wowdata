package runtime

import (
	"strings"
	"testing"
	"time"

	"wowdata/internal/db2"
	"wowdata/internal/dbd"
)

type loaderFileReader struct {
	files map[uint32][]byte
}

type blockingLoaderFileReader struct {
	started chan<- struct{}
	release <-chan struct{}
	data    []byte
}

func (r blockingLoaderFileReader) ReadFileData(uint32) ([]byte, error) {
	r.started <- struct{}{}
	<-r.release
	return r.data, nil
}

type blockingLoaderDBDSource struct {
	started    chan<- struct{}
	release    <-chan struct{}
	definition string
}

func (s blockingLoaderDBDSource) Definition(string) (string, error) {
	s.started <- struct{}{}
	<-s.release
	return s.definition, nil
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

func TestDB2LoaderReadsFileAndDefinitionConcurrently(t *testing.T) {
	manifest, err := dbd.ParseManifest(strings.NewReader(`[{"tableName":"TestTable","db2FileDataID":99}]`))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	definition := `
COLUMNS
int ID
int Value

BUILD 1.0.0.1
$id$ID<u32>
Value<u32>
`
	loader := NewDB2Loader(manifest,
		blockingLoaderDBDSource{started: started, release: release, definition: definition},
		blockingLoaderFileReader{started: started, release: release, data: db2.BuildMinimalWDC2ForTest()},
		"1.0.0.1")
	done := make(chan error, 1)
	go func() { done <- loader.LoadTable(NewMemoryDB2Store(), "TestTable") }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("file and definition reads did not start concurrently")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("LoadTable: %v", err)
	}
}
