package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "test.bin")
	data := []byte("hello world export test")

	result, err := ExportFile(data, out)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if !result.OK {
		t.Fatal("result not OK")
	}
	if result.Size != int64(len(data)) {
		t.Fatalf("size = %d", result.Size)
	}
	if result.Hash == "" {
		t.Fatal("hash is empty")
	}
	if !filepath.IsAbs(result.Path) {
		t.Fatalf("path should be absolute, got %q", result.Path)
	}
	if !strings.HasPrefix(result.URI, "file:///") {
		t.Fatalf("uri should be a file URI, got %q", result.URI)
	}
	if result.Name != "test.bin" {
		t.Fatalf("name = %q", result.Name)
	}
	if result.MimeType != "application/octet-stream" {
		t.Fatalf("mimeType = %q", result.MimeType)
	}
	if result.Overwrite {
		t.Fatal("should not overwrite on first write")
	}

	// Read back
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("data mismatch: %q", got)
	}
}

func TestExportFileOverwrite(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "test.bin")
	os.WriteFile(out, []byte("original"), 0644)

	result, err := ExportFile([]byte("updated"), out)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if !result.Overwrite {
		t.Fatal("should detect overwrite")
	}
}

func TestExportFileEmptyPath(t *testing.T) {
	_, err := ExportFile([]byte("data"), "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}
