package parquet

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	parquetgo "github.com/parquet-go/parquet-go"
)

type metadataRow struct {
	ID int32 `parquet:"id"`
}

type Field struct {
	Name     string
	Type     string
	ArrayLen int
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

type RowSource interface {
	NextRow() (map[string]interface{}, bool, error)
	Close() error
}

func WriteRowSourceFile(path string, meta Metadata, schema []Field, rows RowSource) (int, error) {
	var rowCount int
	err := writeFile(path, func(file *os.File) error {
		count, err := writeParquetRowSourceFile(file, meta, schema, rows)
		rowCount = count
		return err
	})
	return rowCount, err
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
	columns, err := parquetColumns(fields)
	if err != nil {
		return err
	}
	schema, err := rowSchema(columns)
	if err != nil {
		return err
	}
	writer := parquetgo.NewWriter(file, append(metadataWriterOptions(meta), schema)...)
	for _, row := range rows {
		normalized, err := normalizeRow(columns, row)
		if err != nil {
			_ = writer.Close()
			return err
		}
		if _, err := writer.WriteRows([]parquetgo.Row{schema.Deconstruct(nil, normalized)}); err != nil {
			_ = writer.Close()
			return err
		}
	}
	return writer.Close()
}

func writeParquetRowSourceFile(file *os.File, meta Metadata, fields []Field, rows RowSource) (int, error) {
	columns, err := parquetColumns(fields)
	if err != nil {
		return 0, err
	}
	schema, err := rowSchema(columns)
	if err != nil {
		return 0, err
	}
	writer := parquetgo.NewWriter(file, append(metadataWriterOptions(meta), schema)...)
	defer rows.Close()
	rowCount := 0
	for {
		row, ok, err := rows.NextRow()
		if err != nil {
			_ = writer.Close()
			return 0, err
		}
		if !ok {
			break
		}
		normalized, err := normalizeRow(columns, row)
		if err != nil {
			_ = writer.Close()
			return 0, err
		}
		if _, err := writer.WriteRows([]parquetgo.Row{schema.Deconstruct(nil, normalized)}); err != nil {
			_ = writer.Close()
			return 0, err
		}
		rowCount++
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}
	return rowCount, nil
}

type parquetColumn struct {
	Name       string
	SourceName string
	Type       string
	ArrayIndex int
}

func parquetColumns(fields []Field) ([]parquetColumn, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("parquet schema is required")
	}
	columns := make([]parquetColumn, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		if field.Name == "" {
			return nil, fmt.Errorf("parquet field name is required")
		}
		fieldType, err := canonicalFieldType(field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		if field.ArrayLen < 0 {
			return nil, fmt.Errorf("field %s: array length must not be negative", field.Name)
		}
		if field.ArrayLen == 0 {
			if _, ok := seen[field.Name]; ok {
				return nil, fmt.Errorf("duplicate parquet field name %q", field.Name)
			}
			seen[field.Name] = struct{}{}
			columns = append(columns, parquetColumn{Name: field.Name, SourceName: field.Name, Type: fieldType, ArrayIndex: -1})
			continue
		}
		for i := 0; i < field.ArrayLen; i++ {
			name := fmt.Sprintf("%s_%d", field.Name, i)
			if _, ok := seen[name]; ok {
				return nil, fmt.Errorf("duplicate parquet field name %q", name)
			}
			seen[name] = struct{}{}
			columns = append(columns, parquetColumn{Name: name, SourceName: field.Name, Type: fieldType, ArrayIndex: i})
		}
	}
	return columns, nil
}

func rowSchema(columns []parquetColumn) (*parquetgo.Schema, error) {
	group := parquetgo.Group{}
	for _, column := range columns {
		node, err := parquetNode(column.Type)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", column.Name, err)
		}
		group[column.Name] = node
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

func canonicalFieldType(fieldType string) (string, error) {
	switch fieldType {
	case "dbFieldString":
		return "string", nil
	case "dbFieldInt8":
		return "int8", nil
	case "dbFieldUInt8":
		return "uint8", nil
	case "dbFieldInt16":
		return "int16", nil
	case "dbFieldUInt16":
		return "uint16", nil
	case "dbFieldInt32":
		return "int32", nil
	case "dbFieldUInt32":
		return "uint32", nil
	case "dbFieldInt64":
		return "int64", nil
	case "dbFieldUInt64":
		return "uint64", nil
	case "dbFieldFloat":
		return "float32", nil
	case "dbFieldRelation":
		return "relation", nil
	case "dbFieldNonInlineID":
		return "noninlineid", nil
	}
	if _, err := parquetNode(fieldType); err != nil {
		return "", err
	}
	return fieldType, nil
}

func normalizeRow(columns []parquetColumn, row map[string]interface{}) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(columns))
	for _, column := range columns {
		value := row[column.SourceName]
		if column.ArrayIndex >= 0 {
			element, err := arrayElement(column.SourceName, value, column.ArrayIndex)
			if err != nil {
				return nil, err
			}
			value = element
		}
		normalized, err := normalizeValue(column.Type, value)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", column.Name, err)
		}
		out[column.Name] = normalized
	}
	return out, nil
}

func arrayElement(fieldName string, value interface{}, index int) (interface{}, error) {
	if value == nil {
		return nil, fmt.Errorf("field %s: array value is nil", fieldName)
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("field %s: array value has type %T", fieldName, value)
	}
	if rv.Len() <= index {
		return nil, fmt.Errorf("field %s: array length %d is less than required %d", fieldName, rv.Len(), index+1)
	}
	return rv.Index(index).Interface(), nil
}

func normalizeValue(fieldType string, value interface{}) (interface{}, error) {
	switch fieldType {
	case "int8", "int16", "int32", "int", "relation", "noninlineid":
		return int32FromValue(fieldType, value)
	case "uint8", "uint16", "uint32":
		return uint32FromValue(fieldType, value)
	case "int64":
		return int64FromValue(value)
	case "uint64":
		return uint64FromValue(value)
	case "float", "float32":
		if v, ok := value.(float32); ok {
			return v, nil
		}
		if v, ok := value.(float64); ok {
			if v > math.MaxFloat32 || v < -math.MaxFloat32 {
				return nil, fmt.Errorf("%v overflows float32", v)
			}
			return float32(v), nil
		}
		return nil, fmt.Errorf("cannot convert %T to float32", value)
	case "double", "float64":
		if v, ok := value.(float32); ok {
			return float64(v), nil
		}
		if v, ok := value.(float64); ok {
			return v, nil
		}
		return nil, fmt.Errorf("cannot convert %T to float64", value)
	case "string":
		if value == nil {
			return "", nil
		}
		return fmt.Sprint(value), nil
	}
	return value, nil
}

func int32Bounds(fieldType string) (int64, int64) {
	switch fieldType {
	case "int8":
		return math.MinInt8, math.MaxInt8
	case "int16":
		return math.MinInt16, math.MaxInt16
	default:
		return math.MinInt32, math.MaxInt32
	}
}

func uint32Max(fieldType string) uint64 {
	switch fieldType {
	case "uint8":
		return math.MaxUint8
	case "uint16":
		return math.MaxUint16
	default:
		return math.MaxUint32
	}
}

func int32FromValue(fieldType string, value interface{}) (int32, error) {
	min, max := int32Bounds(fieldType)
	checkSigned := func(v int64) (int32, error) {
		if v < min || v > max {
			return 0, fmt.Errorf("%d overflows %s", v, fieldType)
		}
		return int32(v), nil
	}
	checkUnsigned := func(v uint64) (int32, error) {
		if v > uint64(max) {
			return 0, fmt.Errorf("%d overflows %s", v, fieldType)
		}
		return int32(v), nil
	}
	switch v := value.(type) {
	case int:
		return checkSigned(int64(v))
	case int8:
		return checkSigned(int64(v))
	case int16:
		return checkSigned(int64(v))
	case int32:
		return checkSigned(int64(v))
	case int64:
		return checkSigned(v)
	case uint:
		return checkUnsigned(uint64(v))
	case uint8:
		return checkUnsigned(uint64(v))
	case uint16:
		return checkUnsigned(uint64(v))
	case uint32:
		return checkUnsigned(uint64(v))
	case uint64:
		return checkUnsigned(v)
	default:
		return 0, fmt.Errorf("cannot convert %T to %s", value, fieldType)
	}
}

func uint32FromValue(fieldType string, value interface{}) (uint32, error) {
	max := uint32Max(fieldType)
	checkSigned := func(v int64) (uint32, error) {
		if v < 0 {
			return 0, fmt.Errorf("cannot convert negative %d to %s", v, fieldType)
		}
		if uint64(v) > max {
			return 0, fmt.Errorf("%d overflows %s", v, fieldType)
		}
		return uint32(v), nil
	}
	checkUnsigned := func(v uint64) (uint32, error) {
		if v > max {
			return 0, fmt.Errorf("%d overflows %s", v, fieldType)
		}
		return uint32(v), nil
	}
	switch v := value.(type) {
	case int:
		return checkSigned(int64(v))
	case int8:
		return checkSigned(int64(v))
	case int16:
		return checkSigned(int64(v))
	case int32:
		return checkSigned(int64(v))
	case int64:
		return checkSigned(v)
	case uint:
		return checkUnsigned(uint64(v))
	case uint8:
		return checkUnsigned(uint64(v))
	case uint16:
		return checkUnsigned(uint64(v))
	case uint32:
		return checkUnsigned(uint64(v))
	case uint64:
		return checkUnsigned(v)
	default:
		return 0, fmt.Errorf("cannot convert %T to %s", value, fieldType)
	}
}

func int64FromValue(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return uint64ToInt64(uint64(v))
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return uint64ToInt64(v)
	default:
		return 0, fmt.Errorf("cannot convert %T to int64", value)
	}
}

func uint64ToInt64(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("%d overflows int64", v)
	}
	return int64(v), nil
}

func uint64FromValue(value interface{}) (uint64, error) {
	checkSigned := func(v int64) (uint64, error) {
		if v < 0 {
			return 0, fmt.Errorf("cannot convert negative %d to uint64", v)
		}
		return uint64(v), nil
	}
	switch v := value.(type) {
	case int:
		return checkSigned(int64(v))
	case int8:
		return checkSigned(int64(v))
	case int16:
		return checkSigned(int64(v))
	case int32:
		return checkSigned(int64(v))
	case int64:
		return checkSigned(v)
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to uint64", value)
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
