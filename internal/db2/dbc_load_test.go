package db2

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestNewDBCReaderFromBytes(t *testing.T) {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(dbcMagic))
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint32(2))
	binary.Write(buf, binary.LittleEndian, uint32(8))
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint32(7))
	binary.Write(buf, binary.LittleEndian, uint32(42))
	buf.WriteByte(0)

	reader, err := NewDBCReaderFromBytes("Test", "1.0.0.1", buf.Bytes(), []SchemaField{
		{Name: "ID", Type: FieldUInt32},
		{Name: "Value", Type: FieldUInt32},
	})
	if err != nil {
		t.Fatalf("NewDBCReaderFromBytes: %v", err)
	}
	row := reader.GetRow(0)
	if row == nil || row["ID"] != uint32(7) || row["Value"] != uint32(42) {
		t.Fatalf("unexpected row: %#v", row)
	}
}

func TestDBCReaderSchemaPastRecordDoesNotPanic(t *testing.T) {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, uint32(dbcMagic))
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint32(4))
	binary.Write(buf, binary.LittleEndian, uint32(1))
	binary.Write(buf, binary.LittleEndian, uint32(7))
	buf.WriteByte(0)

	reader, err := NewDBCReaderFromBytes("Test", "1.0.0.1", buf.Bytes(), []SchemaField{
		{Name: "ID", Type: FieldUInt32},
		{Name: "Value", Type: FieldUInt32},
	})
	if err != nil {
		t.Fatalf("NewDBCReaderFromBytes: %v", err)
	}
	if row := reader.GetRow(0); row != nil {
		t.Fatalf("expected nil row for schema beyond record, got %#v", row)
	}
}
