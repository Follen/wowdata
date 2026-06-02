package service

import (
	"context"
	"fmt"
)

type DecorItemRequest struct {
	Context         RequestContext
	ID              uint32
	ModelFileDataID uint32
}

type DecorItemResult struct {
	ID                  uint32 `json:"id"`
	Name                string `json:"name,omitempty"`
	ModelFileDataID     uint32 `json:"modelFileDataID,omitempty"`
	ThumbnailFileDataID uint32 `json:"thumbnailFileDataID,omitempty"`
	ItemID              uint32 `json:"itemID,omitempty"`
	GameObjectID        uint32 `json:"gameObjectID,omitempty"`
	Type                int    `json:"type"`
	ModelType           int    `json:"modelType"`
}

func DecorItem(ctx context.Context, query QueryService, req DecorItemRequest) (*DecorItemResult, error) {
	idField := "ID"
	ids := []uint32{req.ID}
	if req.ID == 0 && req.ModelFileDataID != 0 {
		idField = "ModelFileDataID"
		ids = []uint32{req.ModelFileDataID}
	}
	rows, err := rowsByUint32ForeignKey(ctx, query, req.Context, "HouseDecor", idField, ids)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return decorItemResult(rows[0]), nil
}

func decorItemResult(row map[string]interface{}) *DecorItemResult {
	name := rowString(row, "Name_lang")
	if name == "" {
		name = fmt.Sprintf("Decor %d", rowUint32(row, "ID"))
	}
	return &DecorItemResult{
		ID:                  rowUint32(row, "ID"),
		Name:                name,
		ModelFileDataID:     rowUint32(row, "ModelFileDataID"),
		ThumbnailFileDataID: rowUint32(row, "ThumbnailFileDataID"),
		ItemID:              rowUint32(row, "ItemID"),
		GameObjectID:        rowUint32(row, "GameObjectID"),
		Type:                rowInt(row, "Type"),
		ModelType:           rowInt(row, "ModelType"),
	}
}
