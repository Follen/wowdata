package db2

import (
	"reflect"
	"strings"
	"testing"

	appruntime "wowdata/internal/runtime"
)

type fakeStore struct {
	rowsCalled   bool
	schemaCalled bool
	rowsTable    string
	rowsIDs      []uint32
	rowsFields   []string
	rowsFilter   string
	rowsLimit    int
	schemaTable  string
}

func (s *fakeStore) Schema(table string) ([]appruntime.SchemaField, int, error) {
	s.schemaCalled = true
	s.schemaTable = table
	return []appruntime.SchemaField{{Name: "ID", Type: "uint32"}}, 1, nil
}

func (s *fakeStore) Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	s.rowsCalled = true
	s.rowsTable = table
	s.rowsIDs = ids
	s.rowsFields = fields
	s.rowsFilter = filter
	s.rowsLimit = limit
	return []map[string]interface{}{{"ID": uint32(1)}}, nil
}

func (s *fakeStore) Search(table string, field string, query string, limit int) ([]map[string]interface{}, error) {
	return nil, nil
}

func (s *fakeStore) ForeignKey(table string, field string, value uint32, limit int) ([]map[string]interface{}, error) {
	return nil, nil
}

func (s *fakeStore) Stream(table string, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	return nil, nil
}

func TestRowsCallsStoreAndReturnsRows(t *testing.T) {
	store := &fakeStore{}
	query := Query{
		Table:  "SpellName",
		IDs:    []uint32{1, 2},
		Fields: []string{"ID", "Name"},
		Filter: "ID > 0",
		Limit:  7,
	}
	rows, err := NewService(store).Rows(query)
	if err != nil {
		t.Fatalf("Rows returned error: %v", err)
	}
	if !store.rowsCalled {
		t.Fatal("Rows did not call store")
	}
	if len(rows) != 1 || rows[0]["ID"] != uint32(1) {
		t.Fatalf("Rows = %#v, want ID 1", rows)
	}
	if store.rowsTable != query.Table {
		t.Fatalf("Rows table = %q, want %q", store.rowsTable, query.Table)
	}
	if !reflect.DeepEqual(store.rowsIDs, query.IDs) {
		t.Fatalf("Rows ids = %#v, want %#v", store.rowsIDs, query.IDs)
	}
	if !reflect.DeepEqual(store.rowsFields, query.Fields) {
		t.Fatalf("Rows fields = %#v, want %#v", store.rowsFields, query.Fields)
	}
	if store.rowsFilter != query.Filter {
		t.Fatalf("Rows filter = %q, want %q", store.rowsFilter, query.Filter)
	}
	if store.rowsLimit != query.Limit {
		t.Fatalf("Rows limit = %d, want %d", store.rowsLimit, query.Limit)
	}
}

func TestRowsRejectsEmptyTable(t *testing.T) {
	_, err := NewService(&fakeStore{}).Rows(Query{})
	if err == nil {
		t.Fatal("Rows returned nil error")
	}
	if !strings.Contains(err.Error(), "table is required") {
		t.Fatalf("Rows error = %q, want table is required", err)
	}
}

func TestSchemaRejectsEmptyTable(t *testing.T) {
	_, _, err := NewService(&fakeStore{}).Schema("")
	if err == nil {
		t.Fatal("Schema returned nil error")
	}
	if !strings.Contains(err.Error(), "table is required") {
		t.Fatalf("Schema error = %q, want table is required", err)
	}
}

func TestServiceSchemaCallsStore(t *testing.T) {
	store := &fakeStore{}
	fields, rowCount, err := NewService(store).Schema("SpellName")
	if err != nil {
		t.Fatalf("Schema returned error: %v", err)
	}
	if !store.schemaCalled {
		t.Fatal("Schema did not call store")
	}
	if store.schemaTable != "SpellName" {
		t.Fatalf("Schema table = %q, want SpellName", store.schemaTable)
	}
	if rowCount != 1 || len(fields) != 1 || fields[0].Name != "ID" {
		t.Fatalf("Schema = %#v, %d; want ID field and row count 1", fields, rowCount)
	}
}
