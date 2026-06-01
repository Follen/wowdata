package duckdb

import (
	"reflect"
	"strings"
	"testing"
)

func TestIsSafeIdentifier(t *testing.T) {
	safe := []string{"SpellName", "Item_Sparse1"}
	for _, value := range safe {
		if !IsSafeIdentifier(value) {
			t.Fatalf("IsSafeIdentifier(%q) = false, want true", value)
		}
	}

	unsafe := []string{"", "SpellName; DROP TABLE x", "Spell Name", "Item-Sparse", `"SpellName"`, "ID OR 1=1"}
	for _, value := range unsafe {
		if IsSafeIdentifier(value) {
			t.Fatalf("IsSafeIdentifier(%q) = true, want false", value)
		}
	}
}

func TestSelectByIDUsesParameterizedID(t *testing.T) {
	sql, args, err := SelectByID("SpellName", "ID", 1)
	if err != nil {
		t.Fatalf("SelectByID: %v", err)
	}

	if !strings.Contains(sql, `"SpellName"`) || !strings.Contains(sql, `"ID"`) {
		t.Fatalf("SelectByID sql = %q, want quoted identifiers", sql)
	}
	if !strings.Contains(sql, "?") {
		t.Fatalf("SelectByID sql = %q, want parameter marker", sql)
	}
	if strings.Contains(sql, " 1") {
		t.Fatalf("SelectByID sql = %q, id appears interpolated", sql)
	}
	wantArgs := []interface{}{uint32(1)}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("SelectByID args = %#v, want %#v", args, wantArgs)
	}
}

func TestSelectByIDRejectsIdentifierInjection(t *testing.T) {
	if _, _, err := SelectByID("SpellName; DROP TABLE x", "ID", 1); err == nil {
		t.Fatal("table injection accepted")
	}
	if _, _, err := SelectByID("SpellName", "ID OR 1=1", 1); err == nil {
		t.Fatal("field injection accepted")
	}
}
