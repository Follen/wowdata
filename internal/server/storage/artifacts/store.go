package artifacts

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"wowdata/internal/server/storage/metadata"
	"wowdata/internal/shared/artifact"
)

type Config struct {
	Root    string
	BaseURL string
}

type RequestContext struct {
	Region   string
	Product  string
	Locale   string
	BuildKey string
}

type Record struct {
	Path        string
	URI         string
	DownloadURL string
	MIMEType    string
	Name        string
	SHA256      string
	Size        int64
}

type Store struct {
	manager *artifact.Manager
	db      *sql.DB
	root    string
}

func NewStore(cfg Config, db *sql.DB) *Store {
	root := cleanRoot(cfg.Root)
	return &Store{
		manager: artifact.NewManager(artifact.Config{
			Root:    cfg.Root,
			BaseURL: cfg.BaseURL,
		}),
		db:   db,
		root: root,
	}
}

func (s *Store) Reserve(category, filename, mimeType string) (string, Record, error) {
	if s == nil || s.manager == nil {
		return "", Record{}, fmt.Errorf("artifact store is required")
	}
	path, link, err := s.manager.Reserve(category, filename, mimeType)
	if err != nil {
		return "", Record{}, err
	}
	return path, recordFromLink(link), nil
}

func (s *Store) StoreBytes(ctx context.Context, rc RequestContext, category, filename, mimeType string, data []byte) (Record, error) {
	path, _, err := s.Reserve(category, filename, mimeType)
	if err != nil {
		return Record{}, err
	}
	if err := writeFileAtomically(s.root, path, data); err != nil {
		return Record{}, fmt.Errorf("write artifact: %w", err)
	}
	link, err := s.manager.LinkForPath(path, mimeType)
	if err != nil {
		return Record{}, err
	}
	record := recordFromLink(link)
	if s.db != nil {
		if err := metadata.UpsertArtifact(ctx, s.db, metadata.Artifact{
			Region:      rc.Region,
			Product:     rc.Product,
			Locale:      rc.Locale,
			BuildKey:    rc.BuildKey,
			Path:        filepath.ToSlash(record.Path),
			DownloadURL: record.DownloadURL,
			MIMEType:    record.MIMEType,
			Size:        record.Size,
			SHA256:      record.SHA256,
		}); err != nil {
			return Record{}, fmt.Errorf("upsert artifact metadata: %w", err)
		}
	}
	return record, nil
}

func (s *Store) RecordExisting(ctx context.Context, rc RequestContext, path, mimeType string) (Record, error) {
	if s == nil || s.manager == nil {
		return Record{}, fmt.Errorf("artifact store is required")
	}
	link, err := s.manager.LinkForPath(path, mimeType)
	if err != nil {
		return Record{}, err
	}
	record := recordFromLink(link)
	if s.db != nil {
		if err := metadata.UpsertArtifact(ctx, s.db, metadata.Artifact{
			Region:      rc.Region,
			Product:     rc.Product,
			Locale:      rc.Locale,
			BuildKey:    rc.BuildKey,
			Path:        filepath.ToSlash(record.Path),
			DownloadURL: record.DownloadURL,
			MIMEType:    record.MIMEType,
			Size:        record.Size,
			SHA256:      record.SHA256,
		}); err != nil {
			return Record{}, fmt.Errorf("upsert artifact metadata: %w", err)
		}
	}
	return record, nil
}

func FileHandler(root string) http.Handler {
	root = cleanRoot(root)
	if root == "" {
		return http.HandlerFunc(notFound)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel, ok := cleanRequestPath(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		filePath := filepath.Join(root, rel)
		if !insideRoot(root, filePath) {
			http.NotFound(w, r)
			return
		}
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		realPath, err := filepath.EvalSymlinks(filePath)
		if err != nil || !insideRoot(realRoot, realPath) {
			http.NotFound(w, r)
			return
		}
		file, err := os.Open(realPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	})
}

func recordFromLink(link artifact.Link) Record {
	return Record{
		Path:        link.Path,
		URI:         link.URI,
		DownloadURL: link.DownloadURL,
		MIMEType:    link.MimeType,
		Name:        link.Name,
		SHA256:      link.SHA256,
		Size:        link.Size,
	}
}

func cleanRequestPath(r *http.Request) (string, bool) {
	rawPath := r.URL.EscapedPath()
	if !strings.HasPrefix(rawPath, "/files/") {
		return "", false
	}
	rawRel := strings.TrimPrefix(rawPath, "/files/")
	if rawRel == "" {
		return "", false
	}
	rel, err := url.PathUnescape(rawRel)
	if err != nil {
		return "", false
	}
	if strings.Contains(rel, `\`) {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

func insideRoot(root, path string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(absRoot), filepath.Clean(absPath))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func writeFileAtomically(root string, path string, data []byte) error {
	if root == "" {
		return fmt.Errorf("artifact root is required")
	}
	if err := validateRealParentInsideRoot(root, path); err != nil {
		return err
	}
	if err := rejectFinalSymlink(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(0644); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := rejectFinalSymlink(path); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		if retryErr := replaceExistingFile(tempPath, path); retryErr != nil {
			return err
		}
	}
	cleanup = false
	return nil
}

func validateRealParentInsideRoot(root string, path string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	if !insideRoot(realRoot, realParent) {
		return fmt.Errorf("artifact parent %q escapes artifact root", parent)
	}
	return nil
}

func replaceExistingFile(tempPath string, path string) error {
	if err := rejectFinalSymlink(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("artifact path %q is a directory", path)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}

func rejectFinalSymlink(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("artifact path %q is a symlink", path)
	}
	return nil
}

func notFound(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func cleanRoot(root string) string {
	if root == "" || filepath.Clean(root) == "." {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}
