package db2

import (
	"testing"

	"wowdata/internal/shared/dbd"
)

func TestSchemaFromDBDConvertsDBDFieldRules(t *testing.T) {
	entry := &dbd.DBDEntry{Fields: []dbd.DBDField{
		{Name: "ID", Type: "int", IsSigned: false, IsID: true, IsInline: false, Size: 32},
		{Name: "ParentID", Type: "int", IsSigned: false, IsRelation: true, IsInline: false, Size: 32},
		{Name: "Name_lang", Type: "locstring", IsInline: true},
		{Name: "Amount", Type: "int", IsSigned: true, IsInline: true, Size: 16},
		{Name: "Scale", Type: "float", IsInline: true, Size: 32},
	}}

	schema, err := SchemaFromDBD(entry)
	if err != nil {
		t.Fatalf("SchemaFromDBD: %v", err)
	}

	want := []SchemaField{
		{Name: "ID", Type: FieldNonInlineID, IsID: true},
		{Name: "ParentID", Type: FieldRelation},
		{Name: "Name_lang", Type: FieldString},
		{Name: "Amount", Type: FieldInt16},
		{Name: "Scale", Type: FieldFloat},
	}
	if len(schema) != len(want) {
		t.Fatalf("len(schema) = %d, want %d", len(schema), len(want))
	}
	for i := range want {
		if schema[i].Name != want[i].Name || schema[i].Type != want[i].Type {
			t.Fatalf("schema[%d] = %#v, want %#v", i, schema[i], want[i])
		}
	}
	if !schema[0].IsID {
		t.Fatalf("schema[0].IsID = false, want true")
	}
}

func TestSchemaFromDBDRejectsUnsupportedIntegerSize(t *testing.T) {
	_, err := SchemaFromDBD(&dbd.DBDEntry{Fields: []dbd.DBDField{
		{Name: "Bad", Type: "int", IsInline: true, Size: 24},
	}})
	if err == nil {
		t.Fatal("expected unsupported integer size error")
	}
}
