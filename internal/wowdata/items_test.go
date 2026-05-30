package wowdata

import "testing"

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
