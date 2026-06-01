package parquet

import (
	"fmt"
	"os"
	"strconv"

	parquetgo "github.com/parquet-go/parquet-go"
)

func ReadMetadata(path string) (Metadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return Metadata{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return Metadata{}, err
	}
	pf, err := parquetgo.OpenFile(file, info.Size())
	if err != nil {
		return Metadata{}, err
	}

	get := func(key string) (string, error) {
		value, ok := pf.Lookup(key)
		if !ok {
			return "", fmt.Errorf("missing parquet metadata key %q", key)
		}
		return value, nil
	}

	db2FileDataIDValue, err := get("wowdata.db2_file_data_id")
	if err != nil {
		return Metadata{}, err
	}
	db2FileDataID, err := strconv.Atoi(db2FileDataIDValue)
	if err != nil {
		return Metadata{}, fmt.Errorf("parse parquet metadata db2 file data id: %w", err)
	}

	meta := Metadata{DB2FileDataID: db2FileDataID}
	if meta.Region, err = get("wowdata.region"); err != nil {
		return Metadata{}, err
	}
	if meta.Product, err = get("wowdata.product"); err != nil {
		return Metadata{}, err
	}
	if meta.BuildKey, err = get("wowdata.build_key"); err != nil {
		return Metadata{}, err
	}
	if meta.Locale, err = get("wowdata.locale"); err != nil {
		return Metadata{}, err
	}
	if meta.Table, err = get("wowdata.table"); err != nil {
		return Metadata{}, err
	}
	if meta.DBDDefinitionHash, err = get("wowdata.dbd_definition_hash"); err != nil {
		return Metadata{}, err
	}
	if meta.DecoderVersion, err = get("wowdata.decoder_version"); err != nil {
		return Metadata{}, err
	}
	if meta.MaterializerVersion, err = get("wowdata.materializer_version"); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}
