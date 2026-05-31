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

func TestEquipmentSlots(t *testing.T) {
	if GetSlotName(1) != "Head" {
		t.Fatalf("slot 1 = %s", GetSlotName(1))
	}
	if GetSlotIDForInventoryType(1) != 1 {
		t.Fatalf("inv type 1 -> slot %d", GetSlotIDForInventoryType(1))
	}
	if GetSlotIDForInventoryType(13) == 0 {
		t.Fatalf("inv type 13 (1H) should map to a valid slot")
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
