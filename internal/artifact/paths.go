package artifact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cleanRelative(parts ...string) (string, error) {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return "", fmt.Errorf("artifact path part is required")
		}
		if filepath.IsAbs(part) {
			return "", fmt.Errorf("artifact path %q must be relative", part)
		}
		clean := filepath.Clean(filepath.FromSlash(part))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("artifact path %q escapes artifact root", part)
		}
		cleaned = append(cleaned, clean)
	}
	return filepath.Join(cleaned...), nil
}

func (m *Manager) pathForRel(rel string) (string, error) {
	artifactPath := filepath.Join(m.root, rel)
	if err := m.ensureTextPathInsideRoot(artifactPath); err != nil {
		return "", err
	}
	return artifactPath, nil
}

func (m *Manager) relForPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve artifact path: %w", err)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(m.root, abs)
	if err != nil {
		return "", fmt.Errorf("resolve artifact relative path: %w", err)
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path %q escapes artifact root", path)
	}
	if err := m.ensureRealPathInsideRoot(abs); err != nil {
		return "", err
	}
	return rel, nil
}

func (m *Manager) ensureTextPathInsideRoot(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve artifact path: %w", err)
	}
	rel, err := filepath.Rel(m.root, filepath.Clean(abs))
	if err != nil {
		return fmt.Errorf("resolve artifact relative path: %w", err)
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("artifact path %q escapes artifact root", path)
	}
	return nil
}

func ensureArtifactDir(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	return nil
}

func (m *Manager) ensureRealPathInsideRoot(path string) error {
	root, err := filepath.EvalSymlinks(m.root)
	if err != nil {
		return fmt.Errorf("resolve artifact root symlinks: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve artifact path symlinks: %w", err)
	}
	return ensureInside(filepath.Clean(root), filepath.Clean(realPath), path)
}

func (m *Manager) ensureRealParentInsideRoot(path string) error {
	root, err := filepath.EvalSymlinks(m.root)
	if err != nil {
		return fmt.Errorf("resolve artifact root symlinks: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("resolve artifact parent symlinks: %w", err)
	}
	return ensureInside(filepath.Clean(root), filepath.Clean(parent), path)
}

func ensureInside(root, path, original string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("resolve artifact real path: %w", err)
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("artifact path %q escapes artifact root", original)
	}
	return nil
}
