package db2

import "testing"

type queryTestReader struct {
	rows map[uint32]map[string]interface{}
}

func (r queryTestReader) GetRow(recordID uint32) map[string]interface{} {
	return r.rows[recordID]
}

func (r queryTestReader) GetAllRows() map[uint32]map[string]interface{} {
	return r.rows
}

func TestGetRowsWithIDsOnlyReturnsRequestedIDs(t *testing.T) {
	reader := queryTestReader{rows: map[uint32]map[string]interface{}{
		1: {"ID": uint32(1), "Name": "one"},
		2: {"ID": uint32(2), "Name": "two"},
	}}

	rows := GetRows(reader, []uint32{2}, nil, nil, 0)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0]["ID"] != uint32(2) {
		t.Fatalf("ID = %v, want 2", rows[0]["ID"])
	}
}

func TestGetRowsAddsSyntheticIDFromRecordKey(t *testing.T) {
	reader := queryTestReader{rows: map[uint32]map[string]interface{}{
		10: {"Name": "ten"},
	}}

	rows := GetRows(reader, []uint32{10}, nil, nil, 0)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0]["ID"] != uint32(10) || rows[0]["Name"] != "ten" {
		t.Fatalf("row = %#v, want synthetic ID 10 and Name", rows[0])
	}
}

func TestGetRowsProjectedSyntheticID(t *testing.T) {
	reader := queryTestReader{rows: map[uint32]map[string]interface{}{
		10: {"Name": "ten"},
	}}

	rows := GetRows(reader, []uint32{10}, []string{"ID"}, nil, 0)
	if len(rows) != 1 || rows[0]["ID"] != uint32(10) {
		t.Fatalf("rows = %#v, want projected synthetic ID", rows)
	}
	if _, ok := rows[0]["Name"]; ok {
		t.Fatalf("projected row should not include Name: %#v", rows[0])
	}
}

func TestGetRowsWithoutIDsReturnsDeterministicOrder(t *testing.T) {
	reader := queryTestReader{rows: map[uint32]map[string]interface{}{
		3: {"ID": uint32(3), "Name": "three"},
		1: {"ID": uint32(1), "Name": "one"},
		2: {"ID": uint32(2), "Name": "two"},
	}}

	rows := GetRows(reader, nil, nil, nil, 0)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	for i, want := range []uint32{1, 2, 3} {
		if rows[i]["ID"] != want {
			t.Fatalf("row %d ID = %v, want %d; rows=%#v", i, rows[i]["ID"], want, rows)
		}
	}
}

func TestSchemaWithSyntheticIDPrependsIDWhenMissing(t *testing.T) {
	got := SchemaWithSyntheticID([]SchemaField{{Name: "Name", Type: FieldString}})
	if len(got) != 2 || got[0].Name != "ID" || got[0].Type != FieldUInt32 || got[1].Name != "Name" {
		t.Fatalf("schema = %#v, want synthetic ID then Name", got)
	}
}

func TestSchemaWithSyntheticIDDoesNotDuplicateID(t *testing.T) {
	schema := []SchemaField{{Name: "ID", Type: FieldNonInlineID}, {Name: "Name", Type: FieldString}}
	got := SchemaWithSyntheticID(schema)
	if len(got) != 2 || got[0].Type != FieldNonInlineID {
		t.Fatalf("schema = %#v, want original ID preserved", got)
	}
}

func TestSearchRowsReturnsDeterministicOrderAndAppliesLimit(t *testing.T) {
	reader := queryTestReader{rows: map[uint32]map[string]interface{}{
		30: {"ID": uint32(30), "Name": "Attack C"},
		10: {"ID": uint32(10), "Name": "Attack A"},
		20: {"ID": uint32(20), "Name": "Attack B"},
	}}

	rows := SearchRows(reader, "Name", "attack", false, 2)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2: %#v", len(rows), rows)
	}
	for i, want := range []uint32{10, 20} {
		if rows[i]["ID"] != want {
			t.Fatalf("row %d ID = %v, want %d; rows=%#v", i, rows[i]["ID"], want, rows)
		}
	}
}
