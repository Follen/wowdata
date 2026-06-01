package wowdata

import "testing"

type itemDB2TestStore struct {
	rows map[string][]map[string]interface{}
}

func (s itemDB2TestStore) Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	return s.rows[table], nil
}

func TestItemGet(t *testing.T) {
	svc := NewItemService()
	svc.AddItem(ItemSummary{
		ID: 19019, Name: "Thunderfury", InventoryType: 13,
		ClassID: 2, SubclassID: 7, Quality: 5,
	})

	item := svc.GetItem(19019)
	if item == nil {
		t.Fatal("item not found")
	}
	if item.SlotName == "None" || item.SlotName == "" {
		t.Fatalf("slotName = '%s'", item.SlotName)
	}
	if item.Name != "Thunderfury" {
		t.Fatalf("name = %s", item.Name)
	}

	if svc.GetItem(99999) != nil {
		t.Fatal("expected nil for unknown item")
	}
}

func TestItemIsBow(t *testing.T) {
	svc := NewItemService()
	svc.AddItem(ItemSummary{ID: 100, InventoryType: 15, SubclassID: 2})
	svc.AddItem(ItemSummary{ID: 200, InventoryType: 1, SubclassID: 0})

	if !svc.IsItemBow(100) {
		t.Fatal("item 100 should be a bow")
	}
	if svc.IsItemBow(200) {
		t.Fatal("item 200 should not be a bow")
	}
}

func TestInventoryTypeDisplayNames(t *testing.T) {
	cases := map[int]string{
		0: "None", 1: "Head", 2: "Neck", 3: "Shoulder", 4: "Shirt",
		5: "Chest", 6: "Waist", 7: "Legs", 8: "Feet", 9: "Wrist",
		10: "Hands", 11: "Finger", 12: "Trinket", 13: "One-Hand",
		14: "Off Hand", 15: "Ranged", 16: "Back", 17: "Two-Hand",
		18: "Bag", 19: "Tabard", 20: "Chest", 21: "Main Hand",
		22: "Off Hand", 23: "Off Hand", 24: "Ammo", 25: "Thrown",
		26: "Ranged", 27: "Quiver", 28: "Relic",
	}
	for invType, want := range cases {
		if got := GetItemSlotNameForInventoryType(invType); got != want {
			t.Fatalf("inventory type %d should be %s, got %q", invType, want, got)
		}
	}
	if got := GetItemSlotNameForInventoryType(999); got != "Unknown" {
		t.Fatalf("unknown inventory type = %q", got)
	}
}

func TestEquipmentSlotMappingIsSeparateFromInventoryTypeNames(t *testing.T) {
	cases := map[int]int{
		1: 1, 2: 2, 3: 3, 6: 6, 9: 9, 10: 10,
		12: 0, // trinkets have an item inventory type, but no character model equipment slot.
		13: 16, 14: 17, 21: 16, 22: 17, 23: 17,
	}
	for invType, want := range cases {
		if got := GetEquipmentSlotIDForInventoryType(invType); got != want {
			t.Fatalf("inventory type %d equipment slot = %d, want %d", invType, got, want)
		}
	}
	if got := GetEquipmentSlotName(16); got != "Main-hand" {
		t.Fatalf("equipment slot 16 = %q", got)
	}
}

func TestItemServiceUsesDB2SummaryRows(t *testing.T) {
	svc := NewItemServiceWithDB2(itemDB2TestStore{rows: map[string][]map[string]interface{}{
		"Item":       {{"ID": uint32(19019), "ClassID": uint32(2), "SubclassID": uint32(7)}},
		"ItemSparse": {{"ID": uint32(19019), "Display_lang": "Thunderfury", "InventoryType": uint32(13), "OverallQualityID": uint32(5)}},
	}})

	item := svc.GetItem(19019)
	if item == nil {
		t.Fatal("item not found")
	}
	if item.Name != "Thunderfury" || item.ClassID != 2 || item.SubclassID != 7 || item.Quality != 5 {
		t.Fatalf("item = %#v", item)
	}
	if item.SlotName == "" || item.SlotName == "None" {
		t.Fatalf("slotName = %q", item.SlotName)
	}
}

func TestItemServiceUsesNarrowDB2NumericTypes(t *testing.T) {
	svc := NewItemServiceWithDB2(itemDB2TestStore{rows: map[string][]map[string]interface{}{
		"Item":       {{"ID": uint32(25), "ClassID": uint8(2), "SubclassID": uint8(7)}},
		"ItemSparse": {{"ID": uint32(25), "Display_lang": "Worn Shortsword", "InventoryType": int8(21), "OverallQualityID": int8(1)}},
	}})

	item := svc.GetItem(25)
	if item == nil {
		t.Fatal("item not found")
	}
	if item.ClassID != 2 || item.SubclassID != 7 || item.InventoryType != 21 || item.Quality != 1 {
		t.Fatalf("item = %#v", item)
	}
}

func TestItemServiceUsesDB2DisplayModelTextureAndGeosetRows(t *testing.T) {
	svc := NewItemServiceWithDB2(itemDB2TestStore{rows: map[string][]map[string]interface{}{
		"ItemModifiedAppearance":     {{"ItemID": uint32(100), "ItemAppearanceID": uint32(200)}},
		"ItemAppearance":             {{"ID": uint32(200), "ItemDisplayInfoID": uint32(300)}},
		"ItemDisplayInfo":            {{"ID": uint32(300), "ModelResourcesID": []uint32{400}, "ModelMaterialResourcesID": []uint32{500}, "GeosetGroup": []int{1, 2}, "HelmetGeosetVis": []int{77, 88}}},
		"ModelFileData":              {{"ID": uint32(9000), "ModelResourcesID": uint32(400)}},
		"TextureFileData":            {{"ID": uint32(9100), "MaterialResourcesID": uint32(500), "UsageType": uint32(0)}},
		"ComponentModelFileData":     {{"ID": uint32(9000), "RaceID": uint32(1), "GenderIndex": uint32(0)}},
		"ItemDisplayInfoMaterialRes": {{"ItemDisplayInfoID": uint32(300), "ComponentSection": uint32(3), "MaterialResourcesID": uint32(500)}},
		"HelmetGeosetData":           {{"HelmetGeosetVisDataID": uint32(77), "RaceID": uint32(1), "HideGeosetGroup": uint32(27)}},
	}})

	models := svc.GetItemModels(100, 1, 0)
	if models.DisplayID != 300 || len(models.Models) != 1 || models.Models[0] != 9000 || len(models.Textures) != 1 || models.Textures[0] != 9100 {
		t.Fatalf("models = %#v", models)
	}

	geo := svc.GetItemGeosets(100)
	if geo == nil || len(geo.GeosetGroup) != 2 || geo.GeosetGroup[0] != 1 || len(geo.HelmetHide) != 1 || geo.HelmetHide[0] != 27 {
		t.Fatalf("geo = %#v", geo)
	}

	tex := svc.GetItemTextures(100)
	if tex == nil || len(tex.Sections) != 1 || tex.Sections[0].Section != 3 || tex.Sections[0].FileDataID != 9100 {
		t.Fatalf("textures = %#v", tex)
	}
}

func TestItemServiceSelectsModelForRequestedRaceGender(t *testing.T) {
	svc := NewItemServiceWithDB2(itemDB2TestStore{rows: map[string][]map[string]interface{}{
		"ItemModifiedAppearance": {{"ItemID": uint32(100), "ItemAppearanceID": uint32(200)}},
		"ItemAppearance":         {{"ID": uint32(200), "ItemDisplayInfoID": uint32(300)}},
		"ItemDisplayInfo":        {{"ID": uint32(300), "ModelResourcesID": []uint32{400}}},
		"ModelFileData": {
			{"ID": uint32(9000), "ModelResourcesID": uint32(400)},
			{"ID": uint32(9001), "ModelResourcesID": uint32(400)},
			{"ID": uint32(9002), "ModelResourcesID": uint32(400)},
		},
		"ComponentModelFileData": {
			{"ID": uint32(9000), "RaceID": uint32(1), "GenderIndex": uint32(0)},
			{"ID": uint32(9001), "RaceID": uint32(1), "GenderIndex": uint32(2)},
			{"ID": uint32(9002), "RaceID": uint32(0), "GenderIndex": uint32(0)},
		},
	}})

	if got := svc.GetItemModels(100, 1, 1).Models; len(got) != 1 || got[0] != 9001 {
		t.Fatalf("race 1 female should fall back to race any-gender model, got %#v", got)
	}
	if got := svc.GetItemModels(100, 2, 0).Models; len(got) != 1 || got[0] != 9002 {
		t.Fatalf("race 2 male should fall back to any-race model, got %#v", got)
	}
}

func TestItemServiceSelectsLeftAndRightShoulderModelsByPosition(t *testing.T) {
	modelOptions := []uint32{9100, 9101, 9102, 9103}
	svc := NewItemServiceWithDB2(itemDB2TestStore{rows: map[string][]map[string]interface{}{
		"ItemModifiedAppearance": {{"ItemID": uint32(100), "ItemAppearanceID": uint32(200)}},
		"ItemAppearance":         {{"ID": uint32(200), "ItemDisplayInfoID": uint32(300)}},
		"ItemDisplayInfo":        {{"ID": uint32(300), "ModelResourcesID": []uint32{400, 401}}},
		"ModelFileData": {
			{"ID": modelOptions[0], "ModelResourcesID": uint32(400)},
			{"ID": modelOptions[1], "ModelResourcesID": uint32(400)},
			{"ID": modelOptions[2], "ModelResourcesID": uint32(400)},
			{"ID": modelOptions[3], "ModelResourcesID": uint32(400)},
			{"ID": modelOptions[0], "ModelResourcesID": uint32(401)},
			{"ID": modelOptions[1], "ModelResourcesID": uint32(401)},
			{"ID": modelOptions[2], "ModelResourcesID": uint32(401)},
			{"ID": modelOptions[3], "ModelResourcesID": uint32(401)},
		},
		"ComponentModelFileData": {
			{"ID": modelOptions[0], "RaceID": uint32(1), "GenderIndex": uint32(0), "PositionIndex": uint32(0)},
			{"ID": modelOptions[1], "RaceID": uint32(1), "GenderIndex": uint32(1), "PositionIndex": uint32(0)},
			{"ID": modelOptions[2], "RaceID": uint32(1), "GenderIndex": uint32(0), "PositionIndex": uint32(1)},
			{"ID": modelOptions[3], "RaceID": uint32(1), "GenderIndex": uint32(1), "PositionIndex": uint32(1)},
		},
	}})

	got := svc.GetItemModels(100, 1, 1).Models
	if len(got) != 2 || got[0] != 9101 || got[1] != 9103 {
		t.Fatalf("female shoulder models = %#v", got)
	}
}
