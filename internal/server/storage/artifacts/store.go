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

	"wowdata/internal/artifact"
	"wowdata/internal/server/storage/metadata"
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
}

func NewStore(cfg Config, db *sql.DB) *Store {
	return &Store{
		manager: artifact.NewManager(artifact.Config{
			Root:    cfg.Root,
			BaseURL: cfg.BaseURL,
		}),
		db: db,
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
	if err := os.WriteFile(path, data, 0644); err != nil {
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

func FileHandler(root string) http.Handler {
	root = filepath.Clean(root)
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
		if realPath, err := filepath.EvalSymlinks(filePath); err == nil && !insideRoot(root, realPath) {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filePath)
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
