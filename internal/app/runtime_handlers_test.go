package app

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appruntime "wowdata/internal/local/runtime"
	"wowdata/internal/wowdata"
)

type fakeDB2Store struct{}

func (fakeDB2Store) Schema(table string) ([]appruntime.SchemaField, int, error) {
	return []appruntime.SchemaField{{Name: "ID", Type: "dbFieldNonInlineID"}, {Name: "Name_lang", Type: "dbFieldString"}, {Name: "EffectMiscValue", Type: "dbFieldInt32", ArrayLen: 2}}, 2, nil
}

func (fakeDB2Store) Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"ID": uint32(123), "Name_lang": "Fireball", "fields": strings.Join(fields, ","), "filter": filter, "ids": ids}}, nil
}

func (fakeDB2Store) Search(table string, field string, query string, limit int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"ID": uint32(456), field: "Greater Fireball", "limit": limit}}, nil
}

func (fakeDB2Store) ForeignKey(table string, field string, value uint32, limit int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"ID": uint32(1), field: value}}, nil
}

func (fakeDB2Store) Stream(table string, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"ID": uint32(789)}}, nil
}

func TestRuntimeDB2RowsReturnsStoreRows(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2HandlerWithStore(fakeDB2Store{})}, "query", "rows", "SpellName", "--id", "123")
	if err != nil {
		t.Fatalf("db2 rows returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("db2 rows should succeed with runtime store:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"Fireball"`) {
		t.Fatalf("db2 rows should include store row:\n%s", stdout)
	}
}

func TestRuntimeDB2RowsPassesFieldsAndFilter(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2HandlerWithStore(fakeDB2Store{})}, "query", "rows", "SpellName", "--fields", "ID,Name_lang", "--filter", "Name_lang=Fireball")
	if err != nil {
		t.Fatalf("db2 rows returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"fields": "ID,Name_lang"`) {
		t.Fatalf("db2 rows should pass --fields to store:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"filter": "Name_lang=Fireball"`) {
		t.Fatalf("db2 rows should pass --filter to store:\n%s", stdout)
	}
}

func TestRuntimeDB2RowsAcceptsMultipleIDs(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2HandlerWithStore(fakeDB2Store{})}, "query", "rows", "SpellName", "--id", "1,2", "--ids", "3")
	if err != nil {
		t.Fatalf("db2 rows returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ids": [`) || !strings.Contains(stdout, "1") || !strings.Contains(stdout, "2") || !strings.Contains(stdout, "3") {
		t.Fatalf("db2 rows should pass all requested IDs to store:\n%s", stdout)
	}
}

func TestRuntimeDB2RowsRejectsInvalidID(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2HandlerWithStore(fakeDB2Store{})}, "query", "rows", "SpellName", "--ids", "nope")
	if err != nil {
		t.Fatalf("db2 rows returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) || !strings.Contains(stdout, `"invalid_argument"`) {
		t.Fatalf("db2 rows should reject invalid IDs:\n%s", stdout)
	}
}

func TestRuntimeDB2SchemaReturnsStoreSchema(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2HandlerWithStore(fakeDB2Store{})}, "query", "schema", "SpellName")
	if err != nil {
		t.Fatalf("db2 schema returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"rowCount": 2`) {
		t.Fatalf("db2 schema should include row count:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"Name_lang"`) {
		t.Fatalf("db2 schema should include field names:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"EffectMiscValue": "dbFieldInt32[2]"`) {
		t.Fatalf("db2 schema should include array lengths in Node-compatible field descriptions:\n%s", stdout)
	}
}

func TestRuntimeDB2SearchUsesStableRowsShape(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2HandlerWithStore(fakeDB2Store{})}, "query", "search", "SpellName", "--field", "Name_lang", "--query", "Fire", "--limit", "3")
	if err != nil {
		t.Fatalf("db2 search returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"mode": "search"`) || !strings.Contains(stdout, `"rows": [`) {
		t.Fatalf("db2 search should use stable rows payload:\n%s", stdout)
	}
	if strings.Contains(stdout, `"results"`) || strings.Contains(stdout, `"field"`) || strings.Contains(stdout, `"query"`) {
		t.Fatalf("db2 search should not emit Go-only payload keys:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"limit": 3`) {
		t.Fatalf("db2 search should pass --limit to store:\n%s", stdout)
	}
}

func TestRuntimeDB2RowsIncludesMode(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2HandlerWithStore(fakeDB2Store{})}, "query", "rows", "SpellName", "--id", "123")
	if err != nil {
		t.Fatalf("db2 rows returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"mode": "rows"`) {
		t.Fatalf("db2 rows should include stable mode:\n%s", stdout)
	}
}

func TestRuntimeDB2ForeignKeyUsesStableRowsShape(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2HandlerWithStore(fakeDB2Store{})}, "query", "foreign-key", "SpellEffect", "--field", "SpellID", "--value", "1")
	if err != nil {
		t.Fatalf("db2 foreign-key returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"mode": "foreign-key"`) || !strings.Contains(stdout, `"rows": [`) {
		t.Fatalf("db2 foreign-key should use stable rows payload:\n%s", stdout)
	}
	if strings.Contains(stdout, `"field"`) || strings.Contains(stdout, `"value"`) {
		t.Fatalf("db2 foreign-key should not emit Go-only payload keys:\n%s", stdout)
	}
}

func TestRuntimeDB2StreamSupportsJSONFormat(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2HandlerWithStore(fakeDB2Store{})}, "query", "stream", "SpellEffect", "--limit", "1", "--format", "json")
	if err != nil {
		t.Fatalf("db2 stream returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"command": "query stream"`) || !strings.Contains(stdout, `"mode": "stream"`) || !strings.Contains(stdout, `"rows": [`) {
		t.Fatalf("db2 stream --format json should return aggregate JSON payload:\n%s", stdout)
	}
}

func TestRuntimeDB2StreamRejectsUnknownFormat(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{DB2: NewDB2HandlerWithStore(fakeDB2Store{})}, "query", "stream", "SpellEffect", "--format", "yaml")
	if err != nil {
		t.Fatalf("db2 stream returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": false`) || !strings.Contains(stdout, `"invalid_argument"`) {
		t.Fatalf("db2 stream should reject unknown format:\n%s", stdout)
	}
}

type fakeFileStore struct{}

func (fakeFileStore) Lookup(fileDataID uint32) (string, bool) {
	if fileDataID == 1 {
		return "interface/icons/test.blp", true
	}
	return "", false
}

func (fakeFileStore) Search(query string, limit int) []appruntime.FileEntry {
	return []appruntime.FileEntry{{FileDataID: uint32(limit), Filename: "interface/icons/test.blp"}}
}

func (fakeFileStore) SearchCount(query string) int { return 7 }

func (fakeFileStore) Extension(extension string, limit int) []appruntime.FileEntry {
	return []appruntime.FileEntry{{FileDataID: uint32(limit), Filename: "interface/icons/test.blp"}}
}

func (fakeFileStore) ExtensionCount(extension string) int { return 8 }

func (fakeFileStore) ExistsByID(fileDataID uint32) bool { return fileDataID == 1 }

func (fakeFileStore) ExistsByName(filename string) bool {
	return filename == "interface/icons/test.blp"
}

func (fakeFileStore) EncodingInfo(fileDataID uint32) (interface{}, error) {
	return map[string]interface{}{"fileDataID": fileDataID, "encodingKey": "abcd"}, nil
}

func (fakeFileStore) ReadByID(fileDataID uint32) ([]byte, error) { return []byte("hello wow"), nil }

func (fakeFileStore) ReadByName(filename string) ([]byte, error) { return []byte("hello wow"), nil }

func TestRuntimeFileSearchPassesLimit(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{File: NewFileHandlerWithStore(fakeFileStore{})}, "file", "search", "--query", "icons", "--limit", "3")
	if err != nil {
		t.Fatalf("file search returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"fileDataID": 3`) {
		t.Fatalf("file search should pass --limit to store:\n%s", stdout)
	}
}

func TestRuntimeFileExtensionPassesLimit(t *testing.T) {
	stdout, stderr, err := executeCommandWithService(t, &Service{File: NewFileHandlerWithStore(fakeFileStore{})}, "file", "extension", "--extension", "blp", "--limit", "4")
	if err != nil {
		t.Fatalf("file extension returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `interface/icons/test.blp [4]`) {
		t.Fatalf("file extension should pass --limit to store:\n%s", stdout)
	}
}

func TestRuntimeFileExportWritesStoreBytes(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "file.bin")
	stdout, stderr, err := executeCommandWithService(t, &Service{File: NewFileHandlerWithStore(fakeFileStore{})}, "file", "export", "--file-data-id", "1", "--output", outPath)
	if err != nil {
		t.Fatalf("file export returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("file export should succeed with runtime store:\n%s", stdout)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected exported file: %v", err)
	}
	if string(data) != "hello wow" {
		t.Fatalf("exported data = %q", data)
	}
}

func TestRuntimeFileExportReadsByFilename(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "file.bin")
	stdout, stderr, err := executeCommandWithService(t, &Service{File: NewFileHandlerWithStore(fakeFileStore{})}, "file", "export", "--filename", "interface/icons/test.blp", "--output", outPath)
	if err != nil {
		t.Fatalf("file export by filename returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("file export by filename should succeed with runtime store:\n%s", stdout)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected exported file: %v", err)
	}
	if string(data) != "hello wow" {
		t.Fatalf("exported data = %q", data)
	}
}

type fakeIconStore struct{}

func (fakeIconStore) ReadByID(fileDataID uint32) ([]byte, error) {
	return buildRuntimeTestBLP(2, 2), nil
}

func TestRuntimeIconExportWritesPNG(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "icon.png")
	stdout, stderr, err := executeCommandWithService(t, &Service{Icon: NewIconHandlerWithStore(fakeIconStore{})}, "icon", "export", "--file-data-id", "1", "--output", outPath)
	if err != nil {
		t.Fatalf("icon export returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("icon export should succeed with runtime store:\n%s", stdout)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected exported icon: %v", err)
	}
}

func TestRuntimeIconExportWritesWebP(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "icon.webp")
	stdout, stderr, err := executeCommandWithService(t, &Service{Icon: NewIconHandlerWithStore(fakeIconStore{})}, "icon", "export", "--file-data-id", "1", "--format", "webp", "--mask", "15", "--output", outPath)
	if err != nil {
		t.Fatalf("icon export returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("icon export webp should succeed with runtime store:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"format": "webp"`) || strings.Contains(stdout, `"quality"`) {
		t.Fatalf("icon export webp should report format without quality:\n%s", stdout)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected exported icon: %v", err)
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		t.Fatalf("expected webp RIFF output, got %x", data[:min(len(data), 12)])
	}
}

func TestRuntimeItemGeosetsIncludesHelmetHideAtTopLevel(t *testing.T) {
	svc := wowdata.NewItemService()
	svc.AddItem(wowdata.ItemSummary{ID: 25})
	svc.SetItemGeoset(25, wowdata.ItemGeosetResult{
		ItemID:          25,
		GeosetGroup:     []int{0, 0, 0, 0, 0, 0},
		HelmetGeosetVis: []int{0, 0},
		HelmetHide:      []int{301, 401},
	})

	stdout, stderr, err := executeCommandWithService(t, &Service{Item: NewItemHandler(svc)}, "item", "geosets", "--item-id", "25")
	if err != nil {
		t.Fatalf("item geosets returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"geosets": {`) || !strings.Contains(stdout, `"helmetHide": [`) {
		t.Fatalf("item geosets should include stable top-level helmetHide:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"helmetGeosetVis": [`) {
		t.Fatalf("item geosets should include geoset payload:\n%s", stdout)
	}
}

func TestRuntimeItemTexturesUsesStableArrayShape(t *testing.T) {
	svc := wowdata.NewItemService()
	svc.AddItem(wowdata.ItemSummary{ID: 7517})
	svc.SetItemTextures(7517, wowdata.ItemTextureResult{
		ItemID: 7517,
		Sections: []wowdata.TextureSection{
			{Section: 0, FileDataID: 151296},
		},
	})

	stdout, stderr, err := executeCommandWithService(t, &Service{Item: NewItemHandler(svc)}, "item", "textures", "--item-id", "7517")
	if err != nil {
		t.Fatalf("item textures returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"textures": [`) || !strings.Contains(stdout, `"fileDataID": 151296`) {
		t.Fatalf("item textures should emit stable textures array:\n%s", stdout)
	}
	if strings.Contains(stdout, `"sections"`) {
		t.Fatalf("item textures should not emit Go-only sections wrapper:\n%s", stdout)
	}
}

func TestRuntimeCreatureModelUsesStableDisplayShape(t *testing.T) {
	svc := wowdata.NewCreatureService()
	svc.AddDisplay(5000, wowdata.CreatureDisplayInfo{
		DisplayID:       100,
		ModelFileDataID: 5000,
		Textures:        []uint32{6000},
	})
	svc.AddDisplay(5000, wowdata.CreatureDisplayInfo{
		DisplayID:       101,
		ModelFileDataID: 5000,
	})

	stdout, stderr, err := executeCommandWithService(t, &Service{Creature: NewCreatureHandler(svc)}, "creature", "model", "--file-data-id", "5000")
	if err != nil {
		t.Fatalf("creature model returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"ID": 100`) || !strings.Contains(stdout, `"modelID":`) || !strings.Contains(stdout, `"textures": [`) {
		t.Fatalf("creature model should emit stable display entries:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"ID": 101`) || strings.Contains(stdout, `"textures": null`) {
		t.Fatalf("creature model should emit empty texture arrays instead of null:\n%s", stdout)
	}
	if strings.Contains(stdout, `"displayID"`) || strings.Contains(stdout, `"modelFileDataID"`) {
		t.Fatalf("creature model should not emit Go-only display keys:\n%s", stdout)
	}
}

func TestRuntimeCreatureDisplayAcceptsFileDataID(t *testing.T) {
	svc := wowdata.NewCreatureService()
	svc.AddDisplay(5000, wowdata.CreatureDisplayInfo{
		DisplayID:       100,
		ModelFileDataID: 5000,
		Textures:        []uint32{6000},
	})

	stdout, stderr, err := executeCommandWithService(t, &Service{Creature: NewCreatureHandler(svc)}, "creature", "display", "--file-data-id", "5000")
	if err != nil {
		t.Fatalf("creature display by fileDataID returned command error: %v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"displayID": 100`) || !strings.Contains(stdout, `"modelFileDataID": 5000`) {
		t.Fatalf("creature display should resolve the first display for fileDataID:\n%s", stdout)
	}
}

func buildRuntimeTestBLP(width, height int) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(0x32504C42))
	buf.WriteByte(3)
	buf.WriteByte(0)
	buf.WriteByte(0)
	buf.WriteByte(1)
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint32(width))
	binary.Write(buf, binary.LittleEndian, uint32(height))
	binary.Write(buf, binary.LittleEndian, uint32(148+256*4))
	for i := 1; i < 16; i++ {
		binary.Write(buf, binary.LittleEndian, uint32(0))
	}
	binary.Write(buf, binary.LittleEndian, uint32(width*height*4))
	for i := 1; i < 16; i++ {
		binary.Write(buf, binary.LittleEndian, uint32(0))
	}
	for i := 0; i < 256; i++ {
		binary.Write(buf, binary.LittleEndian, uint32(0xFF0000FF))
	}
	for i := 0; i < width*height; i++ {
		buf.Write([]byte{0x20, 0x40, 0x80, 0xFF})
	}
	return buf.Bytes()
}
