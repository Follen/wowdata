package parquet

import (
	"os"
	"path/filepath"
	"strconv"

	parquetgo "github.com/parquet-go/parquet-go"
)

type metadataRow struct {
	ID int32 `parquet:"id"`
}

type writerOptions struct {
	write func(*os.File, Metadata) error
}

type WriterOption func(*writerOptions)

func WithWriterForTest(write func(*os.File, Metadata) error) WriterOption {
	return func(opts *writerOptions) {
		opts.write = write
	}
}

func WriteMetadataFile(path string, meta Metadata, options ...WriterOption) error {
	opts := writerOptions{write: writeParquetMetadataFile}
	for _, option := range options {
		option(&opts)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := opts.write(tmp, meta); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func writeParquetMetadataFile(file *os.File, meta Metadata) error {
	writer := parquetgo.NewGenericWriter[metadataRow](file, metadataWriterOptions(meta)...)
	if _, err := writer.Write([]metadataRow{{ID: int32(meta.DB2FileDataID)}}); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func metadataWriterOptions(meta Metadata) []parquetgo.WriterOption {
	values := metadataValues(meta)
	options := make([]parquetgo.WriterOption, 0, len(values))
	for key, value := range values {
		options = append(options, parquetgo.KeyValueMetadata(key, value))
	}
	return options
}

func metadataValues(meta Metadata) map[string]string {
	return map[string]string{
		"wowdata.region":               meta.Region,
		"wowdata.product":              meta.Product,
		"wowdata.build_key":            meta.BuildKey,
		"wowdata.locale":               meta.Locale,
		"wowdata.table":                meta.Table,
		"wowdata.db2_file_data_id":     strconv.Itoa(meta.DB2FileDataID),
		"wowdata.dbd_definition_hash":  meta.DBDDefinitionHash,
		"wowdata.decoder_version":      meta.DecoderVersion,
		"wowdata.materializer_version": meta.MaterializerVersion,
	}
}
