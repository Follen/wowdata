package duckdb

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestIsSafeIdentifier(t *testing.T) {
	for _, value := range []string{"SpellName", "Item_Sparse", "Field123"} {
		if !IsSafeIdentifier(value) {
			t.Fatalf("IsSafeIdentifier(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "123Field", "SpellName;DROP", "ID OR 1=1", "Name-Value", "schema.table"} {
		if IsSafeIdentifier(value) {
			t.Fatalf("IsSafeIdentifier(%q) = true, want false", value)
		}
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

func TestSelectByIDUsesParameterMarker(t *testing.T) {
	sql, args, err := SelectByID("SpellName", "ID", 42)
	if err != nil {
		t.Fatalf("SelectByID: %v", err)
	}
	if !strings.Contains(sql, "?") {
		t.Fatalf("SQL = %q, want parameter marker", sql)
	}
	if strings.Contains(sql, "42") {
		t.Fatalf("SQL = %q, should not inline id value", sql)
	}
	if len(args) != 1 || args[0] != uint32(42) {
		t.Fatalf("args = %#v, want uint32 id", args)
	}
}

func TestNoCGOEngineReportsUnavailable(t *testing.T) {
	engine := NewEngine(":memory:")
	if engine.Available() {
		t.Skip("DuckDB is available in this build")
	}
	if err := engine.Open(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Open error = %v, want ErrUnavailable", err)
	}
	if _, err := engine.QueryParquet(context.Background(), "SELECT 1", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("QueryParquet error = %v, want ErrUnavailable", err)
	}
}
