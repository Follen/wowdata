package parquet

import (
	"encoding/json"
	"os"
)

func ReadMetadata(path string) (Metadata, error) {
	data, err := os.ReadFile(metadataPath(path))
	if err != nil {
		return Metadata{}, err
	}
	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}
