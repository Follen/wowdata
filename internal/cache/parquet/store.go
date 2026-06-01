package parquet

import (
	"errors"
	"fmt"
	"os"

	"wowdata/internal/cache"
)

var ErrStale = errors.New("stale parquet metadata")

type Metadata struct {
	Region              string `json:"region"`
	Product             string `json:"product"`
	BuildKey            string `json:"build_key"`
	Locale              string `json:"locale"`
	Table               string `json:"table"`
	DBDDefinitionHash   string `json:"dbd_definition_hash"`
	DecoderVersion      string `json:"decoder_version"`
	MaterializerVersion string `json:"materializer_version"`
	DB2FileDataID       uint32 `json:"db2_file_data_id"`
}

func (m Metadata) Matches(want Metadata) bool {
	return m.Region == want.Region &&
		m.Product == want.Product &&
		m.BuildKey == want.BuildKey &&
		m.Locale == want.Locale &&
		m.Table == want.Table &&
		m.DBDDefinitionHash == want.DBDDefinitionHash &&
		m.DecoderVersion == want.DecoderVersion &&
		m.MaterializerVersion == want.MaterializerVersion &&
		m.DB2FileDataID == want.DB2FileDataID
}

func PathFor(root string, metadata Metadata) string {
	return cache.DB2ParquetPath(root, metadata.Region, metadata.Product, metadata.BuildKey, metadata.Locale, metadata.Table)
}

func ValidateExisting(path string, want Metadata) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("parquet file unavailable: %w", err)
	}

	got, err := ReadMetadata(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStale, err)
	}
	if !got.Matches(want) {
		return ErrStale
	}
	return nil
}
