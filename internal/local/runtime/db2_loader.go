package runtime

import (
	"fmt"
	"strings"

	"wowdata/internal/db2"
	"wowdata/internal/dbd"
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
	entry := parser.GetStructure(l.buildID, "")
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
	out := make([]SchemaField, 0, len(schema))
	for _, field := range schema {
		out = append(out, SchemaField{Name: field.Name, Type: field.Type.SchemaDescription(), ArrayLen: field.ArrayLen})
	}
	return out
}
