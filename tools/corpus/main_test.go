package main

import (
	"encoding/binary"
	"testing"

	"wowdata/internal/db2"
)

type noIDCorpusReader struct {
	rows       []map[string]interface{}
	scanLimits []int
	allCalls   int
}

func (r *noIDCorpusReader) GetRow(uint32) map[string]interface{} { return nil }

func (r *noIDCorpusReader) GetAllRows() map[uint32]map[string]interface{} {
	r.allCalls++
	return nil
}

func (r *noIDCorpusReader) Scan(_ []string, _ func(map[string]interface{}) bool, limit int) []map[string]interface{} {
	r.scanLimits = append(r.scanLimits, limit)
	if limit > 0 && len(r.rows) > limit {
		return r.rows[:limit]
	}
	return r.rows
}

func TestDifferentialBoundsTablesWithoutExtractableIDs(t *testing.T) {
	reader := &noIDCorpusReader{rows: []map[string]interface{}{
		{"Name": "one"},
		{"Name": "two"},
		{"Name": "three"},
		{"Name": "four"},
	}}

	ok, pointSamples, _, _, _, _, errText := differential(reader, nil, "NoID")
	if !ok || errText != "" {
		t.Fatalf("differential = ok %t, error %q", ok, errText)
	}
	if pointSamples != 0 {
		t.Fatalf("point samples = %d, want 0", pointSamples)
	}
	if reader.allCalls != 0 {
		t.Fatalf("GetAllRows calls = %d, want 0", reader.allCalls)
	}
	if len(reader.scanLimits) != 2 || reader.scanLimits[0] != 3 || reader.scanLimits[1] != 3 {
		t.Fatalf("scan limits = %v, want [3 3]", reader.scanLimits)
	}
}

func TestInferRelationshipFieldUsesObservedRowsWithoutDBDRelationType(t *testing.T) {
	schema := []db2.SchemaField{
		{Name: "ID", Type: db2.FieldUInt32},
		{Name: "ParentID", Type: db2.FieldUInt32},
	}
	rows := []map[string]interface{}{
		{"ID": uint32(10), "ParentID": uint16(7)},
		{"ID": uint32(11), "ParentID": uint16(7)},
	}
	if got := inferRelationshipField(schema, rows, 7); got != "ParentID" {
		t.Fatalf("relationship field = %q, want ParentID", got)
	}
}

func TestInferRelationshipFieldUsesCompleteRelationshipGroup(t *testing.T) {
	schema := []db2.SchemaField{
		{Name: "CoincidentValue", Type: db2.FieldUInt32},
		{Name: "ParentID", Type: db2.FieldUInt32},
	}
	rows := []map[string]interface{}{
		{"CoincidentValue": uint32(7), "ParentID": uint32(7)},
		{"CoincidentValue": uint32(7), "ParentID": uint32(7)},
		{"CoincidentValue": uint32(7), "ParentID": uint32(7)},
		{"CoincidentValue": uint32(8), "ParentID": uint32(7)},
	}
	if got := inferRelationshipField(schema, rows, 7); got != "ParentID" {
		t.Fatalf("relationship field = %q, want ParentID", got)
	}
}

func TestStrategyCountIncludesEveryClassifiedSection(t *testing.T) {
	strategies := map[string]int{
		db2.LookupSorted: 4,
		db2.LookupDense:  2,
		db2.LookupHash:   1,
	}
	if got := strategyCount(strategies); got != 7 {
		t.Fatalf("strategy count = %d, want 7", got)
	}
}

func TestCorpusStatusFailsOnAnyRootPresentTableError(t *testing.T) {
	tests := []struct {
		name           string
		errors         int
		differentialOK bool
		want           string
	}{
		{name: "clean", differentialOK: true, want: "pass"},
		{name: "parse error", errors: 1, differentialOK: true, want: "fail"},
		{name: "differential", differentialOK: false, want: "fail"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := report{TotalErrors: test.errors, DifferentialOK: test.differentialOK}
			updateReportStatus(&result)
			if result.Status != test.want {
				t.Fatalf("status = %q, want %q", result.Status, test.want)
			}
		})
	}
}

func TestSchemaLessHeaderGateOnlyAcceptsProvablyEmptyWDC(t *testing.T) {
	emptyData := make([]byte, 204)
	binary.LittleEndian.PutUint32(emptyData, 0x35434457)
	emptyReader, err := db2.NewWDCReaderFromBytes("Empty", emptyData, nil)
	if err != nil {
		t.Fatalf("parse empty WDC5: %v", err)
	}
	if !isProvablyEmptyWDC(emptyReader) {
		t.Fatal("zero-record, zero-payload WDC5 should pass the header-only gate")
	}

	nonEmpty := *emptyReader
	nonEmpty.Sections = []db2.Section{{Header: db2.SectionHeader{RecordCount: 1}}}
	if isProvablyEmptyWDC(&nonEmpty) {
		t.Fatal("non-empty schema-less WDC must remain an error")
	}

	payload := *emptyReader
	payload.Sections = []db2.Section{{Header: db2.SectionHeader{StringTableSize: 1}}}
	if isProvablyEmptyWDC(&payload) {
		t.Fatal("schema-less WDC with section payload must remain an error")
	}
}

func TestFullDifferentialStreamsEveryBaseAndCopyRow(t *testing.T) {
	schema := []db2.SchemaField{
		{Name: "ID", Type: db2.FieldUInt32},
		{Name: "Value", Type: db2.FieldUInt32},
	}
	reader, err := db2.NewWDCReaderFromBytes("Full", db2.BuildMinimalWDC2ForTest(), schema)
	if err != nil {
		t.Fatal(err)
	}
	reader.CopyTable[9] = 1
	reader.CopyTable[4] = 1
	ok, rows, pointRows, legacySHA, engineSHA, relationshipOK, _, errText := fullDifferential(reader, schema, "Full")
	if !ok || !relationshipOK || errText != "" {
		t.Fatalf("full differential ok=%t relationship=%t error=%q", ok, relationshipOK, errText)
	}
	if rows != 4 || pointRows != 4 || legacySHA == "" || legacySHA != engineSHA {
		t.Fatalf("full rows=%d pointRows=%d legacy=%q engine=%q", rows, pointRows, legacySHA, engineSHA)
	}
}

func TestFullDifferentialAvoidsQuadraticPointReplayForInlineScan(t *testing.T) {
	schema := []db2.SchemaField{
		{Name: "ID", Type: db2.FieldUInt32},
		{Name: "Value", Type: db2.FieldUInt32},
	}
	reader, err := db2.NewWDCReaderFromBytes("Inline", db2.BuildMinimalWDC2ForTest(), schema)
	if err != nil {
		t.Fatal(err)
	}
	reader.Sections[0].IDList = nil
	reader.Sections[0].IDListSorted = false
	ok, rows, pointRows, legacySHA, engineSHA, relationshipOK, _, errText := fullDifferential(reader, schema, "Inline")
	if !ok || !relationshipOK || errText != "" || rows != 2 || pointRows != 0 || legacySHA != engineSHA {
		t.Fatalf("inline full differential ok=%t rows=%d point=%d relationship=%t hashes=%s/%s error=%q", ok, rows, pointRows, relationshipOK, legacySHA, engineSHA, errText)
	}
}

func TestValidateFullRelationshipsUsesTypedStreamEvidence(t *testing.T) {
	reader := &db2.WDCReader{
		Sections:           []db2.Section{{IsNormal: true, Header: db2.SectionHeader{RecordCount: 3}, RelationshipMap: map[uint32]uint32{0: 7, 1: 7, 2: 9}}},
		RelationshipLookup: map[uint32][]uint32{7: {10, 11}, 9: {12}},
	}
	ok, digest, errText := validateFullRelationships(reader, []uint32{10, 11, 12})
	if !ok || digest == "" || errText != "" {
		t.Fatalf("relationship validation ok=%t digest=%q error=%q", ok, digest, errText)
	}
	reader.RelationshipLookup[7] = []uint32{10, 99}
	ok, _, errText = validateFullRelationships(reader, []uint32{10, 11, 12})
	if ok || errText != "relationship 7 references missing row 99" {
		t.Fatalf("missing row validation ok=%t error=%q", ok, errText)
	}
	reader.RelationshipLookup[7] = []uint32{10, 11}
	reader.RelationshipLookup[9] = nil
	ok, _, errText = validateFullRelationships(reader, []uint32{10, 11, 12})
	if ok || errText != "relationship tuples differ: lookup=2 expected=3" {
		t.Fatalf("tuple validation ok=%t error=%q", ok, errText)
	}
}
