package service

import "context"

type ItemInfoRequest struct {
	Context RequestContext
	ItemID  uint32
}

type ItemInfoResult struct {
	ID            uint32 `json:"id"`
	Name          string `json:"name,omitempty"`
	InventoryType int    `json:"inventoryType"`
	ClassID       int    `json:"classID"`
	SubclassID    int    `json:"subclassID"`
	Quality       int    `json:"quality"`
	SlotName      string `json:"slotName,omitempty"`
}

func ItemInfo(ctx context.Context, query QueryService, req ItemInfoRequest) (*ItemInfoResult, error) {
	itemRows, err := rowsByUint32IDs(ctx, query, req.Context, "Item", "ID", []uint32{req.ItemID})
	if err != nil {
		return nil, err
	}
	sparseRows, err := rowsByUint32IDs(ctx, query, req.Context, "ItemSparse", "ID", []uint32{req.ItemID})
	if err != nil {
		return nil, err
	}
	itemRow := itemRows[req.ItemID]
	sparseRow := sparseRows[req.ItemID]
	if itemRow == nil && sparseRow == nil {
		return nil, nil
	}
	result := &ItemInfoResult{
		ID:            req.ItemID,
		Name:          rowString(sparseRow, "Display_lang"),
		InventoryType: rowInt(sparseRow, "InventoryType"),
		ClassID:       rowInt(itemRow, "ClassID"),
		SubclassID:    rowInt(itemRow, "SubclassID"),
		Quality:       rowInt(sparseRow, "OverallQualityID"),
	}
	result.SlotName = itemSlotName(result.InventoryType)
	if result.Name == "" {
		result.Name = "Unknown item #" + rowString(sparseRow, "ID")
	}
	return result, nil
}

func itemSlotName(inventoryType int) string {
	switch inventoryType {
	case 1:
		return "Head"
	case 2:
		return "Neck"
	case 3:
		return "Shoulder"
	case 4:
		return "Shirt"
	case 5:
		return "Chest"
	case 6:
		return "Waist"
	case 7:
		return "Legs"
	case 8:
		return "Feet"
	case 9:
		return "Wrist"
	case 10:
		return "Hands"
	case 11:
		return "Finger"
	case 12:
		return "Trinket"
	case 13:
		return "One-Hand"
	case 14:
		return "Shield"
	case 15:
		return "Ranged"
	case 16:
		return "Back"
	case 17:
		return "Two-Hand"
	case 18:
		return "Bag"
	case 19:
		return "Tabard"
	case 20:
		return "Robe"
	case 21:
		return "Main Hand"
	case 22:
		return "Off Hand"
	case 23:
		return "Held In Off-hand"
	case 24:
		return "Ammo"
	case 25:
		return "Thrown"
	case 26:
		return "Ranged"
	case 28:
		return "Relic"
	default:
		return ""
	}
}
