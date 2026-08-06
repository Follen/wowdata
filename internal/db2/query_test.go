package db2

import (
	"context"
	"testing"
)

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

type wrongRelationshipReader struct{ queryTestReader }

func (r wrongRelationshipReader) GetRelationshipRowsBatch([]uint32, []string) map[uint32][]map[string]interface{} {
	return map[uint32][]map[string]interface{}{}
}

type emptyRelationshipReader struct {
	queryTestReader
	allCalls int
}

func (r *emptyRelationshipReader) GetAllRows() map[uint32]map[string]interface{} {
	r.allCalls++
	return r.rows
}

func (r *emptyRelationshipReader) GetRelationshipRowsBatch(values []uint32, _ []string) map[uint32][]map[string]interface{} {
	return map[uint32][]map[string]interface{}{values[0]: {}}
}

func (r *emptyRelationshipReader) GetRelationshipRowsBatchContext(ctx context.Context, values []uint32, fields []string) (map[uint32][]map[string]interface{}, error) {
	return r.GetRelationshipRowsBatch(values, fields), ctx.Err()
}

func TestForeignRowsKeepsKnownEmptyRelationshipOnIndexPath(t *testing.T) {
	reader := &emptyRelationshipReader{queryTestReader: queryTestReader{rows: map[uint32]map[string]interface{}{
		1: {"ID": uint32(1), "ParentID": uint32(9)},
	}}}
	rows, relationship := GetForeignRows(reader, "T", "ParentID", 7)
	if !relationship || len(rows) != 0 || reader.allCalls != 0 {
		t.Fatalf("rows=%#v relationship=%v allCalls=%d", rows, relationship, reader.allCalls)
	}

	engine := NewEngine()
	if err := engine.Register("T", nil, reader); err != nil {
		t.Fatal(err)
	}
	snapshot := engine.Snapshot()
	defer snapshot.Close()
	result, stats, err := snapshot.Execute(context.Background(), QueryPlan{Table: "T", Mode: PlanForeignKey, ForeignField: "ParentID", ForeignValue: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 0 || stats.Physical != "relationship" || reader.allCalls != 0 {
		t.Fatalf("result=%#v stats=%+v allCalls=%d", result.Rows, stats, reader.allCalls)
	}
}

func TestForeignRowsFallsBackWhenNativeRelationshipDoesNotMatchField(t *testing.T) {
	reader := wrongRelationshipReader{queryTestReader{rows: map[uint32]map[string]interface{}{
		1: {"ID": uint32(1), "JournalInstanceID": uint16(7)},
	}}}
	rows, relationship := GetForeignRows(reader, "JournalEncounter", "JournalInstanceID", 7)
	if relationship || len(rows) != 1 {
		t.Fatalf("rows=%#v relationship=%v", rows, relationship)
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
