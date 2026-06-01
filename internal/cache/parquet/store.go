package parquet

import (
	"errors"
	"path/filepath"
)

var ErrStale = errors.New("stale parquet metadata")

type Metadata struct {
	Region              string
	Product             string
	BuildKey            string
	Locale              string
	Table               string
	DB2FileDataID       int
	DBDDefinitionHash   string
	DecoderVersion      string
	MaterializerVersion string
}

func (m Metadata) Matches(want Metadata) bool {
	return m.Region == want.Region &&
		m.Product == want.Product &&
		m.BuildKey == want.BuildKey &&
		m.Locale == want.Locale &&
		m.Table == want.Table &&
		m.DB2FileDataID == want.DB2FileDataID &&
		m.DBDDefinitionHash == want.DBDDefinitionHash &&
		m.DecoderVersion == want.DecoderVersion &&
		m.MaterializerVersion == want.MaterializerVersion
}

func PathFor(root string, meta Metadata) string {
	return filepath.Join(root, "db2", meta.Region, meta.Product, meta.BuildKey, meta.Locale, meta.Table+".parquet")
}

func ValidateExisting(path string, want Metadata) (Metadata, error) {
	got, err := ReadMetadata(path)
	if err != nil {
		return Metadata{}, err
	}
	if !got.Matches(want) {
		return got, ErrStale
	}
	return got, nil
}
