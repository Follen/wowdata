package artifact

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Config struct {
	Root           string
	BaseURL        string
	RetentionHours int
}

type Link struct {
	Path        string
	URI         string
	DownloadURL string
	MimeType    string
	Name        string
	Size        int64
	SHA256      string
}

type Manager struct {
	root           string
	baseURL        string
	retentionHours int
}

func NewManager(cfg Config) *Manager {
	root := ""
	if cfg.Root != "" {
		if abs, err := filepath.Abs(cfg.Root); err == nil {
			root = filepath.Clean(abs)
		} else {
			root = filepath.Clean(cfg.Root)
		}
	}
	return &Manager{
		root:           root,
		baseURL:        strings.TrimRight(cfg.BaseURL, "/"),
		retentionHours: cfg.RetentionHours,
	}
}

func (m *Manager) Reserve(category, filename, mimeType string) (string, Link, error) {
	if m == nil || m.root == "" {
		return "", Link{}, fmt.Errorf("artifact root is required")
	}
	rel, err := cleanRelative(category, filename)
	if err != nil {
		return "", Link{}, err
	}
	artifactPath, err := m.pathForRel(rel)
	if err != nil {
		return "", Link{}, err
	}
	if err := ensureArtifactDir(artifactPath); err != nil {
		return "", Link{}, err
	}
	if err := m.ensureRealParentInsideRoot(artifactPath); err != nil {
		return "", Link{}, err
	}
	link := m.linkForRel(artifactPath, rel, mimeType)
	return artifactPath, link, nil
}

func (m *Manager) LinkForPath(path, mimeType string) (Link, error) {
	if m == nil || m.root == "" {
		return Link{}, fmt.Errorf("artifact root is required")
	}
	rel, err := m.relForPath(path)
	if err != nil {
		return Link{}, err
	}
	link := m.linkForRel(filepath.Clean(path), rel, mimeType)
	if err := fillFileMetadata(&link); err != nil {
		return Link{}, err
	}
	return link, nil
}
