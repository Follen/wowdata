package runtime

import "testing"

type storeTestReader struct {
	schema []SchemaField
	rows   map[uint32]map[string]interface{}
}

func (r storeTestReader) GetRow(recordID uint32) map[string]interface{} {
	return r.rows[recordID]
}

func (r storeTestReader) GetAllRows() map[uint32]map[string]interface{} {
	return r.rows
}

func TestMemoryDB2StoreRows(t *testing.T) {
	store := NewMemoryDB2Store()
	store.AddTable("SpellName", []SchemaField{{Name: "ID", Type: "uint32"}, {Name: "Name_lang", Type: "string"}}, storeTestReader{
		rows: map[uint32]map[string]interface{}{
			123: {"ID": uint32(123), "Name_lang": "Fireball"},
		},
	})

	rows, err := store.Rows("SpellName", []uint32{123}, []string{"Name_lang"}, "", 10)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0]["Name_lang"] != "Fireball" {
		t.Fatalf("Name_lang = %v", rows[0]["Name_lang"])
	}
	if _, ok := rows[0]["ID"]; ok {
		t.Fatalf("projected rows should not include ID when fields omit it: %#v", rows[0])
	}
}

func TestMemoryDB2StoreSearch(t *testing.T) {
	store := NewMemoryDB2Store()
	store.AddTable("SpellName", nil, storeTestReader{
		rows: map[uint32]map[string]interface{}{
			1: {"ID": uint32(1), "Name_lang": "Frostbolt"},
			2: {"ID": uint32(2), "Name_lang": "Fireball"},
		},
	})

	rows, err := store.Search("SpellName", "Name_lang", "fire", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(rows) != 1 || rows[0]["ID"] != uint32(2) {
		t.Fatalf("unexpected search rows: %#v", rows)
	}
}

func TestMemoryDB2StoreRowsAppliesFilter(t *testing.T) {
	store := NewMemoryDB2Store()
	store.AddTable("SpellName", nil, storeTestReader{
		rows: map[uint32]map[string]interface{}{
			1: {"ID": uint32(1), "Name_lang": "Frostbolt"},
			2: {"ID": uint32(2), "Name_lang": "Fireball"},
		},
	})

	rows, err := store.Rows("SpellName", nil, nil, "Name_lang=Fireball", 10)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != 1 || rows[0]["ID"] != uint32(2) {
		t.Fatalf("unexpected filtered rows: %#v", rows)
	}
}

func TestMemoryDB2StoreResetKeepsStoreUsable(t *testing.T) {
	store := NewMemoryDB2Store()
	store.AddTable("Old", nil, storeTestReader{rows: map[uint32]map[string]interface{}{1: {"ID": uint32(1)}}})
	store.Reset()
	store.AddTable("New", nil, storeTestReader{rows: map[uint32]map[string]interface{}{2: {"ID": uint32(2)}}})

	if _, err := store.Rows("Old", nil, nil, "", 1); err == nil {
		t.Fatal("expected old table to be removed")
	}
	rows, err := store.Rows("New", nil, nil, "", 1)
	if err != nil {
		t.Fatalf("Rows New: %v", err)
	}
	if len(rows) != 1 || rows[0]["ID"] != uint32(2) {
		t.Fatalf("unexpected rows after reset: %#v", rows)
	}
}
