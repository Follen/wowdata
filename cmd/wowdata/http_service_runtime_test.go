package main

import (
	"testing"

	appruntime "wowdata/internal/local/runtime"
)

func TestParquetFieldsPreservesRuntimeSchemaVocabularyAndArrayLen(t *testing.T) {
	got := parquetFields([]appruntime.SchemaField{
		{Name: "ID", Type: "dbFieldNonInlineID"},
		{Name: "Name_lang", Type: "dbFieldString"},
		{Name: "EffectMiscValue", Type: "dbFieldInt32", ArrayLen: 2},
	})

	if len(got) != 3 {
		t.Fatalf("parquetFields len = %d, want 3", len(got))
	}
	if got[0].Name != "ID" || got[0].Type != "dbFieldNonInlineID" || got[0].ArrayLen != 0 {
		t.Fatalf("ID field = %#v", got[0])
	}
	if got[1].Name != "Name_lang" || got[1].Type != "dbFieldString" || got[1].ArrayLen != 0 {
		t.Fatalf("Name_lang field = %#v", got[1])
	}
	if got[2].Name != "EffectMiscValue" || got[2].Type != "dbFieldInt32" || got[2].ArrayLen != 2 {
		t.Fatalf("EffectMiscValue field = %#v", got[2])
	}
}
