package db2

import (
	"context"
	"fmt"
	"strconv"
	"testing"
)

type benchmarkReader struct {
	rows map[uint32]map[string]interface{}
}

func (r *benchmarkReader) GetRow(id uint32) map[string]interface{}       { return r.rows[id] }
func (r *benchmarkReader) GetAllRows() map[uint32]map[string]interface{} { return r.rows }
func (r *benchmarkReader) Size() int                                     { return len(r.rows) }
func (r *benchmarkReader) GetRows(ids []uint32, fields []string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		if row := r.rows[id]; row != nil {
			out = append(out, projectFields(row, fields))
		}
	}
	return out
}
func (r *benchmarkReader) Scan(fields []string, filterFn func(map[string]interface{}) bool, limit int) []map[string]interface{} {
	out := make([]map[string]interface{}, 0)
	for id := uint32(0); id < uint32(len(r.rows)); id++ {
		row := r.rows[id]
		if row == nil || (filterFn != nil && !filterFn(row)) {
			continue
		}
		out = append(out, projectFields(row, fields))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
func (r *benchmarkReader) GetRelationshipRows(fk uint32) ([]map[string]interface{}, bool) {
	row := r.rows[fk]
	if row == nil {
		return []map[string]interface{}{}, true
	}
	return []map[string]interface{}{row}, true
}

func benchmarkSnapshot(b *testing.B) *Snapshot {
	rows := make(map[uint32]map[string]interface{}, 10000)
	for i := 0; i < 10000; i++ {
		rows[uint32(i)] = map[string]interface{}{"ID": uint32(i), "Name": "spell-" + strconv.Itoa(i), "Value": i}
	}
	engine := NewEngine()
	if err := engine.Register("Bench", []SchemaField{{Name: "ID"}, {Name: "Name"}, {Name: "Value"}}, &benchmarkReader{rows: rows}); err != nil {
		b.Fatal(err)
	}
	return engine.Snapshot()
}

func BenchmarkEnginePointLookup(b *testing.B) {
	snapshot := benchmarkSnapshot(b)
	defer snapshot.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := snapshot.Execute(context.Background(), QueryPlan{Table: "Bench", Mode: PlanRows, IDs: []uint32{uint32(i % 10000)}, Fields: []string{"ID", "Name"}}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineBatchGet(b *testing.B) {
	snapshot := benchmarkSnapshot(b)
	defer snapshot.Close()
	ids := make([]uint32, 128)
	for i := range ids {
		ids[i] = uint32(i * 71 % 10000)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := snapshot.Execute(context.Background(), QueryPlan{Table: "Bench", Mode: PlanRows, IDs: ids, Fields: []string{"ID", "Name"}}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineProjectedScan(b *testing.B) {
	snapshot := benchmarkSnapshot(b)
	defer snapshot.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := snapshot.Execute(context.Background(), QueryPlan{Table: "Bench", Mode: PlanRows, Fields: []string{"ID", "Name"}, Limit: 1000}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineSearch(b *testing.B) {
	snapshot := benchmarkSnapshot(b)
	defer snapshot.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := snapshot.Execute(context.Background(), QueryPlan{Table: "Bench", Mode: PlanSearch, SearchField: "Name", SearchQuery: fmt.Sprintf("spell-%d", i%10000), Limit: 1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEngineRelationshipLookup(b *testing.B) {
	snapshot := benchmarkSnapshot(b)
	defer snapshot.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := snapshot.Execute(context.Background(), QueryPlan{Table: "Bench", Mode: PlanForeignKey, ForeignField: "ID", ForeignValue: uint32(i % 10000), Limit: 1}); err != nil {
			b.Fatal(err)
		}
	}
}
