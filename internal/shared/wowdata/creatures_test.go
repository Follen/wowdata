package wowdata

import "testing"

type creatureDB2TestStore struct {
	rows map[string][]map[string]interface{}
}

func (s creatureDB2TestStore) Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	return s.rows[table], nil
}

func TestCreatureDisplayByID(t *testing.T) {
	svc := NewCreatureService()
	svc.AddDisplay(100, CreatureDisplayInfo{
		DisplayID: 100, ModelFileDataID: 5000,
		Textures: []uint32{6000, 6001}, Scale: 1.0,
	})

	d := svc.GetDisplayByID(100)
	if d == nil {
		t.Fatal("display not found")
	}
	if d.ModelFileDataID != 5000 {
		t.Fatalf("modelFDID = %d", d.ModelFileDataID)
	}
	if len(d.Textures) != 2 {
		t.Fatalf("textures = %d", len(d.Textures))
	}

	if svc.GetDisplayByID(999) != nil {
		t.Fatal("expected nil for unknown display")
	}
}

func TestCreatureDisplayByFDID(t *testing.T) {
	svc := NewCreatureService()
	svc.AddDisplay(100, CreatureDisplayInfo{
		DisplayID: 100, ModelFileDataID: 5000,
	})

	fdid := svc.GetFileDataIDByDisplayID(100)
	if fdid != 5000 {
		t.Fatalf("fdid = %d", fdid)
	}
}

func TestCreatureLegacy(t *testing.T) {
	svc := NewCreatureService()
	svc.AddLegacyEntry("Creature/Bear/Bear.m2", CreatureLegacyInfo{
		ID: 1, ModelPath: "Creature/Bear/Bear.m2",
		Textures: []string{"Creature/Bear/BearSkin.blp"},
	})

	entries := svc.GetLegacyByPath("Creature/Bear/Bear.m2")
	if len(entries) != 1 {
		t.Fatalf("legacy entries = %d", len(entries))
	}
	if entries[0].ID != 1 {
		t.Fatalf("legacy ID = %d", entries[0].ID)
	}
}

func TestCreatureServiceUsesDB2Rows(t *testing.T) {
	svc := NewCreatureServiceWithDB2(creatureDB2TestStore{rows: map[string][]map[string]interface{}{
		"CreatureDisplayInfo":           {{"ID": uint32(100), "ModelID": uint32(10), "TextureVariationFileDataID": []uint32{6000, 6001}}},
		"CreatureModelData":             {{"ID": uint32(10), "FileDataID": uint32(5000), "CreatureGeosetDataID": uint32(1)}},
		"CreatureDisplayInfoGeosetData": {{"CreatureDisplayInfoID": uint32(100), "GeosetIndex": uint32(2), "GeosetValue": uint32(7)}},
	}})

	d := svc.GetDisplayByID(100)
	if d == nil || d.ModelFileDataID != 5000 || len(d.Textures) != 2 || len(d.Variations) != 1 || d.Variations[0] != 307 {
		t.Fatalf("display = %#v", d)
	}
	displays := svc.GetCreatureDisplaysByFileDataID(5000)
	if len(displays) != 1 || displays[0].DisplayID != 100 {
		t.Fatalf("displays = %#v", displays)
	}
}
