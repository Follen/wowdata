package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wowdata/internal/server/storage/metadata"
)

func TestReservePathStaysUnderArtifactRoot(t *testing.T) {
	root := t.TempDir()
	store := NewStore(Config{
		Root:    root,
		BaseURL: "http://example.test/files/",
	}, nil)

	path, record, err := store.Reserve("icons", "spell icon.png", "image/png")
	if err != nil {
		t.Fatalf("reserve artifact: %v", err)
	}
	assertUnderRoot(t, root, path)
	assertUnderRoot(t, root, record.Path)

	wantURL := "http://example.test/files/icons/spell%20icon.png"
	if record.DownloadURL != wantURL {
		t.Fatalf("DownloadURL = %q, want %q", record.DownloadURL, wantURL)
	}
	if record.URI != record.DownloadURL {
		t.Fatalf("URI = %q, want DownloadURL %q", record.URI, record.DownloadURL)
	}
	if record.MIMEType != "image/png" || record.Name != "spell icon.png" {
		t.Fatalf("record = %#v, want MIMEType image/png and name spell icon.png", record)
	}
}

func TestStoreResponseContainsDownloadURL(t *testing.T) {
	root := t.TempDir()
	db := openMetadataDB(t)
	store := NewStore(Config{
		Root:    root,
		BaseURL: "http://211.154.18.253:11223/files",
	}, db)
	data := []byte("artifact payload")

	record, err := store.StoreBytes(context.Background(), RequestContext{
		Region:   "cn",
		Product:  "wow",
		Locale:   "zhCN",
		BuildKey: "build-1",
	}, "exports", "spell data.json", "application/json", data)
	if err != nil {
		t.Fatalf("store bytes: %v", err)
	}

	assertUnderRoot(t, root, record.Path)
	got, err := os.ReadFile(record.Path)
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("stored bytes = %q, want %q", got, data)
	}

	wantURL := "http://211.154.18.253:11223/files/exports/spell%20data.json"
	if record.DownloadURL != wantURL {
		t.Fatalf("DownloadURL = %q, want %q", record.DownloadURL, wantURL)
	}
	if record.URI != wantURL {
		t.Fatalf("URI = %q, want %q", record.URI, wantURL)
	}
	if record.Size != int64(len(data)) {
		t.Fatalf("Size = %d, want %d", record.Size, len(data))
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256(data))
	if record.SHA256 != wantHash {
		t.Fatalf("SHA256 = %q, want %q", record.SHA256, wantHash)
	}
	if record.MIMEType != "application/json" || record.Name != "spell data.json" {
		t.Fatalf("record = %#v, want MIMEType application/json and name spell data.json", record)
	}

	var row metadata.Artifact
	if err := db.QueryRow(`
SELECT region, product, locale, build_key, artifact_path, download_url, mime_type, size_bytes, sha256
FROM server_artifacts
WHERE artifact_path = ?`, filepath.ToSlash(record.Path)).Scan(
		&row.Region,
		&row.Product,
		&row.Locale,
		&row.BuildKey,
		&row.Path,
		&row.DownloadURL,
		&row.MIMEType,
		&row.Size,
		&row.SHA256,
	); err != nil {
		t.Fatalf("query artifact metadata: %v", err)
	}
	if row.Region != "cn" || row.Product != "wow" || row.Locale != "zhCN" || row.BuildKey != "build-1" {
		t.Fatalf("metadata context = %#v, want cn/wow/zhCN/build-1", row)
	}
	if row.DownloadURL != record.DownloadURL || row.MIMEType != record.MIMEType || row.Size != record.Size || row.SHA256 != record.SHA256 {
		t.Fatalf("metadata row = %#v, want record %#v", row, record)
	}
}

func TestStoreBytesRejectsExistingSymlinkFinalPath(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "outside.txt")
	original := []byte("do not overwrite")
	if err := os.WriteFile(outsidePath, original, 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "exports"), 0755); err != nil {
		t.Fatalf("create exports dir: %v", err)
	}
	linkPath := filepath.Join(root, "exports", "payload.bin")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Skipf("creating symlink unavailable: %v", err)
	}
	db := openMetadataDB(t)
	store := NewStore(Config{
		Root:    root,
		BaseURL: "http://example.test/files",
	}, db)

	_, err := store.StoreBytes(context.Background(), RequestContext{
		Region:   "cn",
		Product:  "wow",
		Locale:   "zhCN",
		BuildKey: "build-1",
	}, "exports", "payload.bin", "application/octet-stream", []byte("attacker overwrite"))
	if err == nil {
		t.Fatal("StoreBytes succeeded for existing symlink final path, want error")
	}
	got, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatalf("read outside file: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("outside file was overwritten through symlink: got %q, want %q", got, original)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM server_artifacts WHERE artifact_path = ?`, filepath.ToSlash(linkPath)).Scan(&count); err != nil {
		t.Fatalf("count artifact metadata: %v", err)
	}
	if count != 0 {
		t.Fatalf("metadata rows = %d, want 0 after rejected write", count)
	}
}

func TestStaticFileHandlerServesBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "exports"), 0755); err != nil {
		t.Fatalf("create exports dir: %v", err)
	}
	want := []byte("served artifact")
	if err := os.WriteFile(filepath.Join(root, "exports", "spell data.json"), want, 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/exports/spell%20data.json", nil)
	FileHandler(root).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("body = %q, want %q", rec.Body.Bytes(), want)
	}
}

func TestStaticFileHandlerRejectsSymlinkEscapingRoot(t *testing.T) {
	root := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "exports"), 0755); err != nil {
		t.Fatalf("create exports dir: %v", err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(root, "exports", "secret.txt")); err != nil {
		t.Skipf("creating symlink unavailable: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/exports/secret.txt", nil)
	FileHandler(root).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 403 or 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("handler served escaped symlink contents: %q", rec.Body.String())
	}
}

func TestFileHandlerEmptyRootDoesNotServeWorkingDirectory(t *testing.T) {
	name := "artifact-handler-working-dir-sentinel.txt"
	if err := os.WriteFile(name, []byte("cwd sentinel"), 0644); err != nil {
		t.Fatalf("write cwd sentinel: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(name) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/"+name, nil)
	FileHandler("").ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 403 or 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "cwd sentinel") {
		t.Fatalf("handler served from working directory with empty root: %q", rec.Body.String())
	}
}

func TestTraversalPathsAreRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0644); err != nil {
		t.Fatalf("write ok file: %v", err)
	}
	handler := FileHandler(root)

	for _, path := range []string{
		"../secret.txt",
		"..%2fsecret.txt",
		"subdir/../../secret.txt",
		`subdir\..\secret.txt`,
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/files/"+path, nil)
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 403 or 404", rec.Code)
			}
		})
	}
}

func assertUnderRoot(t *testing.T, root string, path string) {
	t.Helper()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		t.Fatalf("relative path: %v", err)
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("path %q is not under root %q", path, root)
	}
}

func openMetadataDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := metadata.Open(filepath.Join(t.TempDir(), "metadata.sqlite"))
	if err != nil {
		t.Fatalf("open metadata DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
