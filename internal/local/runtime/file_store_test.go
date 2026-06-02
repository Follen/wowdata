package runtime

import (
	"fmt"
	"testing"

	"wowdata/internal/casc"
	"wowdata/internal/listfile"
)

type readByIDSource struct {
	data []byte
}

func (s readByIDSource) ReadFileData(fileDataID uint32) ([]byte, error) {
	return s.data, nil
}

type metadataReader struct {
	exists bool
	info   *casc.FileInfo
}

func (m metadataReader) ReadFileData(uint32) ([]byte, error) { return []byte("raw"), nil }

func (m metadataReader) FileExists(uint32) bool { return m.exists }

func (m metadataReader) GetFileEncodingInfo(uint32) (*casc.FileInfo, error) {
	if m.info == nil {
		return nil, fmt.Errorf("missing metadata")
	}
	return m.info, nil
}

func TestCASCFileStoreReadAndLookup(t *testing.T) {
	lf := listfile.New()
	lf.AddEntry(10, "interface/icons/test.blp")
	fs := casc.NewFileService()
	fs.AddRootEntry(10, "content")
	fs.AddEncodingEntry("content", "encoding", 12)

	store := NewCASCFileStore(lf, fs, readByIDSource{data: []byte("raw")})
	name, found := store.Lookup(10)
	if !found || name != "interface/icons/test.blp" {
		t.Fatalf("lookup = %q %v", name, found)
	}
	data, err := store.ReadByID(10)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}
	if string(data) != "raw" {
		t.Fatalf("data = %q", data)
	}
}

func TestCASCFileStoreReadyWithFileServiceOnly(t *testing.T) {
	fs := casc.NewFileService()
	fs.AddRootEntry(134400, "content")

	store := NewCASCFileStore(nil, fs, nil)
	if !store.Ready() {
		t.Fatal("store with CASC file service should be ready for ID-based operations")
	}
	if !store.ExistsByID(134400) {
		t.Fatal("ExistsByID should use CASC file service without requiring listfile")
	}
}

func TestCASCFileStoreUsesReaderMetadataWithoutFileServiceCopy(t *testing.T) {
	reader := metadataReader{
		exists: true,
		info: &casc.FileInfo{
			FileDataID:  134400,
			ContentKey:  "content",
			EncodingKey: "encoding",
			Enc:         "encoding",
			Size:        42,
		},
	}
	store := NewCASCFileStore(nil, nil, reader)

	if !store.ExistsByID(134400) {
		t.Fatal("ExistsByID should use reader metadata without requiring FileService")
	}
	info, err := store.EncodingInfo(134400)
	if err != nil {
		t.Fatalf("EncodingInfo: %v", err)
	}
	got := info.(*casc.FileInfo)
	if got.EncodingKey != "encoding" || got.Size != 42 {
		t.Fatalf("metadata = %#v", got)
	}
}

func TestCASCFileStoreExtensionPreservesFormattedIDs(t *testing.T) {
	lf := listfile.New()
	lf.AddEntry(10, "interface/icons/test.blp")
	store := NewCASCFileStore(lf, nil, nil)

	entries := store.Extension(".blp", 1)
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].FileDataID != 10 || entries[0].Filename != "interface/icons/test.blp" {
		t.Fatalf("entry = %#v", entries[0])
	}
}

func TestCASCFileStoreSearchPreservesListfileOrderAndLimitsResults(t *testing.T) {
	lf := listfile.New()
	lf.AddEntry(30, "z/test.blp")
	lf.AddEntry(10, "a/test.blp")
	lf.AddEntry(20, "a/another-test.blp")
	store := NewCASCFileStore(lf, nil, nil)

	entries := store.Search("test", 2)
	if len(entries) != 2 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].FileDataID != 30 || entries[0].Filename != "z/test.blp" {
		t.Fatalf("first entry = %#v", entries[0])
	}
	if entries[1].FileDataID != 10 || entries[1].Filename != "a/test.blp" {
		t.Fatalf("second entry = %#v", entries[1])
	}
}
