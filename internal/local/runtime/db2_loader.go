package runtime

import (
	"encoding/binary"
	"fmt"
	"strings"

	"wowdata/internal/shared/db2"
	"wowdata/internal/shared/dbd"
)

type DBDDefinitionSource interface {
	Definition(tableName string) (string, error)
}

type PartialFileDataReader interface {
	ReadFileDataPartial(fileDataID uint32) ([]byte, error)
}

type DB2Loader struct {
	manifest  *dbd.Manifest
	dbdSource DBDDefinitionSource
	files     FileDataReader
	buildID   string
}

func NewDB2Loader(manifest *dbd.Manifest, dbdSource DBDDefinitionSource, files FileDataReader, buildID string) *DB2Loader {
	return &DB2Loader{manifest: manifest, dbdSource: dbdSource, files: files, buildID: buildID}
}

func (l *DB2Loader) LoadTable(store *MemoryDB2Store, tableName string) error {
	if store == nil {
		return fmt.Errorf("DB2 store is nil")
	}
	if l.manifest == nil {
		return fmt.Errorf("DBD manifest is not loaded")
	}
	fileDataID, ok := l.manifest.GetByTableName(tableName)
	if !ok {
		return fmt.Errorf("table not found in DBD manifest: %s", tableName)
	}
	if l.files == nil {
		return fmt.Errorf("CASC file reader is not initialized")
	}
	var data []byte
	var err error
	if partialReader, ok := l.files.(PartialFileDataReader); ok {
		data, err = partialReader.ReadFileDataPartial(fileDataID)
	} else {
		data, err = l.files.ReadFileData(fileDataID)
	}
	if err != nil {
		return err
	}
	if l.dbdSource == nil {
		return fmt.Errorf("DBD definition source is not initialized")
	}
	rawDBD, err := l.dbdSource.Definition(tableName)
	if err != nil {
		return err
	}
	parser, err := dbd.Parse(strings.NewReader(rawDBD))
	if err != nil {
		return err
	}
	layoutHash, err := db2LayoutHash(data)
	if err != nil {
		return err
	}
	entry := parser.GetStructure(l.buildID, layoutHash)
	if entry == nil {
		return fmt.Errorf("no DBD structure for table %s build %s", tableName, l.buildID)
	}
	schema, err := db2.SchemaFromDBD(entry)
	if err != nil {
		return err
	}
	reader, err := db2.NewWDCReaderFromBytes(tableName, data, schema)
	if err != nil {
		return err
	}
	store.AddTable(tableName, schemaForRuntime(schema), reader)
	return nil
}

func schemaForRuntime(schema []db2.SchemaField) []SchemaField {
	schema = db2.SchemaWithSyntheticID(schema)
	out := make([]SchemaField, 0, len(schema))
	for _, field := range schema {
		out = append(out, SchemaField{Name: field.Name, Type: field.Type.SchemaDescription(), ArrayLen: field.ArrayLen})
	}
	return out
}

func db2LayoutHash(data []byte) (string, error) {
	if len(data) < 24 {
		return "", fmt.Errorf("DB2 data too short for layout hash: %d bytes", len(data))
	}
	pos := 4
	magic := binary.LittleEndian.Uint32(data[:4])
	if magic == 0x35434457 {
		if len(data) < 156 {
			return "", fmt.Errorf("WDC5 data too short for layout hash: %d bytes", len(data))
		}
		pos += 4 + 128
	}
	pos += 4 // recordCount
	pos += 4 // fieldCount
	pos += 4 // recordSize
	pos += 4 // stringTableSize
	pos += 4 // tableHash
	if pos+4 > len(data) {
		return "", fmt.Errorf("DB2 data too short for layout hash: %d bytes", len(data))
	}
	layout := binary.LittleEndian.Uint32(data[pos : pos+4])
	return fmt.Sprintf("%08X", layout), nil
}
