package duckdb

import (
	"os"
	"path/filepath"
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

func TestEnsureDuckDBParentDirCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "wowdata.duckdb")
	if err := ensureDuckDBParentDir(path); err != nil {
		t.Fatalf("ensureDuckDBParentDir: %v", err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil || !info.IsDir() {
		t.Fatalf("parent directory was not created")
	}
}
