package metadata

import (
	"path/filepath"
	"testing"
)

func TestMaterializedTableFingerprintAndStaleState(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "metadata.sqlite"), "../../../migrations/sqlite")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	table := MaterializedTable{
		Region:              "cn",
		Product:             "wow",
		BuildKey:            "abcd",
		BuildName:           "12.0.0.61234",
		Locale:              "zhCN",
		TableName:           "SpellName",
		DB2FileDataID:       123,
		DBDDefinitionHash:   "dbd-hash",
		DecoderVersion:      "decoder-v1",
		MaterializerVersion: "materializer-v1",
		ParquetPath:         "root/db2/cn/wow/abcd/zhCN/SpellName.parquet",
		RowCount:            42,
		State:               StateValid,
	}
	if err := UpsertMaterializedTable(db, table); err != nil {
		t.Fatalf("upsert materialized table: %v", err)
	}

	got, err := GetMaterializedTable(db, "cn", "wow", "abcd", "zhCN", "SpellName")
	if err != nil {
		t.Fatalf("get materialized table: %v", err)
	}
	if got.State != StateValid {
		t.Fatalf("state = %q, want %q", got.State, StateValid)
	}
	if !got.FingerprintMatches(table) {
		t.Fatalf("fingerprint mismatch for identical table: got=%#v want=%#v", got, table)
	}

	changed := table
	changed.DecoderVersion = "decoder-v2"
	if got.FingerprintMatches(changed) {
		t.Fatal("fingerprint matched after decoder version changed")
	}

	if err := MarkMaterializedTableStale(db, "cn", "wow", "abcd", "zhCN", "SpellName"); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	got, err = GetMaterializedTable(db, "cn", "wow", "abcd", "zhCN", "SpellName")
	if err != nil {
		t.Fatalf("get stale materialized table: %v", err)
	}
	if got.State != StateStale {
		t.Fatalf("state after stale = %q, want %q", got.State, StateStale)
	}
}
