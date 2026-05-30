package wowdata

import "testing"

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
