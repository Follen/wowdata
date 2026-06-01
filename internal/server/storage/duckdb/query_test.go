package duckdb

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRowsSQLQuotesIdentifiersAndBindsValues(t *testing.T) {
	root := t.TempDir()
	table := tableRef(root, "Spell")
	query, args, err := NewQueryBuilder(root).BuildRowsSQL(table, RowsQuery{
		Columns: []string{"ID", "Name_lang"},
		Where:   map[string]interface{}{"Name_lang": `Malicious' OR 1=1 --`},
		Limit:   25,
		Offset:  50,
	})
	if err != nil {
		t.Fatalf("BuildRowsSQL: %v", err)
	}

	assertSQLContains(t, query, `SELECT "ID", "Name_lang" FROM read_parquet(?) AS "Spell"`)
	assertSQLContains(t, query, `WHERE "Name_lang" = ?`)
	assertSQLContains(t, query, `LIMIT ? OFFSET ?`)
	assertNotInterpolated(t, query, `Malicious' OR 1=1 --`)
	assertArgs(t, args, table.ParquetPath, `Malicious' OR 1=1 --`, 25, 50)
}

func TestBuildSearchSQLQuotesIdentifiersAndBindsPattern(t *testing.T) {
	root := t.TempDir()
	table := tableRef(root, "SpellName")
	query, args, err := NewQueryBuilder(root).BuildSearchSQL(table, SearchQuery{
		Columns:      []string{"ID", "Name_lang"},
		SearchColumn: "Name_lang",
		Pattern:      `%foo%' UNION SELECT secret --`,
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("BuildSearchSQL: %v", err)
	}

	assertSQLContains(t, query, `SELECT "ID", "Name_lang" FROM read_parquet(?) AS "SpellName"`)
	assertSQLContains(t, query, `WHERE "Name_lang" ILIKE ?`)
	assertSQLContains(t, query, `LIMIT ?`)
	assertNotInterpolated(t, query, `%foo%' UNION SELECT secret --`)
	assertArgs(t, args, table.ParquetPath, `%foo%' UNION SELECT secret --`, 10)
}

func TestBuildForeignKeySQLQuotesIdentifiersAndBindsID(t *testing.T) {
	root := t.TempDir()
	table := tableRef(root, "SpellEffect")
	query, args, err := NewQueryBuilder(root).BuildForeignKeySQL(table, ForeignKeyQuery{
		Columns: []string{"ID", "SpellID", "EffectTriggerSpell"},
		Field:   "SpellID",
		Value:   uint32(1337),
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("BuildForeignKeySQL: %v", err)
	}

	assertSQLContains(t, query, `SELECT "ID", "SpellID", "EffectTriggerSpell" FROM read_parquet(?) AS "SpellEffect"`)
	assertSQLContains(t, query, `WHERE "SpellID" = ?`)
	assertSQLContains(t, query, `LIMIT ?`)
	assertArgs(t, args, table.ParquetPath, uint32(1337), 100)
}

func TestBuildStreamSQLUsesValidatedTableAndBoundedArgs(t *testing.T) {
	root := t.TempDir()
	table := tableRef(root, "ItemSparse")
	query, args, err := NewQueryBuilder(root).BuildStreamSQL(table, StreamQuery{
		Columns: []string{"ID", "Display_lang"},
		Limit:   500,
		Offset:  1000,
	})
	if err != nil {
		t.Fatalf("BuildStreamSQL: %v", err)
	}

	assertSQLContains(t, query, `SELECT "ID", "Display_lang" FROM read_parquet(?) AS "ItemSparse"`)
	assertSQLContains(t, query, `LIMIT ? OFFSET ?`)
	assertArgs(t, args, table.ParquetPath, 500, 1000)
}

func TestBuildSchemaSQLReadsParquetSchemaWithoutUserValues(t *testing.T) {
	root := t.TempDir()
	table := tableRef(root, "Spell")
	query, args, err := NewQueryBuilder(root).BuildSchemaSQL(table)
	if err != nil {
		t.Fatalf("BuildSchemaSQL: %v", err)
	}

	assertSQLContains(t, query, `DESCRIBE SELECT * FROM read_parquet(?) AS "Spell"`)
	assertArgs(t, args, table.ParquetPath)
}

func TestQueryBuilderRejectsInvalidIdentifiers(t *testing.T) {
	root := t.TempDir()
	builder := NewQueryBuilder(root)
	cases := []struct {
		name string
		ref  TableRef
		run  func(TableRef) error
	}{
		{
			name: "table",
			ref:  tableRef(root, `Spell"; DROP TABLE server_materialized_tables; --`),
			run: func(ref TableRef) error {
				_, _, err := builder.BuildSchemaSQL(ref)
				return err
			},
		},
		{
			name: "column",
			ref:  tableRef(root, "Spell"),
			run: func(ref TableRef) error {
				_, _, err := builder.BuildRowsSQL(ref, RowsQuery{Columns: []string{`ID"; DROP TABLE x; --`}, Limit: 1})
				return err
			},
		},
		{
			name: "where column",
			ref:  tableRef(root, "Spell"),
			run: func(ref TableRef) error {
				_, _, err := builder.BuildRowsSQL(ref, RowsQuery{Where: map[string]interface{}{`ID OR 1=1`: 1}, Limit: 1})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(tc.ref); err == nil {
				t.Fatal("expected invalid identifier error")
			}
		})
	}
}

func TestQueryBuilderRejectsParquetPathTraversal(t *testing.T) {
	root := t.TempDir()
	builder := NewQueryBuilder(root)
	evilPath := filepath.Join(root, "..", "outside", "Spell.parquet")

	_, _, err := builder.BuildSchemaSQL(TableRef{TableName: "Spell", ParquetPath: evilPath})
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}

func tableRef(root string, name string) TableRef {
	return TableRef{
		TableName:   name,
		ParquetPath: filepath.Join(root, "db2", "us", "wow", "build-1", "enUS", name+".parquet"),
	}
}

func assertSQLContains(t *testing.T, query string, want string) {
	t.Helper()
	if !strings.Contains(query, want) {
		t.Fatalf("query %q does not contain %q", query, want)
	}
}

func assertNotInterpolated(t *testing.T, query string, value string) {
	t.Helper()
	if strings.Contains(query, value) {
		t.Fatalf("query interpolated user value %q: %s", value, query)
	}
}

func assertArgs(t *testing.T, got []interface{}, want ...interface{}) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %#v, want %#v; all args %#v", i, got[i], want[i], got)
		}
	}
}
