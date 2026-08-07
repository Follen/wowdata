package runtime

import (
	"fmt"
	"strings"

	"wowdata/internal/db2"
	"wowdata/internal/dbd"
	"wowdata/internal/resource"
)

type DBDDefinitionSource interface {
	Definition(tableName string) (string, error)
}

type stagedDBDDefinitionSource interface {
	DefinitionWithStage(tableName string, stage resource.StageID) (string, error)
}

type PartialFileDataReader interface {
	ReadFileDataPartial(fileDataID uint32) ([]byte, error)
}

type stagedPartialFileDataReader interface {
	ReadFileDataPartialWithStage(fileDataID uint32, stage resource.StageID) ([]byte, error)
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
	return l.LoadTableWithStage(store, tableName, 0, resource.StageWaveInitial)
}

func (l *DB2Loader) LoadTableWithStage(store *MemoryDB2Store, tableName string, parent resource.StageID, wave string) error {
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
	fileStage := resource.StartStage("db2-file-read", resource.StageOptions{ParentID: parent, Wave: wave, Instance: tableName, DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
	if l.dbdSource == nil {
		resource.FinishStage(fileStage, fmt.Errorf("DBD definition source is not initialized"))
		return fmt.Errorf("DBD definition source is not initialized")
	}
	definitionStage := resource.StartStage("dbd-definition", resource.StageOptions{ParentID: parent, Wave: wave, Instance: tableName, DependsOn: []resource.StageDependency{resource.Dependency(parent, resource.StageRelationHard)}})
	type fileResult struct {
		data []byte
		err  error
	}
	type definitionResult struct {
		raw string
		err error
	}
	fileCh := make(chan fileResult, 1)
	definitionCh := make(chan definitionResult, 1)
	go func() {
		var result fileResult
		if stagedReader, ok := l.files.(stagedPartialFileDataReader); ok {
			result.data, result.err = stagedReader.ReadFileDataPartialWithStage(fileDataID, fileStage)
		} else if partialReader, ok := l.files.(PartialFileDataReader); ok {
			result.data, result.err = partialReader.ReadFileDataPartial(fileDataID)
		} else {
			result.data, result.err = l.files.ReadFileData(fileDataID)
		}
		fileCh <- result
	}()
	go func() {
		var result definitionResult
		if stagedSource, ok := l.dbdSource.(stagedDBDDefinitionSource); ok {
			result.raw, result.err = stagedSource.DefinitionWithStage(tableName, definitionStage)
		} else {
			result.raw, result.err = l.dbdSource.Definition(tableName)
		}
		definitionCh <- result
	}()
	fileRead := <-fileCh
	definitionRead := <-definitionCh
	resource.FinishStage(fileStage, fileRead.err)
	resource.FinishStage(definitionStage, definitionRead.err)
	if fileRead.err != nil {
		return fileRead.err
	}
	if definitionRead.err != nil {
		return definitionRead.err
	}
	data := fileRead.data
	rawDBD := definitionRead.raw
	parseStage := resource.StartStage("dbd-parse-schema", resource.StageOptions{ParentID: parent, Wave: wave, Instance: tableName, DependsOn: []resource.StageDependency{resource.Dependency(definitionStage, resource.StageRelationHard)}})
	parser, err := dbd.Parse(strings.NewReader(rawDBD))
	if err != nil {
		resource.FinishStage(parseStage, err)
		return err
	}
	entry := parser.GetStructure(l.buildID, "")
	if entry == nil {
		resource.FinishStage(parseStage, fmt.Errorf("missing structure"))
		return fmt.Errorf("no DBD structure for table %s build %s", tableName, l.buildID)
	}
	schema, err := db2.SchemaFromDBD(entry)
	if err != nil {
		resource.FinishStage(parseStage, err)
		return err
	}
	resource.FinishStage(parseStage, nil)
	openStage := resource.StartStage("db2-table-open", resource.StageOptions{ParentID: parent, Wave: wave, Instance: tableName, DependsOn: []resource.StageDependency{resource.Dependency(fileStage, resource.StageRelationJoin), resource.Dependency(parseStage, resource.StageRelationJoin)}})
	reader, err := db2.NewWDCReaderFromBytes(tableName, data, schema)
	if err != nil {
		resource.FinishStage(openStage, err)
		return err
	}
	store.AddTable(tableName, schemaForRuntime(schema), reader)
	resource.FinishStage(openStage, nil)
	return nil
}

func schemaForRuntime(schema []db2.SchemaField) []SchemaField {
	out := make([]SchemaField, 0, len(schema))
	for _, field := range schema {
		out = append(out, SchemaField{Name: field.Name, Type: field.Type.SchemaDescription()})
	}
	return out
}
