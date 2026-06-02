package artifact

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestReserveCreatesLinkInsideRoot(t *testing.T) {
	root := t.TempDir()
	m := NewManager(Config{Root: root, BaseURL: "https://mcp.example.test/files", RetentionHours: 24})

	gotPath, link, err := m.Reserve("icons", "134400.png", "image/png")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	wantPath := filepath.Join(root, "icons", "134400.png")
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
	if link.Path != wantPath {
		t.Fatalf("link path = %q, want %q", link.Path, wantPath)
	}
	if link.DownloadURL != "https://mcp.example.test/files/icons/134400.png" {
		t.Fatalf("download URL = %q", link.DownloadURL)
	}
	if link.URI != link.DownloadURL {
		t.Fatalf("URI = %q, want %q", link.URI, link.DownloadURL)
	}
	if link.MimeType != "image/png" {
		t.Fatalf("mime type = %q", link.MimeType)
	}
	if link.Name != "134400.png" {
		t.Fatalf("name = %q", link.Name)
	}
}

func TestReserveEscapesDownloadURLPathSegments(t *testing.T) {
	root := t.TempDir()
	m := NewManager(Config{Root: root, BaseURL: "https://mcp.example.test/files/", RetentionHours: 24})

	_, link, err := m.Reserve("icons", "foo #bar?.png", "image/png")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	want := "https://mcp.example.test/files/icons/foo%20%23bar%3F.png"
	if link.DownloadURL != want {
		t.Fatalf("download URL = %q, want %q", link.DownloadURL, want)
	}
	if link.URI != want {
		t.Fatalf("URI = %q, want %q", link.URI, want)
	}
}

func TestReserveRejectsTraversalFilename(t *testing.T) {
	m := NewManager(Config{Root: t.TempDir(), BaseURL: "https://mcp.example.test/files"})

	if _, _, err := m.Reserve("icons", "../evil.png", "image/png"); err == nil {
		t.Fatal("expected traversal filename to be rejected")
	}
}

func TestReserveRejectsTraversalCategory(t *testing.T) {
	m := NewManager(Config{Root: t.TempDir(), BaseURL: "https://mcp.example.test/files"})

	if _, _, err := m.Reserve("../icons", "134400.png", "image/png"); err == nil {
		t.Fatal("expected traversal category to be rejected")
	}
}

func TestLinkForPathMapsOnlyRootFiles(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	m := NewManager(Config{Root: root, BaseURL: "https://mcp.example.test/files"})
	inRoot := filepath.Join(root, "icons", "134400.png")
	if err := os.MkdirAll(filepath.Dir(inRoot), 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("icon bytes")
	if err := os.WriteFile(inRoot, content, 0644); err != nil {
		t.Fatal(err)
	}

	link, err := m.LinkForPath(inRoot, "image/png")
	if err != nil {
		t.Fatalf("LinkForPath root file: %v", err)
	}
	if link.DownloadURL != "https://mcp.example.test/files/icons/134400.png" {
		t.Fatalf("download URL = %q", link.DownloadURL)
	}
	if link.URI != link.DownloadURL {
		t.Fatalf("URI = %q", link.URI)
	}
	if link.Size != int64(len(content)) {
		t.Fatalf("size = %d", link.Size)
	}
	sum := sha256.Sum256(content)
	if link.SHA256 != fmt.Sprintf("%x", sum) {
		t.Fatalf("sha256 = %q", link.SHA256)
	}

	outside := filepath.Join(other, "secret.png")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.LinkForPath(outside, "image/png"); err == nil {
		t.Fatal("expected root outside path to be rejected")
	}
}

func TestLinkForPathRejectsSymlinkTraversal(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	linkDir := filepath.Join(root, "link")
	if err := os.Symlink(other, linkDir); err != nil {
		if runtime.GOOS == "windows" || errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink not available: %v", err)
		}
		t.Fatalf("create symlink: %v", err)
	}
	outside := filepath.Join(other, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(Config{Root: root, BaseURL: "https://mcp.example.test/files"})
	if _, err := m.LinkForPath(filepath.Join(linkDir, "outside.txt"), "text/plain"); err == nil {
		t.Fatal("expected symlink traversal to be rejected")
	}
}

func TestReserveRejectsSymlinkTraversal(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	linkDir := filepath.Join(root, "link")
	if err := os.Symlink(other, linkDir); err != nil {
		if runtime.GOOS == "windows" || errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink not available: %v", err)
		}
		t.Fatalf("create symlink: %v", err)
	}

	m := NewManager(Config{Root: root, BaseURL: "https://mcp.example.test/files"})
	if _, _, err := m.Reserve("link", "outside.txt", "text/plain"); err == nil {
		t.Fatal("expected reserve through symlink to be rejected")
	}
}

func TestReserveRejectsNestedSymlinkTraversalWithoutCreatingOutsideDirectory(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	linkDir := filepath.Join(root, "link")
	if err := os.Symlink(other, linkDir); err != nil {
		if runtime.GOOS == "windows" || errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink not available: %v", err)
		}
		t.Fatalf("create symlink: %v", err)
	}

	m := NewManager(Config{Root: root, BaseURL: "https://mcp.example.test/files"})
	if _, _, err := m.Reserve("link/nested", "outside.txt", "text/plain"); err == nil {
		t.Fatal("expected nested reserve through symlink to be rejected")
	}
	if _, err := os.Stat(filepath.Join(other, "nested")); !os.IsNotExist(err) {
		t.Fatalf("outside nested directory should not be created, stat err=%v", err)
	}
}

func TestCleanupExpiredDeletesExpiredFiles(t *testing.T) {
	root := t.TempDir()
	m := NewManager(Config{Root: root, BaseURL: "https://mcp.example.test/files", RetentionHours: 0})
	path := filepath.Join(root, "icons", "old.png")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if err := m.CleanupExpired(); err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected expired file to be deleted, stat err=%v", err)
	}
}
