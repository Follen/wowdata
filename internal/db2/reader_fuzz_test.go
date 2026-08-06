package db2

import "testing"

func FuzzWDCReaderCorruptInput(f *testing.F) {
	f.Add(buildMinimalWDC2())
	f.Add(buildWDC5EmptyTableAtEOF())
	f.Add([]byte("WDC5"))
	schema := []SchemaField{{Name: "ID", Type: FieldUInt32}, {Name: "Name", Type: FieldString}}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		reader, err := NewWDCReaderFromBytes("Fuzz", data, schema)
		if err != nil {
			return
		}
		_ = reader.GetRow(0)
		_ = reader.GetAllRows()
		_ = reader.Scan([]string{"ID"}, nil, 2)
		_ = reader.GetRelationshipRowsBatch([]uint32{0, 1}, []string{"ID"})
	})
}

func FuzzDBCReaderCorruptInput(f *testing.F) {
	f.Add([]byte("WDBC"))
	f.Add([]byte{
		'W', 'D', 'B', 'C',
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	})
	schema := []SchemaField{{Name: "ID", Type: FieldUInt32}, {Name: "Name", Type: FieldString}}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		reader, err := NewDBCReaderFromBytes("Fuzz", "0.0.0.0", data, schema)
		if err != nil {
			return
		}
		_ = reader.GetRow(0)
		_ = reader.GetAllRows()
		_ = reader.Scan([]string{"ID"}, nil, 2)
	})
}
