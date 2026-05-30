package casc

import "testing"

func TestFileServiceExists(t *testing.T) {
	fs := NewFileService()
	fs.AddRootEntry(123, "cafebabecafebabecafebabecafebabe")
	if !fs.Exists(123) {
		t.Fatal("file 123 should exist")
	}
	if fs.Exists(999) {
		t.Fatal("file 999 should not exist")
	}
}

func TestFileServiceEncodingInfo(t *testing.T) {
	fs := NewFileService()
	fs.AddRootEntry(123, "contentkey123")
	fs.AddEncodingEntry("contentkey123", "encodingkey456", 1024)

	info, err := fs.GetEncodingInfo(123)
	if err != nil {
		t.Fatalf("GetEncodingInfo: %v", err)
	}
	if info.EncodingKey != "encodingkey456" {
		t.Fatalf("encoding key = %s", info.EncodingKey)
	}
	if info.Size != 1024 {
		t.Fatalf("size = %d", info.Size)
	}
}

func TestFileServiceEncodingInfoMissing(t *testing.T) {
	fs := NewFileService()
	_, err := fs.GetEncodingInfo(999)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
