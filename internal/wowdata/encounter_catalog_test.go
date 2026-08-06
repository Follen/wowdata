package wowdata

import "testing"

type encounterCatalogTestStore struct{}

func (encounterCatalogTestStore) Search(string, string, string, int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"ID": uint32(7), "Name_lang": "虚影尖塔"}}, nil
}

func (encounterCatalogTestStore) ForeignKey(string, string, uint32, int) ([]map[string]interface{}, error) {
	return []map[string]interface{}{
		{"ID": uint32(12), "Name_lang": "老二", "OrderIndex": uint32(2)},
		{"ID": uint32(11), "Name_lang": "老一", "OrderIndex": uint32(1)},
	}, nil
}

func TestResolveJournalEncounterUsesOneBasedOrder(t *testing.T) {
	got, err := ResolveJournalEncounter(encounterCatalogTestStore{}, "虚影尖塔", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.JournalInstanceID != 7 || got.JournalEncounterID != 11 || got.EncounterName != "老一" {
		t.Fatalf("resolved = %#v", got)
	}
}
