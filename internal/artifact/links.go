package artifact

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (m *Manager) linkForRel(artifactPath, rel, mimeType string) Link {
	downloadURL := ""
	if m.baseURL != "" {
		downloadURL = m.baseURL + "/" + path.Join(strings.Split(filepath.ToSlash(rel), "/")...)
	}
	return Link{
		Path:        artifactPath,
		URI:         downloadURL,
		DownloadURL: downloadURL,
		MimeType:    mimeType,
		Name:        path.Base(filepath.ToSlash(rel)),
	}
}

func fillFileMetadata(link *Link) error {
	file, err := os.Open(link.Path)
	if err != nil {
		return fmt.Errorf("open artifact file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return fmt.Errorf("hash artifact file: %w", err)
	}
	link.Size = size
	link.SHA256 = fmt.Sprintf("%x", hash.Sum(nil))
	return nil
}
