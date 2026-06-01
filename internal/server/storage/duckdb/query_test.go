package duckdb

import (
	"os"
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

func TestQueryBuilderRejectsNegativeLimitAndOffset(t *testing.T) {
	root := t.TempDir()
	builder := NewQueryBuilder(root)
	table := tableRef(root, "Spell")

	cases := []struct {
		name string
		run  func() error
	}{
		{
			name: "rows negative limit",
			run: func() error {
				_, _, err := builder.BuildRowsSQL(table, RowsQuery{Limit: -1})
				return err
			},
		},
		{
			name: "rows negative offset",
			run: func() error {
				_, _, err := builder.BuildRowsSQL(table, RowsQuery{Offset: -1})
				return err
			},
		},
		{
			name: "search negative limit",
			run: func() error {
				_, _, err := builder.BuildSearchSQL(table, SearchQuery{SearchColumn: "Name_lang", Pattern: "foo", Limit: -1})
				return err
			},
		},
		{
			name: "foreign key negative limit",
			run: func() error {
				_, _, err := builder.BuildForeignKeySQL(table, ForeignKeyQuery{Field: "ID", Value: 1, Limit: -1})
				return err
			},
		},
		{
			name: "stream negative limit",
			run: func() error {
				_, _, err := builder.BuildStreamSQL(table, StreamQuery{Limit: -1})
				return err
			},
		},
		{
			name: "stream negative offset",
			run: func() error {
				_, _, err := builder.BuildStreamSQL(table, StreamQuery{Offset: -1})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("expected negative limit/offset error")
			}
		})
	}
}

func TestQueryBuilderRejectsSymlinkEscapeFromTrustedRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "Spell.parquet"), []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside parquet: %v", err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable on this platform or account: %v", err)
	}

	_, _, err := NewQueryBuilder(root).BuildSchemaSQL(TableRef{
		TableName:   "Spell",
		ParquetPath: filepath.Join(link, "Spell.parquet"),
	})
	if err == nil {
		t.Fatal("expected symlink escape error")
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
