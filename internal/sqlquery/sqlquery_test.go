package sqlquery

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

type fakeSource struct {
	tables                                    map[string]fakeTable
	lookupCalls, relationshipCalls, scanCalls int
	streamCalls                               int
}

func (s *fakeSource) ScanStream(ctx context.Context, table string, fields []string, limit int, yield func(Row) error) (AccessStats, error) {
	s.streamCalls++
	rows := s.tables[lower(table)].rows
	count := 0
	for _, row := range rows {
		if limit > 0 && count >= limit {
			break
		}
		if err := yield(projectTest(row, fields)); err != nil {
			return AccessStats{Physical: "WDCStreamingColumnBatchScan", InputRows: len(rows), DecodedRows: count, OutputRows: count, BatchCount: 1}, err
		}
		count++
	}
	return AccessStats{Physical: "WDCStreamingColumnBatchScan", InputRows: len(rows), VisitedRows: count, ScannedRows: count, DecodedRows: count, OutputRows: count, DecodedFields: len(fields), BatchCount: 1}, ctx.Err()
}

type fakeTable struct {
	info TableInfo
	rows []Row
}

func testSource() *fakeSource {
	return &fakeSource{tables: map[string]fakeTable{
		"spelleffect": {info: TableInfo{Name: "SpellEffect", RowCount: 4, Columns: []Column{{Name: "ID", Type: "uint32", ID: true}, {Name: "SpellID", Type: "relation", Relationship: true}, {Name: "EffectIndex", Type: "uint32"}, {Name: "BasePoints", Type: "int32"}}}, rows: []Row{
			{"ID": uint32(1), "SpellID": uint32(100), "EffectIndex": uint32(0), "BasePoints": int32(10)},
			{"ID": uint32(2), "SpellID": uint32(100), "EffectIndex": uint32(1), "BasePoints": int32(20)},
			{"ID": uint32(3), "SpellID": uint32(200), "EffectIndex": uint32(0), "BasePoints": nil},
			{"ID": uint32(4), "SpellID": uint32(300), "EffectIndex": uint32(0), "BasePoints": int32(5)},
		}},
		"spellname": {info: TableInfo{Name: "SpellName", RowCount: 2, Columns: []Column{{Name: "ID", Type: "uint32", ID: true}, {Name: "Name_lang", Type: "string"}}}, rows: []Row{
			{"ID": uint32(100), "Name_lang": "Fire"}, {"ID": uint32(200), "Name_lang": "Ice"},
		}},
	}}
}

func (s *fakeSource) Describe(ctx context.Context, table string) (TableInfo, error) {
	t, ok := s.tables[lower(table)]
	if !ok {
		return TableInfo{}, sqlErr("sql_unknown_table", Position{}, "table not found: "+table)
	}
	return t.info, ctx.Err()
}
func (s *fakeSource) Lookup(ctx context.Context, table string, ids []uint32, fields []string) ([]Row, AccessStats, error) {
	s.lookupCalls++
	set := map[uint32]bool{}
	for _, id := range ids {
		set[id] = true
	}
	var out []Row
	for _, r := range s.tables[lower(table)].rows {
		if set[r["ID"].(uint32)] {
			out = append(out, projectTest(r, fields))
		}
	}
	return out, AccessStats{Physical: "WDCRecordIDLookup", InputRows: len(s.tables[lower(table)].rows), DecodedRows: len(out), OutputRows: len(out), DecodedFields: len(fields), BatchCount: 1}, ctx.Err()
}
func (s *fakeSource) Relationship(ctx context.Context, table, field string, values []uint32, fields []string) ([]Row, AccessStats, error) {
	s.relationshipCalls++
	set := map[uint32]bool{}
	for _, v := range values {
		set[v] = true
	}
	var out []Row
	for _, r := range s.tables[lower(table)].rows {
		if v, ok := uintValue(r[field]); ok && set[v] {
			out = append(out, projectTest(r, fields))
		}
	}
	return out, AccessStats{Physical: "WDCRelationshipLookup", InputRows: len(s.tables[lower(table)].rows), DecodedRows: len(out), OutputRows: len(out), DecodedFields: len(fields), BatchCount: 1}, ctx.Err()
}
func (s *fakeSource) Scan(ctx context.Context, table string, fields []string, limit int) ([]Row, AccessStats, error) {
	s.scanCalls++
	rows := s.tables[lower(table)].rows
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	out := make([]Row, len(rows))
	for i, r := range rows {
		out[i] = projectTest(r, fields)
	}
	return out, AccessStats{Physical: "WDCColumnBatchScan", InputRows: len(s.tables[lower(table)].rows), VisitedRows: len(rows), ScannedRows: len(rows), DecodedRows: len(rows), OutputRows: len(rows), DecodedFields: len(fields), BatchCount: 1}, ctx.Err()
}
func projectTest(r Row, fields []string) Row {
	if len(fields) == 0 {
		return r
	}
	out := Row{}
	for _, f := range fields {
		out[f] = r[f]
	}
	return out
}
func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func TestParseSelectAndUnsupportedJoin(t *testing.T) {
	st, err := Parse("EXPLAIN ANALYZE SELECT se.ID, COALESCE(se.BasePoints, 0) AS points FROM static.SpellEffect se WHERE se.ID IN (1, 2) ORDER BY se.ID DESC LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Explain || !st.Analyze || st.Query.Limit == nil || *st.Query.Limit != 1 || len(st.Query.Items) != 2 {
		t.Fatalf("statement=%+v", st)
	}
	_, err = Parse("SELECT * FROM A RIGHT JOIN B ON A.ID=B.ID")
	se, ok := err.(*Error)
	if !ok || se.Code != "sql_unsupported_feature" {
		t.Fatalf("error=%v", err)
	}
}

func TestEngineUsesPointLookupAndStableOrder(t *testing.T) {
	src := testSource()
	st, err := Parse("SELECT ID, BasePoints FROM SpellEffect WHERE ID IN (1, 2) ORDER BY ID DESC")
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&Engine{Source: src}).Execute(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if src.lookupCalls != 1 || src.scanCalls != 0 {
		t.Fatalf("lookup=%d scan=%d plan=%+v", src.lookupCalls, src.scanCalls, res.Plan)
	}
	if len(res.Rows) != 2 || res.Rows[0]["ID"] != uint32(2) {
		t.Fatalf("rows=%v", res.Rows)
	}
	if !containsString(res.Plan.Physical, "WDCRecordIDLookup") {
		t.Fatalf("physical=%v", res.Plan.Physical)
	}
}

func TestEngineUsesRelationshipAndParameters(t *testing.T) {
	src := testSource()
	st, err := Parse("SELECT ID, EffectIndex FROM SpellEffect WHERE SpellID = :spell_id ORDER BY EffectIndex")
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&Engine{Source: src}).Execute(context.Background(), st, Parameters{"spell_id": int64(100)})
	if err != nil {
		t.Fatal(err)
	}
	if src.relationshipCalls != 1 || src.scanCalls != 0 || len(res.Rows) != 2 {
		t.Fatalf("relationship=%d scan=%d rows=%v", src.relationshipCalls, src.scanCalls, res.Rows)
	}
	_, err = (&Engine{Source: src}).Execute(context.Background(), st, nil)
	if e, ok := err.(*Error); !ok || e.Code != "sql_missing_parameter" {
		t.Fatalf("missing error=%v", err)
	}
	_, err = (&Engine{Source: src}).Execute(context.Background(), st, Parameters{"spell_id": int64(100), "extra": 1})
	if e, ok := err.(*Error); !ok || e.Code != "sql_unknown_parameter" {
		t.Fatalf("unknown error=%v", err)
	}
}

func TestEngineJoinAggregateAndNullLogic(t *testing.T) {
	src := testSource()
	st, err := Parse("SELECT se.SpellID, sn.Name_lang, COUNT(*) AS effect_count FROM SpellEffect se LEFT JOIN SpellName sn ON sn.ID = se.SpellID WHERE se.BasePoints IS NOT NULL GROUP BY se.SpellID, sn.Name_lang HAVING COUNT(*) >= 1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&Engine{Source: src}).Execute(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows=%v plan=%+v", res.Rows, res.Plan)
	}
	counts := map[uint32]int64{}
	for _, r := range res.Rows {
		counts[r["SpellID"].(uint32)] = r["effect_count"].(int64)
	}
	if !reflect.DeepEqual(counts, map[uint32]int64{100: 2, 300: 1}) {
		t.Fatalf("counts=%v", counts)
	}
	if !containsString(res.Plan.Physical, "TypedHashJoin") || !containsString(res.Plan.Physical, "TypedHashAggregate") {
		t.Fatalf("physical=%v", res.Plan.Physical)
	}
}

func TestTopKHeapMatchesStableFullSort(t *testing.T) {
	src := testSource()
	st, err := Parse("SELECT ID, EffectIndex FROM SpellEffect ORDER BY EffectIndex ASC, ID DESC LIMIT 2 OFFSET 1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&Engine{Source: src}).Execute(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(res.Plan.Physical, "TopKHeap") {
		t.Fatalf("physical=%v", res.Plan.Physical)
	}
	want := []map[string]any{{"ID": uint32(3), "EffectIndex": uint32(0)}, {"ID": uint32(1), "EffectIndex": uint32(0)}}
	if !reflect.DeepEqual(res.Rows, want) {
		t.Fatalf("rows=%v want=%v", res.Rows, want)
	}
}

func TestHashJoinBuildsSmallerSideAndPreservesLeftMajorOrder(t *testing.T) {
	src := testSource()
	st, err := Parse("SELECT se.ID, sn.Name_lang FROM SpellEffect se JOIN SpellName sn ON se.SpellID=sn.ID ORDER BY se.ID")
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&Engine{Source: src}).Execute(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	var join OperatorStats
	for _, op := range res.Plan.Operators {
		if op.Name == "join" {
			join = op
		}
	}
	if join.Physical != "TypedHashJoin" || join.BuildSide != "right" || join.HashEntries != 2 {
		t.Fatalf("join=%+v", join)
	}
	if len(res.Rows) != 3 || res.Rows[0]["ID"] != uint32(1) || res.Rows[2]["ID"] != uint32(3) {
		t.Fatalf("rows=%v", res.Rows)
	}
}

func TestNestedJoinHonorsMemoryBudget(t *testing.T) {
	src := testSource()
	st, err := Parse("SELECT se.ID FROM SpellEffect se CROSS JOIN SpellName sn")
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&Engine{Source: src, MemoryLimitBytes: 128}).Execute(context.Background(), st, nil)
	if e, ok := err.(*Error); !ok || e.Code != "sql_memory_limit" {
		t.Fatalf("error=%v", err)
	}
}

func TestExecuteStreamUsesBoundedSourceAndStopsAtLimit(t *testing.T) {
	src := testSource()
	st, err := Parse("SELECT ID FROM SpellEffect WHERE BasePoints IS NOT NULL LIMIT 2")
	if err != nil {
		t.Fatal(err)
	}
	var ids []uint32
	res, err := (&Engine{Source: src}).ExecuteStream(context.Background(), st, nil, func(_ []string, row map[string]any) error {
		ids = append(ids, row["ID"].(uint32))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if src.streamCalls != 1 || src.scanCalls != 0 || !reflect.DeepEqual(ids, []uint32{1, 2}) || res.Count != 2 {
		t.Fatalf("stream=%d scan=%d ids=%v result=%+v", src.streamCalls, src.scanCalls, ids, res)
	}
}

func TestExtractTableNames(t *testing.T) {
	names, err := ExtractTableNames("WITH e AS (SELECT ID FROM SpellEffect) SELECT e.ID, n.Name_lang FROM e JOIN SpellName n ON n.ID=e.ID")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"SpellEffect", "SpellName"}) {
		t.Fatalf("names=%v", names)
	}
}

func TestNotLikeAndInSubquery(t *testing.T) {
	src := testSource()
	st, err := Parse("SELECT ID FROM SpellEffect WHERE SpellID IN (SELECT ID FROM SpellName WHERE Name_lang NOT LIKE 'I%') ORDER BY ID")
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&Engine{Source: src}).Execute(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 || res.Rows[0]["ID"] != uint32(1) || res.Rows[1]["ID"] != uint32(2) {
		t.Fatalf("rows=%v", res.Rows)
	}
}

func TestExplainDoesNotReadRows(t *testing.T) {
	src := testSource()
	st, err := Parse("EXPLAIN SELECT ID FROM SpellEffect WHERE ID=1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&Engine{Source: src}).Execute(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if src.lookupCalls != 0 || src.scanCalls != 0 || res.Count != 0 {
		t.Fatalf("lookup=%d scan=%d result=%+v", src.lookupCalls, src.scanCalls, res)
	}
	if !containsString(res.Plan.Physical, "WDCRecordIDLookup") {
		t.Fatalf("plan=%+v", res.Plan)
	}
}

func TestCTEDerivedAndRecursiveUnionAll(t *testing.T) {
	src := testSource()
	queries := []struct {
		sql  string
		want int
	}{{"WITH e AS (SELECT ID FROM SpellEffect WHERE ID IN (1,2)) SELECT ID FROM e ORDER BY ID", 2}, {"SELECT x.ID FROM (SELECT ID FROM SpellEffect WHERE ID=1) x", 1}, {"WITH RECURSIVE chain AS (SELECT ID, SpellID FROM SpellEffect WHERE ID=1 UNION ALL SELECT se.ID, se.SpellID FROM SpellEffect se JOIN chain c ON se.ID=c.ID+1 WHERE se.ID<=3) SELECT ID FROM chain ORDER BY ID", 3}}
	for _, tc := range queries {
		st, err := Parse(tc.sql)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.sql, err)
		}
		res, err := (&Engine{Source: src}).Execute(context.Background(), st, nil)
		if err != nil {
			t.Fatalf("execute %s: %v", tc.sql, err)
		}
		if len(res.Rows) != tc.want {
			t.Fatalf("%s rows=%v", tc.sql, res.Rows)
		}
	}
}
func containsString(in []string, want string) bool {
	for _, v := range in {
		if v == want {
			return true
		}
	}
	return false
}
