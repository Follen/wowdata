package wowdata

import "testing"

type encounterDB2TestStore struct {
	rows map[string][]map[string]interface{}
}

func (s encounterDB2TestStore) Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	return s.rows[table], nil
}

func TestEncounterTree(t *testing.T) {
	svc := NewEncounterService()

	svc.AddSection(&EncounterSection{ID: 1, ParentID: 0, Title: "Root", OrderIndex: 1, SpellIDs: []uint32{100}})
	svc.AddSection(&EncounterSection{ID: 2, ParentID: 1, Title: "Child A", OrderIndex: 1, SpellIDs: []uint32{200}})
	svc.AddSection(&EncounterSection{ID: 3, ParentID: 1, Title: "Child B", OrderIndex: 2, SpellIDs: []uint32{300}})
	svc.AddSection(&EncounterSection{ID: 4, ParentID: 2, Title: "Grandchild", OrderIndex: 1, SpellIDs: []uint32{400}})

	result := svc.GetEncounter(0)

	if result.SectionCount != 4 {
		t.Fatalf("sectionCount = %d", result.SectionCount)
	}
	if result.SpellCount != 4 {
		t.Fatalf("spellCount = %d", result.SpellCount)
	}
	if len(result.Sections) != 1 {
		t.Fatalf("root sections = %d", len(result.Sections))
	}

	root := result.Sections[0]
	if root.Title != "Root" {
		t.Fatalf("root title = %s", root.Title)
	}
	if len(root.Children) != 2 {
		t.Fatalf("root children = %d", len(root.Children))
	}
	if root.Children[0].OrderIndex > root.Children[1].OrderIndex {
		t.Fatal("children not sorted by orderIndex")
	}

	childA := root.Children[0]
	if len(childA.Children) != 1 {
		t.Fatalf("childA children = %d", len(childA.Children))
	}
	if childA.Children[0].Title != "Grandchild" {
		t.Fatalf("grandchild title = %s", childA.Children[0].Title)
	}
}

func TestEmptyEncounter(t *testing.T) {
	svc := NewEncounterService()
	result := svc.GetEncounter(999)

	if result.SectionCount != 0 {
		t.Fatalf("sectionCount = %d", result.SectionCount)
	}
	if len(result.Sections) != 0 {
		t.Fatal("should have no sections")
	}
}

func TestEncounterServiceUsesDB2Rows(t *testing.T) {
	svc := NewEncounterServiceWithDB2(encounterDB2TestStore{rows: map[string][]map[string]interface{}{
		"JournalEncounterSection": {
			{"ID": uint32(10), "JournalEncounterID": uint32(42), "ParentSectionID": uint32(0), "Title_lang": "Root", "BodyText_lang": "Body", "OrderIndex": uint32(2), "SpellID": uint32(100), "DifficultyMask": int8(-1), "Type": uint8(2), "IconFlags": int32(8), "FirstChildSectionID": uint16(11), "NextSiblingSectionID": uint16(0)},
			{"ID": uint32(11), "JournalEncounterID": uint32(42), "ParentSectionID": uint32(10), "Title_lang": "Child", "OrderIndex": uint32(1), "SpellID": uint32(200)},
			{"ID": uint32(12), "JournalEncounterID": uint32(99), "ParentSectionID": uint32(0), "Title_lang": "Other", "OrderIndex": uint32(1), "SpellID": uint32(900)},
		},
	}})

	result := svc.GetEncounter(42)
	if result.SectionCount != 2 {
		t.Fatalf("sectionCount = %d", result.SectionCount)
	}
	if result.SpellCount != 2 || result.SpellIDs[0] != 100 || result.SpellIDs[1] != 200 {
		t.Fatalf("spellIDs = %#v", result.SpellIDs)
	}
	if len(result.Sections) != 1 || result.Sections[0].ID != 10 {
		t.Fatalf("sections = %#v", result.Sections)
	}
	if result.Sections[0].BodyText != "Body" || result.Sections[0].DifficultyMask != -1 || result.Sections[0].Type != 2 || result.Sections[0].IconFlags != 8 || result.Sections[0].FirstChildSectionID != 11 {
		t.Fatalf("root section payload = %#v", result.Sections[0])
	}
	if len(result.Sections[0].Children) != 1 || result.Sections[0].Children[0].ID != 11 {
		t.Fatalf("children = %#v", result.Sections[0].Children)
	}
}
