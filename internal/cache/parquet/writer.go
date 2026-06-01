package parquet

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	parquetgo "github.com/parquet-go/parquet-go"
)

type metadataRow struct {
	ID int32 `parquet:"id"`
}

type Field struct {
	Name string
	Type string
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
	return writeFile(path, func(file *os.File) error {
		return opts.write(file, meta)
	})
}

func WriteRowsFile(path string, meta Metadata, schema []Field, rows []map[string]interface{}) error {
	return writeFile(path, func(file *os.File) error {
		return writeParquetRowsFile(file, meta, schema, rows)
	})
}

func writeFile(path string, write func(*os.File) error) error {
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

	if err := write(tmp); err != nil {
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

func writeParquetRowsFile(file *os.File, meta Metadata, fields []Field, rows []map[string]interface{}) error {
	schema, err := rowSchema(fields)
	if err != nil {
		return err
	}
	writer := parquetgo.NewWriter(file, append(metadataWriterOptions(meta), schema)...)
	for _, row := range rows {
		if _, err := writer.WriteRows([]parquetgo.Row{schema.Deconstruct(nil, normalizeRow(fields, row))}); err != nil {
			_ = writer.Close()
			return err
		}
	}
	return writer.Close()
}

func rowSchema(fields []Field) (*parquetgo.Schema, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("parquet schema is required")
	}
	group := parquetgo.Group{}
	for _, field := range fields {
		if field.Name == "" {
			return nil, fmt.Errorf("parquet field name is required")
		}
		node, err := parquetNode(field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		group[field.Name] = node
	}
	return parquetgo.NewSchema("db2", group), nil
}

func parquetNode(fieldType string) (parquetgo.Node, error) {
	switch fieldType {
	case "string":
		return parquetgo.String(), nil
	case "bool", "boolean":
		return parquetgo.Leaf(parquetgo.BooleanType), nil
	case "float", "float32":
		return parquetgo.Leaf(parquetgo.FloatType), nil
	case "double", "float64":
		return parquetgo.Leaf(parquetgo.DoubleType), nil
	case "int8", "int16", "int32", "int", "relation", "noninlineid":
		return parquetgo.Int(32), nil
	case "uint8", "uint16", "uint32":
		return parquetgo.Uint(32), nil
	case "int64":
		return parquetgo.Int(64), nil
	case "uint64":
		return parquetgo.Uint(64), nil
	default:
		return nil, fmt.Errorf("unsupported parquet field type %q", fieldType)
	}
}

func normalizeRow(fields []Field, row map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		out[field.Name] = normalizeValue(field.Type, row[field.Name])
	}
	return out
}

func normalizeValue(fieldType string, value interface{}) interface{} {
	switch fieldType {
	case "int8", "int16", "int32", "int", "relation", "noninlineid":
		return int32FromValue(value)
	case "uint8", "uint16", "uint32":
		return uint32FromValue(value)
	case "int64":
		return int64FromValue(value)
	case "uint64":
		return uint64FromValue(value)
	case "float", "float32":
		if v, ok := value.(float32); ok {
			return v
		}
		if v, ok := value.(float64); ok {
			return float32(v)
		}
	case "double", "float64":
		if v, ok := value.(float32); ok {
			return float64(v)
		}
	case "string":
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
	return value
}

func int32FromValue(value interface{}) int32 {
	switch v := value.(type) {
	case int:
		return int32(v)
	case int8:
		return int32(v)
	case int16:
		return int32(v)
	case int32:
		return v
	case int64:
		return int32(v)
	case uint:
		return int32(v)
	case uint8:
		return int32(v)
	case uint16:
		return int32(v)
	case uint32:
		return int32(v)
	case uint64:
		return int32(v)
	default:
		return 0
	}
}

func uint32FromValue(value interface{}) uint32 {
	switch v := value.(type) {
	case int:
		return uint32(v)
	case int8:
		return uint32(v)
	case int16:
		return uint32(v)
	case int32:
		return uint32(v)
	case int64:
		return uint32(v)
	case uint:
		return uint32(v)
	case uint8:
		return uint32(v)
	case uint16:
		return uint32(v)
	case uint32:
		return v
	case uint64:
		return uint32(v)
	default:
		return 0
	}
}

func int64FromValue(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	default:
		return 0
	}
}

func uint64FromValue(value interface{}) uint64 {
	switch v := value.(type) {
	case int:
		return uint64(v)
	case int8:
		return uint64(v)
	case int16:
		return uint64(v)
	case int32:
		return uint64(v)
	case int64:
		return uint64(v)
	case uint:
		return uint64(v)
	case uint8:
		return uint64(v)
	case uint16:
		return uint64(v)
	case uint32:
		return uint64(v)
	case uint64:
		return v
	default:
		return 0
	}
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
