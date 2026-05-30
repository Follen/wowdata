package wowdata

import "testing"

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
