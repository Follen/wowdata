//go:build wowdata_duckdb && cgo

package duckdb

import (
	"strings"
	"testing"
)

func TestTaggedDuckDBSelectByIDBuildsParameterizedSQL(t *testing.T) {
	sql, args, err := SelectByID("ItemSparse", "ID", 19019)
	if err != nil {
		t.Fatalf("SelectByID: %v", err)
	}
	if sql != `SELECT * FROM "ItemSparse" WHERE "ID" = ?` {
		t.Fatalf("SQL = %q, want quoted parameterized select", sql)
	}
	if strings.Contains(sql, "19019") {
		t.Fatalf("SQL = %q, should not inline id value", sql)
	}
	if len(args) != 1 || args[0] != uint32(19019) {
		t.Fatalf("args = %#v, want uint32 id", args)
	}
}
