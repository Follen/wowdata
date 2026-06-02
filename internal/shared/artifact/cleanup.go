package artifact

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (m *Manager) CleanupExpired() error {
	if m == nil || m.root == "" {
		return fmt.Errorf("artifact root is required")
	}
	cutoff := time.Now().Add(-time.Duration(m.retentionHours) * time.Hour)
	return filepath.WalkDir(m.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().After(cutoff) {
			return os.Remove(path)
		}
		return nil
	})
}
