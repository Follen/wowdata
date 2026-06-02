package db2

import "testing"

func TestNewWDCReaderFromBytes(t *testing.T) {
	data := buildMinimalWDC2()
	reader, err := NewWDCReaderFromBytes("TestTable", data, []SchemaField{
		{Name: "ID", Type: FieldUInt32},
		{Name: "Value", Type: FieldUInt32},
	})
	if err != nil {
		t.Fatalf("NewWDCReaderFromBytes: %v", err)
	}

	row := reader.GetRow(1)
	if row == nil || row["Value"] != uint32(100) {
		t.Fatalf("unexpected row: %#v", row)
	}
}
