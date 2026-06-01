package db2

import (
	"strings"
	"testing"

	appruntime "wowdata/internal/runtime"
)

type fakeStore struct {
	rowsCalled   bool
	schemaCalled bool
}

func (s *fakeStore) Schema(table string) ([]appruntime.SchemaField, int, error) {
	s.schemaCalled = true
	return []appruntime.SchemaField{{Name: "ID", Type: "uint32"}}, 1, nil
}

func (s *fakeStore) Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	s.rowsCalled = true
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
	rows, err := NewService(store).Rows(Query{Table: "SpellName", IDs: []uint32{1}, Limit: 1})
	if err != nil {
		t.Fatalf("Rows returned error: %v", err)
	}
	if !store.rowsCalled {
		t.Fatal("Rows did not call store")
	}
	if len(rows) != 1 || rows[0]["ID"] != uint32(1) {
		t.Fatalf("Rows = %#v, want ID 1", rows)
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

func TestSchemaForwardsStoreSchema(t *testing.T) {
	store := &fakeStore{}
	fields, rowCount, err := NewService(store).Schema("SpellName")
	if err != nil {
		t.Fatalf("Schema returned error: %v", err)
	}
	if !store.schemaCalled {
		t.Fatal("Schema did not call store")
	}
	if rowCount != 1 || len(fields) != 1 || fields[0].Name != "ID" {
		t.Fatalf("Schema = %#v, %d; want ID field and row count 1", fields, rowCount)
	}
}
