package db2

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildKnownRowLocationsUsesAdaptiveIDIndex(t *testing.T) {
	reader := &WDCReader{
		WDCVersion: 2,
		Sections: []Section{
			{Header: SectionHeader{RecordCount: 3}, IDList: []uint32{10, 20, 30}},
			{Header: SectionHeader{RecordCount: 2}, IDList: []uint32{0, 0}},
			{Header: SectionHeader{RecordCount: 3}, IDList: []uint32{9, 7, 8}},
			{IsNormal: false, OffsetMap: map[uint32]OffsetMapEntry{40: {Offset: 1, Size: 1}}},
			{Header: SectionHeader{RecordCount: 2}, IDList: []uint32{1 << 30, 1}},
		},
	}

	reader.buildKnownRowLocations()

	if !reader.Sections[0].IDListSorted || reader.Sections[0].IDListAllZero {
		t.Fatalf("sorted ID list classification = sorted %t, all-zero %t", reader.Sections[0].IDListSorted, reader.Sections[0].IDListAllZero)
	}
	if !reader.Sections[1].IDListAllZero {
		t.Fatal("zero ID list was not classified as inline scan")
	}
	if reader.Sections[2].IDListSorted {
		t.Fatal("unordered ID list was classified as sorted")
	}
	if len(reader.Sections[2].IDListDense) != 3 || reader.Sections[2].IDListDenseMin != 7 {
		t.Fatalf("dense ID index = min %d, slots %v", reader.Sections[2].IDListDenseMin, reader.Sections[2].IDListDense)
	}
	if got := reader.Sections[2].IDListDense[0]; got != 2 {
		t.Fatalf("dense record index for ID 7 = %d, want encoded index 2", got)
	}
	if len(reader.rowLocations) != 3 {
		t.Fatalf("materialized row locations = %d, want 3 sparse entries", len(reader.rowLocations))
	}
	if got := reader.rowLocations[40]; got.section != 3 || got.record != 40 {
		t.Fatalf("sparse row location = %+v", got)
	}
	if got := reader.rowLocations[1]; got.section != 4 || got.record != 1 {
		t.Fatalf("sparse unordered row location = %+v", got)
	}
}

func TestClassifyIDListRequiresStrictOrdering(t *testing.T) {
	tests := []struct {
		name    string
		ids     []uint32
		allZero bool
		sorted  bool
	}{
		{name: "empty", ids: nil, allZero: true, sorted: true},
		{name: "zero", ids: []uint32{0, 0}, allZero: true, sorted: false},
		{name: "ascending", ids: []uint32{1, 3, 5}, sorted: true},
		{name: "duplicate", ids: []uint32{1, 1}, sorted: false},
		{name: "descending", ids: []uint32{2, 1}, sorted: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allZero, sorted := classifyIDList(test.ids)
			if allZero != test.allZero || sorted != test.sorted {
				t.Fatalf("classifyIDList(%v) = (%t, %t), want (%t, %t)", test.ids, allZero, sorted, test.allZero, test.sorted)
			}
		})
	}
}

func TestPhysicalProfileAccountsForEveryDispatchStrategy(t *testing.T) {
	reader := &WDCReader{
		WDCVersion: 2,
		Sections: []Section{
			{Header: SectionHeader{RecordCount: 3}, IDList: []uint32{10, 20, 30}},
			{Header: SectionHeader{RecordCount: 2}, IDList: []uint32{0, 0}},
			{Header: SectionHeader{RecordCount: 3}, IDList: []uint32{9, 7, 8}},
			{Header: SectionHeader{RecordCount: 2}, IDList: []uint32{1 << 30, 1}},
			{Header: SectionHeader{RecordCount: 1}, IsNormal: false, OffsetMap: map[uint32]OffsetMapEntry{40: {Offset: 1, Size: 1}}},
			{Header: SectionHeader{RecordCount: 1}, IsNormal: true},
			{Header: SectionHeader{RecordCount: 1}, IsEncrypted: true},
			{},
		},
	}
	reader.buildKnownRowLocations()

	profile := reader.PhysicalProfile()
	want := map[string]int{
		LookupSorted: 1, LookupPositional: 1, LookupDense: 1, LookupHash: 1,
		LookupSparseOffset: 1, LookupInlineScan: 2, LookupEncrypted: 1,
	}
	if profile.Sections != 8 || profile.Unclassified != 0 {
		t.Fatalf("profile accounting = %+v", profile)
	}
	for strategy, count := range want {
		if profile.Strategies[strategy] != count {
			t.Fatalf("strategy %s = %d, want %d; profile=%+v", strategy, profile.Strategies[strategy], count, profile)
		}
	}
	if profile.IndexedRows != 6 || profile.DenseSlots != 3 || profile.EstimatedBytes != 48 {
		t.Fatalf("profile index cost = %+v, want rows=6 slots=3 bytes=48", profile)
	}
}

func TestWDCStreamOrdersCopyDestinationsDeterministically(t *testing.T) {
	reader, err := NewWDCReaderFromBytes("CopyOrder", BuildMinimalWDC2ForTest(), []SchemaField{
		{Name: "ID", Type: FieldUInt32},
		{Name: "Value", Type: FieldUInt32},
	})
	if err != nil {
		t.Fatal(err)
	}
	reader.CopyTable[9] = 1
	reader.CopyTable[4] = 1
	var ids []uint32
	err = reader.StreamRowsContext(context.Background(), []string{"ID"}, nil, 0, func(row map[string]interface{}) error {
		ids = append(ids, row["ID"].(uint32))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint32{1, 2, 4, 9}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("stream IDs = %v, want %v", ids, want)
	}
}

func TestWDCStreamResolvesInlineIDsBeforePublishingRelationships(t *testing.T) {
	reader, err := NewWDCReaderFromBytes("InlineRelationship", BuildMinimalWDC2ForTest(), []SchemaField{
		{Name: "ID", Type: FieldUInt32},
		{Name: "ParentID", Type: FieldRelation},
	})
	if err != nil {
		t.Fatal(err)
	}
	reader.Sections[0].IDList = nil
	reader.Sections[0].IDListSorted = false
	reader.Sections[0].RelationshipMap = map[uint32]uint32{0: 7, 1: 9}
	reader.RelationshipLookup = map[uint32][]uint32{7: nil, 9: nil}
	if err := reader.StreamRowsContext(context.Background(), nil, nil, 0, func(map[string]interface{}) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := reader.RelationshipLookup[7]; !reflect.DeepEqual(got, []uint32{1}) {
		t.Fatalf("relationship 7 IDs = %v, want [1]", got)
	}
	if got := reader.RelationshipLookup[9]; !reflect.DeepEqual(got, []uint32{2}) {
		t.Fatalf("relationship 9 IDs = %v, want [2]", got)
	}
}

func TestAllZeroIDListDispatchUsesSchemaIDKind(t *testing.T) {
	inline := &WDCReader{IDField: "ID", Schema: []SchemaField{{Name: "ID", Type: FieldUInt32}}, Sections: []Section{{Header: SectionHeader{RecordCount: 2}, IDList: []uint32{0, 0}}}}
	inline.buildKnownRowLocations()
	if got := inline.PhysicalProfile().Strategies[LookupInlineScan]; got != 1 {
		t.Fatalf("inline all-zero strategy count = %d, want 1", got)
	}
	positional := &WDCReader{IDField: "ID", Schema: []SchemaField{{Name: "ID", Type: FieldNonInlineID}}, Sections: []Section{{Header: SectionHeader{RecordCount: 2}, IDList: []uint32{0, 0}}}}
	positional.buildKnownRowLocations()
	if got := positional.PhysicalProfile().Strategies[LookupPositional]; got != 1 {
		t.Fatalf("non-inline all-zero strategy count = %d, want 1", got)
	}
}
