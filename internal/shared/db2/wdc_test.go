package db2

import (
	"encoding/binary"
	"testing"
)

func buildMinimalWDC2() []byte {
	return BuildMinimalWDC2ForTest()
}

func TestWDCHeaderParsing(t *testing.T) {
	data := buildMinimalWDC2()
	reader := &WDCReader{
		FileName: "TestTable",
		Schema: []SchemaField{
			{Name: "ID", Type: FieldUInt32},
			{Name: "Value", Type: FieldUInt32},
		},
		IsLoaded:     true,
		IDField:      "ID",
		IDFieldIndex: 0,
	}

	err := reader.parseBinary(data)
	if err != nil {
		t.Fatalf("parseBinary: %v", err)
	}

	if reader.RecordCount != 2 {
		t.Fatalf("recordCount = %d, want 2", reader.RecordCount)
	}
	if reader.RecordSize != 8 {
		t.Fatalf("recordSize = %d, want 12", reader.RecordSize)
	}
	if reader.MinID != 1 {
		t.Fatalf("minID = %d, want 1", reader.MinID)
	}
	if reader.MaxID != 2 {
		t.Fatalf("maxID = %d, want 2", reader.MaxID)
	}
	if reader.WDCVersion != 2 {
		t.Fatalf("wdcVersion = %d, want 2", reader.WDCVersion)
	}
	if len(reader.Sections) != 1 {
		t.Fatalf("section count = %d, want 1", len(reader.Sections))
	}
}

func TestWDCGetRow(t *testing.T) {
	data := buildMinimalWDC2()
	reader := &WDCReader{
		FileName: "TestTable",
		Schema: []SchemaField{
			{Name: "ID", Type: FieldUInt32},
			{Name: "Value", Type: FieldUInt32},
		},
		IsLoaded:     true,
		IDField:      "ID",
		IDFieldIndex: 0,
	}

	if err := reader.parseBinary(data); err != nil {
		t.Fatalf("parseBinary: %v", err)
	}

	row := reader.GetRow(1)
	if row == nil {
		t.Fatal("GetRow(1) returned nil")
	}
	if row["ID"] != uint32(1) {
		t.Fatalf("ID = %v", row["ID"])
	}
	if row["Value"] != uint32(100) {
		t.Fatalf("Value = %v", row["Value"])
	}

	row = reader.GetRow(2)
	if row == nil {
		t.Fatal("GetRow(2) returned nil")
	}
	if row["Value"] != uint32(200) {
		t.Fatalf("Value = %v", row["Value"])
	}
}

func TestWDCGetRowMissing(t *testing.T) {
	data := buildMinimalWDC2()
	reader := &WDCReader{
		FileName: "TestTable",
		Schema: []SchemaField{
			{Name: "ID", Type: FieldUInt32},
		},
		IsLoaded:     true,
		IDField:      "ID",
		IDFieldIndex: 0,
	}

	if err := reader.parseBinary(data); err != nil {
		t.Fatalf("parseBinary: %v", err)
	}

	if row := reader.GetRow(999); row != nil {
		t.Fatal("GetRow(999) should be nil")
	}
}

func TestWDCGetAllRows(t *testing.T) {
	data := buildMinimalWDC2()
	reader := &WDCReader{
		FileName: "TestTable",
		Schema: []SchemaField{
			{Name: "ID", Type: FieldUInt32},
			{Name: "Value", Type: FieldUInt32},
		},
		IsLoaded:     true,
		IDField:      "ID",
		IDFieldIndex: 0,
	}

	if err := reader.parseBinary(data); err != nil {
		t.Fatalf("parseBinary: %v", err)
	}

	rows := reader.GetAllRows()
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
}

func TestWDCGetAllRowsAcceptsSignedInlineID(t *testing.T) {
	reader := signedInlineIDReaderForTest()

	rows := reader.GetAllRows()
	if rows[7]["ID"] != int32(7) {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestWDCGetAllRowsPreservesTablesWithoutIDField(t *testing.T) {
	reader := &WDCReader{
		IsLoaded:         true,
		Schema:           []SchemaField{{Name: "FileDataID", Type: FieldUInt32}},
		IDField:          "ID",
		IDFieldIndex:     -1,
		FieldInfo:        []FieldStorageInfo{{FieldSizeBits: 32}},
		RecordSize:       4,
		TotalRecordCount: 2,
		data:             []byte{10, 0, 0, 0, 20, 0, 0, 0},
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 2},
			IsNormal:       true,
			RecordDataOfs:  0,
			RecordDataSize: 8,
		}},
	}

	rows := reader.GetAllRows()
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2: %#v", len(rows), rows)
	}
	if rows[0]["FileDataID"] != uint32(10) || rows[1]["FileDataID"] != uint32(20) {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestWDCEmptyIDListUsesRecordIndexForCommonData(t *testing.T) {
	reader := &WDCReader{
		IsLoaded: true,
		Schema: []SchemaField{
			{Name: "ID", Type: FieldUInt32},
			{Name: "Value", Type: FieldUInt32},
		},
		IDField:          "ID",
		IDFieldIndex:     0,
		FieldInfo:        []FieldStorageInfo{{FieldSizeBits: 32}, {FieldCompression: CompCommonData, FieldCompressionPacking: [3]uint32{10, 0, 0}}},
		CommonData:       []map[uint32]uint32{nil, {1: 99}},
		RecordSize:       4,
		TotalRecordCount: 2,
		data:             []byte{1, 0, 0, 0, 2, 0, 0, 0},
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 2},
			IsNormal:       true,
			IDList:         []uint32{0, 0},
			RecordDataOfs:  0,
			RecordDataSize: 8,
		}},
	}

	rows := reader.GetAllRows()
	if rows[0]["Value"] != uint32(10) {
		t.Fatalf("row 0 value = %v, want default 10; rows=%#v", rows[0]["Value"], rows)
	}
	if rows[1]["Value"] != uint32(99) {
		t.Fatalf("row 1 value = %v, want common-data override 99; rows=%#v", rows[1]["Value"], rows)
	}
}

func TestWDCEmptyIDListWritesRecordIndexForNonInlineID(t *testing.T) {
	reader := &WDCReader{
		IsLoaded: true,
		Schema: []SchemaField{
			{Name: "ID", Type: FieldNonInlineID},
			{Name: "Value", Type: FieldUInt32},
		},
		IDField:          "ID",
		IDFieldIndex:     0,
		FieldInfo:        []FieldStorageInfo{{FieldSizeBits: 32}},
		RecordSize:       4,
		TotalRecordCount: 2,
		data:             []byte{10, 0, 0, 0, 20, 0, 0, 0},
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 2},
			IsNormal:       true,
			IDList:         []uint32{0, 0},
			RecordDataOfs:  0,
			RecordDataSize: 8,
		}},
	}

	rows := reader.GetAllRows()
	if rows[0]["ID"] != uint32(0) || rows[1]["ID"] != uint32(1) {
		t.Fatalf("row IDs = %#v/%#v, want 0/1; rows=%#v", rows[0]["ID"], rows[1]["ID"], rows)
	}
}

func TestWDCSignedInlineIDKeysCommonData(t *testing.T) {
	reader := &WDCReader{
		IsLoaded: true,
		Schema: []SchemaField{
			{Name: "ID", Type: FieldInt32},
			{Name: "Value", Type: FieldUInt32},
		},
		IDField:          "ID",
		IDFieldIndex:     0,
		FieldInfo:        []FieldStorageInfo{{FieldCompression: CompBitpacked, FieldSizeBits: 32}, {FieldCompression: CompCommonData, FieldCompressionPacking: [3]uint32{10, 0, 0}}},
		CommonData:       []map[uint32]uint32{nil, {2: 99}},
		RecordSize:       4,
		TotalRecordCount: 2,
		data:             []byte{1, 0, 0, 0, 2, 0, 0, 0},
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 2},
			IsNormal:       true,
			RecordDataOfs:  0,
			RecordDataSize: 8,
		}},
	}

	rows := reader.GetAllRows()
	if rows[1]["Value"] != uint32(10) {
		t.Fatalf("row 1 value = %v, want default 10; rows=%#v", rows[1]["Value"], rows)
	}
	if rows[2]["Value"] != uint32(99) {
		t.Fatalf("row 2 value = %v, want common-data override 99; rows=%#v", rows[2]["Value"], rows)
	}
}

func TestWDCInlineIDNameKeysCommonDataWhenIDIndexDiffers(t *testing.T) {
	reader := &WDCReader{
		IsLoaded: true,
		Schema: []SchemaField{
			{Name: "ID", Type: FieldInt32},
			{Name: "Value", Type: FieldUInt32},
		},
		IDField: "ID",
		// Some WDC layouts report an idIndex that does not line up with the
		// schema field index used by this reader. The field name is still the
		// authoritative signal for inline ID capture.
		IDFieldIndex:     99,
		FieldInfo:        []FieldStorageInfo{{FieldCompression: CompBitpacked, FieldSizeBits: 32}, {FieldCompression: CompCommonData, FieldCompressionPacking: [3]uint32{10, 0, 0}}},
		CommonData:       []map[uint32]uint32{nil, {2: 99}},
		RecordSize:       4,
		TotalRecordCount: 2,
		data:             []byte{1, 0, 0, 0, 2, 0, 0, 0},
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 2},
			IsNormal:       true,
			RecordDataOfs:  0,
			RecordDataSize: 8,
		}},
	}

	rows := reader.GetAllRows()
	if rows[2]["Value"] != uint32(99) {
		t.Fatalf("row 2 value = %v, want common-data override 99; rows=%#v", rows[2]["Value"], rows)
	}
}

func TestWDCInlineDBDIDFieldAddsIDAlias(t *testing.T) {
	reader := &WDCReader{
		IsLoaded: true,
		Schema: []SchemaField{
			{Name: "LoreTextID", Type: FieldUInt32, IsID: true},
			{Name: "Value", Type: FieldUInt32},
		},
		IDField:          "ID",
		IDFieldIndex:     99,
		FieldInfo:        []FieldStorageInfo{{}, {}},
		RecordSize:       8,
		TotalRecordCount: 2,
		data: []byte{
			10, 0, 0, 0, 100, 0, 0, 0,
			11, 0, 0, 0, 110, 0, 0, 0,
		},
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 2},
			IsNormal:       true,
			RecordDataOfs:  0,
			RecordDataSize: 16,
		}},
	}

	rows := reader.GetAllRows()
	if rows[10]["ID"] != uint32(10) || rows[10]["LoreTextID"] != uint32(10) || rows[10]["Value"] != uint32(100) {
		t.Fatalf("row 10 = %#v, want ID alias from LoreTextID", rows[10])
	}
	row := reader.GetRow(11)
	if row == nil || row["ID"] != uint32(11) || row["LoreTextID"] != uint32(11) {
		t.Fatalf("GetRow(11) = %#v, want ID alias from LoreTextID", row)
	}
}

func TestWDCCommonDataBeforeInlineIDUsesCapturedID(t *testing.T) {
	reader := &WDCReader{
		IsLoaded: true,
		Schema: []SchemaField{
			{Name: "Value", Type: FieldUInt32},
			{Name: "ID", Type: FieldUInt32},
		},
		IDField:          "ID",
		IDFieldIndex:     1,
		FieldInfo:        []FieldStorageInfo{{FieldCompression: CompCommonData, FieldCompressionPacking: [3]uint32{10, 0, 0}}, {FieldCompression: CompBitpacked, FieldSizeBits: 32}},
		CommonData:       []map[uint32]uint32{{2: 99}, nil},
		RecordSize:       4,
		TotalRecordCount: 2,
		data:             []byte{1, 0, 0, 0, 2, 0, 0, 0},
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 2},
			IsNormal:       true,
			RecordDataOfs:  0,
			RecordDataSize: 8,
		}},
	}

	rows := reader.GetAllRows()
	if rows[2]["Value"] != uint32(99) {
		t.Fatalf("row 2 value = %v, want common-data override 99; rows=%#v", rows[2]["Value"], rows)
	}
}

func TestWDCCommonDataWithoutInlineIDKeepsRecordIndexKey(t *testing.T) {
	reader := &WDCReader{
		IsLoaded: true,
		Schema: []SchemaField{
			{Name: "Value", Type: FieldUInt32},
		},
		IDField:          "ID",
		IDFieldIndex:     -1,
		FieldInfo:        []FieldStorageInfo{{FieldCompression: CompCommonData, FieldCompressionPacking: [3]uint32{10, 0, 0}}},
		CommonData:       []map[uint32]uint32{{1: 99}},
		RecordSize:       0,
		TotalRecordCount: 2,
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 2},
			IsNormal:       true,
			RecordDataOfs:  0,
			RecordDataSize: 0,
		}},
	}

	rows := reader.GetAllRows()
	if rows[1]["Value"] != uint32(99) {
		t.Fatalf("row 1 value = %v, want record-index common-data override 99; rows=%#v", rows[1]["Value"], rows)
	}
}

func TestWDCRelationUsesNonInlineRecordIDBeforeRecordIndex(t *testing.T) {
	reader := &WDCReader{
		IsLoaded: true,
		Flags:    2,
		Schema: []SchemaField{
			{Name: "ID", Type: FieldNonInlineID},
			{Name: "CollectableSourceInfoID", Type: FieldRelation},
		},
		IDField:          "ID",
		IDFieldIndex:     0,
		FieldInfo:        []FieldStorageInfo{},
		RecordSize:       0,
		TotalRecordCount: 1,
		Sections: []Section{{
			Header:          SectionHeader{RecordCount: 1},
			IsNormal:        true,
			IDList:          []uint32{2507},
			RelationshipMap: map[uint32]uint32{2507: 19612},
		}},
	}

	row := reader.GetRow(2507)
	if row["CollectableSourceInfoID"] != uint32(19612) {
		t.Fatalf("CollectableSourceInfoID = %v, want 19612; row=%#v", row["CollectableSourceInfoID"], row)
	}
}

func TestWDCRelationUsesRecordIndexWithoutSecondaryKey(t *testing.T) {
	reader := &WDCReader{
		IsLoaded: true,
		Schema: []SchemaField{
			{Name: "ID", Type: FieldNonInlineID},
			{Name: "ParentID", Type: FieldRelation},
		},
		IDField:          "ID",
		IDFieldIndex:     0,
		FieldInfo:        []FieldStorageInfo{},
		RecordSize:       0,
		TotalRecordCount: 1,
		Sections: []Section{{
			Header:          SectionHeader{RecordCount: 1},
			IsNormal:        true,
			IDList:          []uint32{2507},
			RelationshipMap: map[uint32]uint32{0: 19612, 2507: 99999},
		}},
	}

	row := reader.GetRow(2507)
	if row["ParentID"] != uint32(19612) {
		t.Fatalf("ParentID = %v, want record-index relation 19612; row=%#v", row["ParentID"], row)
	}
}

func TestWDCDuplicateFieldInfoUsesCurrentCommonDataIndex(t *testing.T) {
	reader := &WDCReader{
		IsLoaded: true,
		Schema: []SchemaField{
			{Name: "ID", Type: FieldUInt32},
			{Name: "FormID", Type: FieldUInt8},
			{Name: "DisplayID", Type: FieldUInt32},
		},
		IDField:      "ID",
		IDFieldIndex: 0,
		FieldInfo: []FieldStorageInfo{
			{FieldCompression: CompBitpacked, FieldSizeBits: 32},
			{FieldCompression: CompCommonData, AdditionalDataSize: 8},
			{FieldCompression: CompCommonData, AdditionalDataSize: 8},
		},
		CommonData: []map[uint32]uint32{
			nil,
			{1: 0x3f199901},
			{1: 66784},
		},
		RecordSize:       4,
		TotalRecordCount: 1,
		data:             []byte{1, 0, 0, 0},
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 1},
			IsNormal:       true,
			RecordDataOfs:  0,
			RecordDataSize: 4,
		}},
	}

	row := reader.GetRow(1)
	if row["FormID"] != uint8(1) {
		t.Fatalf("FormID = %v, want 1; row=%#v", row["FormID"], row)
	}
	if row["DisplayID"] != uint32(66784) {
		t.Fatalf("DisplayID = %v, want 66784; row=%#v", row["DisplayID"], row)
	}
}

func TestWDCStringArrayReadsAllInlineValues(t *testing.T) {
	reader := &WDCReader{
		IsLoaded:         true,
		Schema:           []SchemaField{{Name: "ParamLabel", Type: FieldString, ArrayLen: 3}},
		IDField:          "ID",
		IDFieldIndex:     -1,
		FieldInfo:        []FieldStorageInfo{{FieldSizeBits: 24}},
		RecordSize:       12,
		TotalRecordCount: 1,
		data:             []byte("Points\x00Accuracy\x00Variance\x00"),
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 1},
			IsNormal:       true,
			RecordDataOfs:  0,
			RecordDataSize: 25,
		}},
	}

	rows := reader.GetAllRows()
	values, ok := rows[0]["ParamLabel"].([]interface{})
	if !ok {
		t.Fatalf("ParamLabel = %#v, want string array", rows[0]["ParamLabel"])
	}
	want := []interface{}{"Points", "Accuracy", "Variance"}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("ParamLabel[%d] = %#v, want %#v; values=%#v", i, values[i], want[i], values)
		}
	}
}

func TestWDCStringArrayReadsAllStringTableOffsets(t *testing.T) {
	data := []byte{
		12, 0, 0, 0,
		15, 0, 0, 0,
		20, 0, 0, 0,
	}
	data = append(data, []byte("Points\x00Accuracy\x00Variance\x00")...)
	reader := &WDCReader{
		IsLoaded:         true,
		Schema:           []SchemaField{{Name: "ParamLabel", Type: FieldString, ArrayLen: 3}},
		IDField:          "ID",
		IDFieldIndex:     -1,
		FieldInfo:        []FieldStorageInfo{{FieldSizeBits: 96}},
		RecordSize:       12,
		RecordCount:      1,
		TotalRecordCount: 1,
		WDCVersion:       3,
		data:             data,
		Sections: []Section{{
			Header:            SectionHeader{RecordCount: 1, StringTableSize: uint32(len(data) - 12)},
			IsNormal:          true,
			RecordDataOfs:     0,
			RecordDataSize:    12,
			StringTableOffset: 12,
		}},
	}

	rows := reader.GetAllRows()
	values, ok := rows[0]["ParamLabel"].([]interface{})
	if !ok {
		t.Fatalf("ParamLabel = %#v, want string array", rows[0]["ParamLabel"])
	}
	want := []interface{}{"Points", "Accuracy", "Variance"}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("ParamLabel[%d] = %#v, want %#v; values=%#v", i, values[i], want[i], values)
		}
	}
}

func TestWDCGetRowAcceptsSignedInlineID(t *testing.T) {
	reader := signedInlineIDReaderForTest()

	row := reader.GetRow(7)
	if row == nil || row["ID"] != int32(7) {
		t.Fatalf("row = %#v", row)
	}
}

func signedInlineIDReaderForTest() *WDCReader {
	return &WDCReader{
		IsLoaded:         true,
		Schema:           []SchemaField{{Name: "ID", Type: FieldInt32}},
		IDField:          "ID",
		IDFieldIndex:     0,
		FieldInfo:        []FieldStorageInfo{{FieldSizeBits: 32}},
		RecordSize:       4,
		TotalRecordCount: 1,
		data:             []byte{7, 0, 0, 0},
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 1},
			IsNormal:       true,
			RecordDataOfs:  0,
			RecordDataSize: 4,
		}},
	}
}

func TestWDCCopyTable(t *testing.T) {
	reader := &WDCReader{
		FileName: "TestTable",
		Schema: []SchemaField{
			{Name: "ID", Type: FieldUInt32},
			{Name: "Value", Type: FieldUInt32},
		},
		IsLoaded:     true,
		IDField:      "ID",
		IDFieldIndex: 0,
	}

	data := buildMinimalWDC2()
	if err := reader.parseBinary(data); err != nil {
		t.Fatalf("parseBinary: %v", err)
	}

	// Verify parse succeeded - copy table may be empty for this fixture
	if reader.TotalRecordCount < 2 {
		t.Fatalf("totalRecordCount = %d", reader.TotalRecordCount)
	}
	// Add a copy table entry manually to test the lookup
	reader.CopyTable[99] = 1
	row := reader.GetRow(99)
	if row == nil {
		t.Fatal("GetRow(99) through copy table should work")
	}
	if row["ID"] != uint32(99) {
		t.Fatalf("copy table row ID = %v, want 99", row["ID"])
	}
	if row["Value"] != uint32(100) {
		t.Fatalf("copy table value = %v, want 100", row["Value"])
	}
}

func TestWDC5EmptyMultiSectionTableAtEOF(t *testing.T) {
	data := buildWDC5EmptyTableAtEOF()
	schema := []SchemaField{
		{Name: "ID", Type: FieldNonInlineID},
		{Name: "JournalEncounterID", Type: FieldUInt32},
	}
	reader, err := NewWDCReaderFromBytes("JournalEncounterSection", data, schema)
	if err != nil {
		t.Fatalf("empty WDC5 table should parse: %v", err)
	}
	if got := reader.GetAllRows(); len(got) != 0 {
		t.Fatalf("expected no rows, got %#v", got)
	}
}

func TestWDCRelationshipDataTruncationReturnsError(t *testing.T) {
	data := buildWDC5TruncatedRelationshipData()
	data = data[:len(data)-1]
	schema := []SchemaField{
		{Name: "ID", Type: FieldNonInlineID},
		{Name: "SpellID", Type: FieldUInt32},
	}
	if _, err := NewWDCReaderFromBytes("SpellEffect", data, schema); err == nil {
		t.Fatal("expected truncated relationship data error")
	}
}

func TestWDCRecordReadPastRecordDoesNotPanic(t *testing.T) {
	reader := &WDCReader{
		IsLoaded:         true,
		Schema:           []SchemaField{{Name: "ID", Type: FieldUInt32}, {Name: "Value", Type: FieldUInt32}},
		IDField:          "ID",
		IDFieldIndex:     0,
		FieldInfo:        []FieldStorageInfo{{FieldSizeBits: 32}, {FieldSizeBits: 32}},
		RecordSize:       4,
		TotalRecordCount: 1,
		data:             []byte{7, 0, 0, 0},
		Sections: []Section{{
			Header:         SectionHeader{RecordCount: 1},
			IsNormal:       true,
			RecordDataOfs:  0,
			RecordDataSize: 4,
		}},
	}
	if row := reader.GetRow(7); row != nil {
		t.Fatalf("expected nil row for schema beyond record, got %#v", row)
	}
}

func buildWDC5EmptyTableAtEOF() []byte {
	buf := make([]byte, 0, 264)
	put32 := func(v uint32) {
		tmp := make([]byte, 4)
		binary.LittleEndian.PutUint32(tmp, v)
		buf = append(buf, tmp...)
	}
	put16 := func(v uint16) {
		tmp := make([]byte, 2)
		binary.LittleEndian.PutUint16(tmp, v)
		buf = append(buf, tmp...)
	}
	put64 := func(v uint64) {
		tmp := make([]byte, 8)
		binary.LittleEndian.PutUint64(tmp, v)
		buf = append(buf, tmp...)
	}

	put32(wdc5Magic)
	put32(5)
	build := make([]byte, 128)
	copy(build, []byte("WOWSTATIC_1_15_8_63631"))
	buf = append(buf, build...)
	put32(0)  // recordCount
	put32(15) // fieldCount
	put32(8)  // recordSize
	put32(0)  // stringTableSize
	put32(0)  // tableHash
	put32(0)  // layoutHash
	put32(0)  // minID
	put32(0)  // maxID
	put32(1)  // locale
	put16(4)  // flags
	put16(15) // id field index
	put32(15) // total field count
	put32(0)  // bitpackedDataOffset
	put32(0)  // lookupColumnCount
	put32(0)  // fieldStorageInfoSize
	put32(0)  // commonDataSize
	put32(0)  // palletDataSize
	put32(4)  // sectionCount

	for i := 0; i < 4; i++ {
		put64(0)
		put32(0) // fileOffset
		put32(0) // recordCount
		put32(0) // stringTableSize
		put32(0) // offsetRecordsEnd
		put32(0) // idListSize
		put32(0) // relationshipDataSize
		put32(0) // offsetMapIDCount
		put32(0) // copyTableCount
	}
	for i := 0; i < 15; i++ {
		put16(32)
		put16(8)
	}
	return buf
}

func buildWDC5TruncatedRelationshipData() []byte {
	buf := make([]byte, 0, 256)
	put32 := func(v uint32) {
		tmp := make([]byte, 4)
		binary.LittleEndian.PutUint32(tmp, v)
		buf = append(buf, tmp...)
	}
	put16 := func(v uint16) {
		tmp := make([]byte, 2)
		binary.LittleEndian.PutUint16(tmp, v)
		buf = append(buf, tmp...)
	}
	put64 := func(v uint64) {
		tmp := make([]byte, 8)
		binary.LittleEndian.PutUint64(tmp, v)
		buf = append(buf, tmp...)
	}

	put32(wdc5Magic)
	put32(5)
	build := make([]byte, 128)
	copy(build, []byte("WOWSTATIC_12_0_5"))
	buf = append(buf, build...)
	put32(1) // recordCount
	put32(2) // fieldCount
	put32(8) // recordSize
	put32(0) // stringTableSize
	put32(0) // tableHash
	put32(0) // layoutHash
	put32(1) // minID
	put32(1) // maxID
	put32(1) // locale
	put16(0) // flags
	put16(0) // id field index
	put32(2) // total field count
	put32(0) // bitpackedDataOffset
	put32(0) // lookupColumnCount
	put32(0) // fieldStorageInfoSize
	put32(0) // commonDataSize
	put32(0) // palletDataSize
	put32(1) // sectionCount

	put64(0)
	put32(0)  // fileOffset
	put32(1)  // recordCount
	put32(0)  // stringTableSize
	put32(8)  // offsetRecordsEnd
	put32(0)  // idListSize
	put32(16) // relationshipDataSize says one entry is present
	put32(0)  // offsetMapIDCount
	put32(0)  // copyTableCount

	for i := 0; i < 2; i++ {
		put16(32)
		put16(uint16(i * 4))
	}

	put32(1)   // ID
	put32(123) // SpellID
	put32(1)   // relationship entry count
	put32(1)   // minID
	put32(1)   // maxID
	put32(123) // foreignID, but recordIndex is missing
	return buf
}
