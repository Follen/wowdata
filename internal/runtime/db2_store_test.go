package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"wowdata/internal/sqlquery"
)

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

func TestMemoryDB2StoreRowsAppliesLimitAfterFilter(t *testing.T) {
	store := NewMemoryDB2Store()
	store.AddTable("SpellName", nil, storeTestReader{rows: map[uint32]map[string]interface{}{
		1: {"ID": uint32(1), "Name_lang": "Frostbolt"},
		2: {"ID": uint32(2), "Name_lang": "Fireball"},
	}})
	rows, err := store.Rows("SpellName", nil, nil, "Name_lang=Fireball", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["ID"] != uint32(2) {
		t.Fatalf("rows=%v", rows)
	}
}

func TestMemoryDB2StorePredicateScanStopsAfterMatchingLimit(t *testing.T) {
	store := NewMemoryDB2Store()
	store.AddTable("SpellEffect", nil, storeTestReader{rows: map[uint32]map[string]interface{}{
		1: {"ID": uint32(1), "EffectIndex": int64(1)},
		2: {"ID": uint32(2), "EffectIndex": int64(0)},
		3: {"ID": uint32(3), "EffectIndex": int64(0)},
	}})
	limit := 1
	query := &sqlquery.Select{Items: []sqlquery.SelectItem{{Expr: &sqlquery.Identifier{Name: "ID"}}}, From: sqlquery.TableRef{Name: "SpellEffect"}, Where: &sqlquery.BinaryExpr{Op: "=", Left: &sqlquery.Identifier{Name: "EffectIndex"}, Right: &sqlquery.Literal{Value: int64(0)}}, Limit: &limit}
	result, err := (&sqlquery.Engine{Source: store}).Execute(context.Background(), &sqlquery.Statement{Query: query}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["ID"] != uint32(2) {
		t.Fatalf("rows=%v", result.Rows)
	}
	found := false
	for _, op := range result.Plan.Operators {
		if op.Physical == "WDCPredicateScan" {
			found = true
		}
	}
	if !found {
		t.Fatalf("predicate scan operator missing: %+v", result.Plan.Operators)
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

type storeBenchmarkReader struct {
	rows map[uint32]map[string]interface{}
	rels map[uint32][]uint32
}

func (r *storeBenchmarkReader) GetRow(id uint32) map[string]interface{}       { return r.rows[id] }
func (r *storeBenchmarkReader) GetAllRows() map[uint32]map[string]interface{} { return r.rows }
func (r *storeBenchmarkReader) Size() int                                     { return len(r.rows) }
func (r *storeBenchmarkReader) GetRowsContext(ctx context.Context, ids []uint32, fields []string) ([]map[string]interface{}, error) {
	out := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		if row := r.rows[id]; row != nil {
			out = append(out, projectStoreBenchmarkRow(row, fields))
		}
	}
	return out, ctx.Err()
}
func (r *storeBenchmarkReader) ScanContext(ctx context.Context, fields []string, filter func(map[string]interface{}) bool, limit int) ([]map[string]interface{}, error) {
	out := make([]map[string]interface{}, 0)
	for id := uint32(1); id <= uint32(len(r.rows)); id++ {
		row := r.rows[id]
		if filter == nil || filter(row) {
			out = append(out, projectStoreBenchmarkRow(row, fields))
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, ctx.Err()
}
func (r *storeBenchmarkReader) GetRelationshipRowsBatchContext(ctx context.Context, values []uint32, fields []string) (map[uint32][]map[string]interface{}, error) {
	out := make(map[uint32][]map[string]interface{}, len(values))
	for _, value := range values {
		out[value] = nil
		for _, id := range r.rels[value] {
			out[value] = append(out[value], projectStoreBenchmarkRow(r.rows[id], fields))
		}
	}
	return out, ctx.Err()
}
func projectStoreBenchmarkRow(row map[string]interface{}, fields []string) map[string]interface{} {
	if len(fields) == 0 {
		return row
	}
	out := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		if value, ok := row[field]; ok {
			out[field] = value
		}
	}
	return out
}
func benchmarkMemoryDB2Store(b *testing.B) *MemoryDB2Store {
	b.Helper()
	rows := make(map[uint32]map[string]interface{}, 10000)
	rels := make(map[uint32][]uint32, 1000)
	for id := uint32(1); id <= 10000; id++ {
		parent := id % 1000
		rows[id] = map[string]interface{}{"ID": id, "ParentID": parent, "Name_lang": fmt.Sprintf("spell-%05d", id)}
		rels[parent] = append(rels[parent], id)
	}
	store := NewMemoryDB2Store()
	store.AddTable("Bench", []SchemaField{{Name: "ID", Type: "uint32"}, {Name: "ParentID", Type: "relation"}, {Name: "Name_lang", Type: "string"}}, &storeBenchmarkReader{rows: rows, rels: rels})
	return store
}

func BenchmarkMemoryDB2StoreAtomicPoint(b *testing.B) {
	store := benchmarkMemoryDB2Store(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Rows("Bench", []uint32{uint32(i%10000) + 1}, []string{"ID", "Name_lang"}, "", 1); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryDB2StoreAtomicRelationship(b *testing.B) {
	store := benchmarkMemoryDB2Store(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.ForeignKey("Bench", "ParentID", uint32(i%1000), 10); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryDB2StoreAtomicSearch(b *testing.B) {
	store := benchmarkMemoryDB2Store(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		query := strings.TrimPrefix(fmt.Sprintf("spell-%05d", i%10000+1), "spell-")
		if _, err := store.Search("Bench", "Name_lang", query, 1); err != nil {
			b.Fatal(err)
		}
	}
}
