package service

import "context"

type CreatureDisplayRequest struct {
	Context    RequestContext
	DisplayID  uint32
	FileDataID uint32
}

type CreatureDisplayResult struct {
	DisplayID       uint32   `json:"displayID"`
	ModelID         uint32   `json:"modelID,omitempty"`
	FileDataID      uint32   `json:"fileDataID,omitempty"`
	ModelFileDataID uint32   `json:"modelFileDataID,omitempty"`
	Textures        []uint32 `json:"textures,omitempty"`
	Scale           float32  `json:"scale,omitempty"`
	Variations      []int    `json:"variations,omitempty"`
}

func CreatureDisplay(ctx context.Context, query QueryService, req CreatureDisplayRequest) (*CreatureDisplayResult, error) {
	if req.DisplayID == 0 {
		return creatureDisplayByFileDataID(ctx, query, req)
	}
	displayRows, err := rowsByUint32IDs(ctx, query, req.Context, "CreatureDisplayInfo", "ID", []uint32{req.DisplayID})
	if err != nil {
		return nil, err
	}
	displayRow := displayRows[req.DisplayID]
	if displayRow == nil {
		return nil, nil
	}
	geosetRows, err := rowsByUint32ForeignKey(ctx, query, req.Context, "CreatureDisplayInfoGeosetData", "CreatureDisplayInfoID", []uint32{req.DisplayID})
	if err != nil {
		return nil, err
	}
	modelID := rowUint32(displayRow, "ModelID")
	modelRows, err := rowsByUint32IDs(ctx, query, req.Context, "CreatureModelData", "ID", []uint32{modelID})
	if err != nil {
		return nil, err
	}
	modelFDID := rowUint32(modelRows[modelID], "FileDataID")
	return creatureDisplayResult(displayRow, modelFDID, geosetRows), nil
}

func creatureDisplayByFileDataID(ctx context.Context, query QueryService, req CreatureDisplayRequest) (*CreatureDisplayResult, error) {
	modelRows, err := rowsByUint32IDs(ctx, query, req.Context, "CreatureModelData", "FileDataID", []uint32{req.FileDataID})
	if err != nil {
		return nil, err
	}
	if len(modelRows) == 0 {
		return nil, nil
	}
	var modelID uint32
	for _, modelRow := range modelRows {
		modelID = rowUint32(modelRow, "ID")
		break
	}
	if modelID == 0 {
		return nil, nil
	}
	displayRows, err := rowsByUint32IDs(ctx, query, req.Context, "CreatureDisplayInfo", "ModelID", []uint32{modelID})
	if err != nil {
		return nil, err
	}
	for _, displayRow := range displayRows {
		displayID := rowUint32(displayRow, "ID")
		geosetRows, err := rowsByUint32ForeignKey(ctx, query, req.Context, "CreatureDisplayInfoGeosetData", "CreatureDisplayInfoID", []uint32{displayID})
		if err != nil {
			return nil, err
		}
		return creatureDisplayResult(displayRow, req.FileDataID, geosetRows), nil
	}
	return nil, nil
}

func creatureDisplayResult(displayRow map[string]interface{}, modelFileDataID uint32, geosetRows []map[string]interface{}) *CreatureDisplayResult {
	return &CreatureDisplayResult{
		DisplayID:       rowUint32(displayRow, "ID"),
		ModelID:         rowUint32(displayRow, "ModelID"),
		FileDataID:      modelFileDataID,
		ModelFileDataID: modelFileDataID,
		Textures:        rowUint32Slice(displayRow, "TextureVariationFileDataID"),
		Scale:           rowFloat32(displayRow, "CreatureModelScale"),
		Variations:      creatureVariations(geosetRows),
	}
}

func creatureVariations(rows []map[string]interface{}) []int {
	out := make([]int, 0, len(rows))
	for _, row := range rows {
		out = append(out, int((rowUint32(row, "GeosetIndex")+1)*100+rowUint32(row, "GeosetValue")))
	}
	return out
}

func rowFloat32(row map[string]interface{}, key string) float32 {
	if row == nil {
		return 0
	}
	switch value := row[key].(type) {
	case float32:
		return value
	case float64:
		return float32(value)
	case int:
		return float32(value)
	case int32:
		return float32(value)
	case uint32:
		return float32(value)
	default:
		return 0
	}
}
