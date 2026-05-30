package wowdata

var inventoryTypeToSlotID = map[int]int{
	1:  1,  // Head
	2:  3,  // Neck
	3:  5,  // Shoulder
	4:  4,  // Shirt (Body)
	5:  6,  // Chest
	6:  8,  // Waist
	7:  9,  // Legs
	8:  10, // Feet
	9:  11, // Wrists
	10: 12, // Hands
	11: 2,  // Finger
	12: 7,  // Trinket
	13: 21, // One-Hand
	14: 4,  // Shield (Off Hand)
	15: 15, // Ranged
	16: 16, // Back
	17: 17, // Two-Hand
	18: 6,  // Bag (Chest slot)
	19: 19, // Tabard
	20: 20, // Robe (Chest)
	21: 21, // Main Hand
	22: 22, // Off Hand (weapon)
	23: 23, // Held In Off-Hand
	24: 24, // Projectile (ammo)
	25: 25, // Thrown
	26: 26, // Ranged Right
	27: 27, // Quiver
	28: 28, // Relic
}

var itemSlots = map[int]string{
	0: "None", 1: "Head", 2: "Neck", 3: "Shoulder", 4: "Shirt",
	5: "Chest", 6: "Wrist", 7: "Hands", 8: "Waist", 9: "Legs",
	10: "Feet", 11: "Finger", 12: "Trinket", 13: "One-Hand",
	14: "Shield", 15: "Ranged", 16: "Back", 17: "Two-Hand",
	18: "Bag", 19: "Tabard", 20: "Robe", 21: "Main Hand",
	22: "Off Hand", 23: "Held In Off-Hand", 24: "Ammo", 25: "Thrown",
	26: "Ranged Right", 27: "Quiver", 28: "Relic",
}

func GetSlotIDForInventoryType(invType int) int {
	if id, ok := inventoryTypeToSlotID[invType]; ok {
		return id
	}
	return 0
}

func GetSlotName(slotID int) string {
	if name, ok := itemSlots[slotID]; ok {
		return name
	}
	return "Unknown"
}
