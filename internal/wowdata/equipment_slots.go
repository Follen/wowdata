package wowdata

var itemInventoryTypeNames = map[int]string{
	0: "None", 1: "Head", 2: "Neck", 3: "Shoulder", 4: "Shirt",
	5: "Chest", 6: "Waist", 7: "Legs", 8: "Feet", 9: "Wrist",
	10: "Hands", 11: "Finger", 12: "Trinket", 13: "One-Hand",
	14: "Off Hand", 15: "Ranged", 16: "Back", 17: "Two-Hand",
	18: "Bag", 19: "Tabard", 20: "Chest", 21: "Main Hand",
	22: "Off Hand", 23: "Off Hand", 24: "Ammo", 25: "Thrown",
	26: "Ranged", 27: "Quiver", 28: "Relic",
}

var equipmentSlotNames = map[int]string{
	1: "Head", 2: "Neck", 3: "Shoulder", 4: "Shirt",
	5: "Chest", 6: "Waist", 7: "Legs", 8: "Feet",
	9: "Wrist", 10: "Hands", 15: "Back", 16: "Main-hand",
	17: "Off-hand", 19: "Tabard",
}

var inventoryTypeToEquipmentSlotID = map[int]int{
	1:  1,
	2:  2,
	3:  3,
	4:  4,
	5:  5,
	6:  6,
	7:  7,
	8:  8,
	9:  9,
	10: 10,
	13: 16,
	14: 17,
	15: 16,
	16: 15,
	17: 16,
	19: 19,
	20: 5,
	21: 16,
	22: 17,
	23: 17,
	26: 16,
}

func GetItemSlotNameForInventoryType(invType int) string {
	if name, ok := itemInventoryTypeNames[invType]; ok {
		return name
	}
	return "Unknown"
}

func GetEquipmentSlotIDForInventoryType(invType int) int {
	if id, ok := inventoryTypeToEquipmentSlotID[invType]; ok {
		return id
	}
	return 0
}

func GetEquipmentSlotName(slotID int) string {
	if name, ok := equipmentSlotNames[slotID]; ok {
		return name
	}
	return "Unknown"
}
