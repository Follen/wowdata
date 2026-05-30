package db2

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func buildMinimalWDC2() []byte {
	buf := new(bytes.Buffer)

	// WDC header
	binary.Write(buf, binary.LittleEndian, uint32(0x32434457)) // WDC2 magic
	binary.Write(buf, binary.LittleEndian, uint32(2))           // recordCount
	binary.Write(buf, binary.LittleEndian, uint32(2))           // fieldCount (unused)
	binary.Write(buf, binary.LittleEndian, uint32(8))           // recordSize
	binary.Write(buf, binary.LittleEndian, uint32(0))           // stringTableSize
	binary.Write(buf, binary.LittleEndian, uint32(0))           // tableHash
	binary.Write(buf, binary.LittleEndian, uint32(0x11223344))  // layoutHash
	binary.Write(buf, binary.LittleEndian, uint32(1))           // minID
	binary.Write(buf, binary.LittleEndian, uint32(2))           // maxID
	binary.Write(buf, binary.LittleEndian, uint32(0))           // locale
	binary.Write(buf, binary.LittleEndian, uint16(0))           // flags (normal)
	binary.Write(buf, binary.LittleEndian, uint16(0))           // idFieldIndex
	binary.Write(buf, binary.LittleEndian, uint32(2))           // totalFieldCount
	binary.Write(buf, binary.LittleEndian, uint32(0))           // bitpackedDataOffset
	binary.Write(buf, binary.LittleEndian, uint32(0))           // lookupColumnCount
	binary.Write(buf, binary.LittleEndian, uint32(48))          // fieldStorageInfoSize (2 entries × 24)
	binary.Write(buf, binary.LittleEndian, uint32(0))           // commonDataSize
	binary.Write(buf, binary.LittleEndian, uint32(0))           // palletDataSize
	binary.Write(buf, binary.LittleEndian, uint32(1))           // sectionCount

	// Section header (WDC2: 40 bytes)
	binary.Write(buf, binary.LittleEndian, uint64(0)) // tactKeyHash
	binary.Write(buf, binary.LittleEndian, uint32(0)) // fileOffset
	binary.Write(buf, binary.LittleEndian, uint32(2)) // recordCount
	binary.Write(buf, binary.LittleEndian, uint32(0)) // stringTableSize
	binary.Write(buf, binary.LittleEndian, uint32(0)) // copyTableSize
	binary.Write(buf, binary.LittleEndian, uint32(0)) // offsetMapOffset
	binary.Write(buf, binary.LittleEndian, uint32(8)) // idListSize
	binary.Write(buf, binary.LittleEndian, uint32(0)) // relationshipDataSize

	// Fields array: 2 entries { size: int16, position: uint16 }
	binary.Write(buf, binary.LittleEndian, int16(4))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, int16(4))
	binary.Write(buf, binary.LittleEndian, uint16(4))

	// Field storage info: 2 entries (24 bytes each)
	// Entry 0: ID at byte offset 0, 32 bits
	binary.Write(buf, binary.LittleEndian, uint16(0))   // fieldOffsetBits=0
	binary.Write(buf, binary.LittleEndian, uint16(32))  // fieldSizeBits=32
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	// Entry 1: Value at byte offset 4, 32 bits
	binary.Write(buf, binary.LittleEndian, uint16(32))  // fieldOffsetBits=32
	binary.Write(buf, binary.LittleEndian, uint16(32))  // fieldSizeBits=32
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))

	// Section data: 2 records × 8 bytes = 16 bytes
	// Record 0: ID=1, Value=100
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint32(100))
	// Record 1: ID=2, Value=200
	binary.Write(buf, binary.LittleEndian, uint32(2))
	binary.Write(buf, binary.LittleEndian, uint32(200))

	// ID list: uint32[2]
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint32(2))

	return buf.Bytes()
}

func TestWDCHeaderParsing(t *testing.T) {
	data := buildMinimalWDC2()
	reader := &WDCReader{
		FileName: "TestTable",
		Schema: []SchemaField{
			{Name: "ID", Type: FieldUInt32},
			{Name: "Value", Type: FieldUInt32},
		},
		IsLoaded:   true,
		IDField:    "ID",
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
		IsLoaded:   true,
		IDField:    "ID",
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
		IsLoaded:   true,
		IDField:    "ID",
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
		IsLoaded:   true,
		IDField:    "ID",
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

func TestWDCCopyTable(t *testing.T) {
	reader := &WDCReader{
		FileName: "TestTable",
		Schema: []SchemaField{
			{Name: "ID", Type: FieldUInt32},
			{Name: "Value", Type: FieldUInt32},
		},
		IsLoaded:   true,
		IDField:    "ID",
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
