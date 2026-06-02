package service

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestSpellAssemblerUsesBoundedQueries(t *testing.T) {
	ctx := context.Background()
	fake := newBoundedBusinessQueryService(map[string][]map[string]interface{}{
		"SpellEffect": {
			{"SpellID": uint32(100), "EffectIndex": int32(0), "Effect": int32(3), "EffectTriggerSpell": uint32(200)},
			{"SpellID": uint32(100), "EffectIndex": int32(1), "Effect": int32(64), "EffectMiscValue": uint32(300)},
		},
		"Spell": {
			{"ID": uint32(100), "Description_lang": "Also casts $@spellname400", "AuraDescription_lang": ""},
			{"ID": uint32(200), "Description_lang": "", "AuraDescription_lang": ""},
			{"ID": uint32(300), "Description_lang": "", "AuraDescription_lang": ""},
			{"ID": uint32(400), "Description_lang": "", "AuraDescription_lang": ""},
		},
		"SpellName": {
			{"ID": uint32(100), "Name_lang": "Seed"},
			{"ID": uint32(200), "Name_lang": "Triggered"},
			{"ID": uint32(300), "Name_lang": "Misc referenced"},
			{"ID": uint32(400), "Name_lang": "Description referenced"},
		},
		"SpellMisc": {
			{"SpellID": uint32(100), "CastingTimeIndex": uint32(10), "DurationIndex": uint32(20), "RangeIndex": uint32(30)},
		},
		"SpellCastTimes": {{"ID": uint32(10), "Base": int32(1500), "Minimum": int32(1000)}},
		"SpellDuration":  {{"ID": uint32(20), "Duration": int32(12000), "MaxDuration": int32(12000)}},
		"SpellRange":     {{"ID": uint32(30), "DisplayName_lang": "30 yd", "RangeMin": []float32{0}, "RangeMax": []float32{30}}},
	})
	fake.requireBounded("SpellEffect", "Spell", "SpellName", "SpellMisc", "SpellCastTimes", "SpellDuration", "SpellRange")

	got, err := SpellInfo(ctx, fake, SpellInfoRequest{
		Context:  RequestContext{Region: "US", Product: "wow", Locale: "enUS"},
		SpellIDs: []uint32{100},
		MaxDepth: 1,
	})
	if err != nil {
		t.Fatalf("SpellInfo error = %v", err)
	}

	for _, want := range []businessQueryCall{
		{table: "SpellEffect", idField: "SpellID"},
		{table: "Spell", idField: "ID"},
		{table: "SpellName", idField: "ID"},
		{table: "SpellMisc", idField: "SpellID"},
		{table: "SpellCastTimes", idField: "ID"},
		{table: "SpellDuration", idField: "ID"},
		{table: "SpellRange", idField: "ID"},
	} {
		if !fake.sawBoundedCall(want.table, want.idField) {
			t.Fatalf("missing bounded %s query by %s; calls: %#v", want.table, want.idField, fake.calls)
		}
	}

	if got.SeedCount != 1 || got.TotalCount != 4 {
		t.Fatalf("counts = seed %d total %d, want seed 1 total 4", got.SeedCount, got.TotalCount)
	}
	if diff := diffUint32s(got.Triggers[100], []uint32{200, 300}); diff != "" {
		t.Fatalf("Triggers[100] %s", diff)
	}
	if diff := diffUint32s(got.DescRefs[100], []uint32{400}); diff != "" {
		t.Fatalf("DescRefs[100] %s", diff)
	}
	if got.Spells[100].Name != "Seed" {
		t.Fatalf("seed name = %#v, want Seed", got.Spells[100].Name)
	}
}

type businessQueryCall struct {
	table   string
	idField string
	ids     []uint64
	filter  string
}

type boundedBusinessQueryService struct {
	rows          map[string][]map[string]interface{}
	bounded       map[string]bool
	calls         []businessQueryCall
	unboundedErrs []string
}

func newBoundedBusinessQueryService(rows map[string][]map[string]interface{}) *boundedBusinessQueryService {
	return &boundedBusinessQueryService{
		rows:    rows,
		bounded: map[string]bool{},
		calls:   []businessQueryCall{},
	}
}

func (s *boundedBusinessQueryService) requireBounded(tables ...string) {
	for _, table := range tables {
		s.bounded[table] = true
	}
}

func (s *boundedBusinessQueryService) sawBoundedCall(table, idField string) bool {
	for _, call := range s.calls {
		if call.table == table && call.idField == idField && len(call.ids) > 0 {
			return true
		}
	}
	return false
}

func (s *boundedBusinessQueryService) assertExactBoundedCalls(t *testing.T, want []businessQueryCall) {
	t.Helper()
	if len(s.calls) != len(want) {
		t.Fatalf("query calls = %#v, want exactly %#v", s.calls, want)
	}
	for i, wantCall := range want {
		got := s.calls[i]
		if got.table != wantCall.table || got.idField != wantCall.idField {
			t.Fatalf("query call %d = %#v, want %#v; all calls: %#v", i, got, wantCall, s.calls)
		}
		if len(got.ids) == 0 && got.filter == "" {
			t.Fatalf("query call %d for %s was unbounded: %#v", i, got.table, got)
		}
	}
}

func (s *boundedBusinessQueryService) Schema(context.Context, SchemaRequest) (Schema, error) {
	return Schema{}, nil
}

func (s *boundedBusinessQueryService) Rows(_ context.Context, req QueryRowsRequest) ([]map[string]interface{}, error) {
	call := businessQueryCall{
		table:   req.Table,
		idField: req.IDField,
		ids:     append([]uint64(nil), req.IDs...),
		filter:  req.Filter,
	}
	s.calls = append(s.calls, call)
	if s.bounded[req.Table] && len(req.IDs) == 0 && req.Filter == "" {
		err := fmt.Sprintf("unbounded full-table read for %s", req.Table)
		s.unboundedErrs = append(s.unboundedErrs, err)
		return nil, fmt.Errorf("%s", err)
	}
	rows := s.rows[req.Table]
	if len(req.IDs) == 0 {
		return cloneBusinessRows(rows), nil
	}
	idField := req.IDField
	if idField == "" {
		idField = "ID"
	}
	wanted := map[uint64]bool{}
	for _, id := range req.IDs {
		wanted[id] = true
	}
	out := []map[string]interface{}{}
	for _, row := range rows {
		if wanted[businessRowUint64(row, idField)] {
			out = append(out, cloneBusinessRow(row))
		}
	}
	return out, nil
}

func (s *boundedBusinessQueryService) Search(context.Context, SearchRequest) ([]map[string]interface{}, error) {
	return nil, nil
}

func (s *boundedBusinessQueryService) ForeignKey(context.Context, ForeignKeyRequest) ([]map[string]interface{}, error) {
	return nil, nil
}

func (s *boundedBusinessQueryService) Stream(context.Context, StreamRequest) ([]map[string]interface{}, error) {
	return nil, nil
}

func cloneBusinessRows(rows []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		out = append(out, cloneBusinessRow(row))
	}
	return out
}

func cloneBusinessRow(row map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range row {
		out[key] = value
	}
	return out
}

func businessRowUint64(row map[string]interface{}, key string) uint64 {
	switch value := row[key].(type) {
	case uint64:
		return value
	case uint32:
		return uint64(value)
	case uint:
		return uint64(value)
	case int:
		if value >= 0 {
			return uint64(value)
		}
	case int32:
		if value >= 0 {
			return uint64(value)
		}
	case int64:
		if value >= 0 {
			return uint64(value)
		}
	}
	return 0
}

func diffUint32s(got, want []uint32) string {
	gotCopy := append([]uint32(nil), got...)
	wantCopy := append([]uint32(nil), want...)
	sort.Slice(gotCopy, func(i, j int) bool { return gotCopy[i] < gotCopy[j] })
	sort.Slice(wantCopy, func(i, j int) bool { return wantCopy[i] < wantCopy[j] })
	if reflect.DeepEqual(gotCopy, wantCopy) {
		return ""
	}
	return fmt.Sprintf("= %#v, want %#v", gotCopy, wantCopy)
}
