package service

import (
	"context"
	"testing"
)

func TestEncounterAssemblerUsesBoundedQueries(t *testing.T) {
	ctx := context.Background()
	fake := newBoundedBusinessQueryService(map[string][]map[string]interface{}{
		"JournalEncounterSection": {
			{"ID": uint32(10), "JournalEncounterID": uint32(900), "Title_lang": "Boss", "BodyText_lang": "Intro", "SpellID": uint32(100), "OrderIndex": uint32(0)},
			{"ID": uint32(11), "JournalEncounterID": uint32(900), "ParentSectionID": uint32(10), "BodyText_lang": "Child", "SpellID": uint32(200), "OrderIndex": uint32(1)},
		},
	})
	fake.requireBounded("JournalEncounterSection")

	got, err := EncounterInfo(ctx, fake, EncounterInfoRequest{Context: RequestContext{Region: "US"}, JournalEncounterID: 900})
	if err != nil {
		t.Fatalf("EncounterInfo error = %v", err)
	}

	fake.assertExactBoundedCalls(t, []businessQueryCall{
		{table: "JournalEncounterSection", idField: "JournalEncounterID"},
	})
	if got.JournalEncounterID != 900 || got.SectionCount != 2 || got.SpellCount != 2 {
		t.Fatalf("EncounterInfo = %#v, want encounter counts", got)
	}
	if diff := diffUint32s(got.SpellIDs, []uint32{100, 200}); diff != "" {
		t.Fatalf("SpellIDs %s", diff)
	}
	if len(got.Sections) != 1 || len(got.Sections[0].Children) != 1 {
		t.Fatalf("Sections = %#v, want one root with one child", got.Sections)
	}
}
