package parquet

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func WriteMetadata(path string, metadata Metadata) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := touchParquet(path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(metadataPath(path), data, 0o644)
}

func touchParquet(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	return file.Close()
}

func metadataPath(path string) string {
	return path + ".metadata.json"
}
