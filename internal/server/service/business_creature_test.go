package service

import (
	"context"
	"testing"
)

func TestCreatureAssemblerUsesBoundedQueries(t *testing.T) {
	ctx := context.Background()
	fake := newBoundedBusinessQueryService(map[string][]map[string]interface{}{
		"CreatureDisplayInfo":           {{"ID": uint32(100), "ModelID": uint32(10), "TextureVariationFileDataID": []uint32{6000, 6001}}},
		"CreatureModelData":             {{"ID": uint32(10), "FileDataID": uint32(5000)}},
		"CreatureDisplayInfoGeosetData": {{"CreatureDisplayInfoID": uint32(100), "GeosetIndex": uint32(2), "GeosetValue": uint32(7)}},
	})
	fake.requireBounded("CreatureDisplayInfo", "CreatureModelData", "CreatureDisplayInfoGeosetData")

	got, err := CreatureDisplay(ctx, fake, CreatureDisplayRequest{Context: RequestContext{Region: "US"}, DisplayID: 100})
	if err != nil {
		t.Fatalf("CreatureDisplay error = %v", err)
	}

	fake.assertExactBoundedCalls(t, []businessQueryCall{
		{table: "CreatureDisplayInfo", idField: "ID"},
		{table: "CreatureDisplayInfoGeosetData", idField: "CreatureDisplayInfoID"},
		{table: "CreatureModelData", idField: "ID"},
	})
	if got.DisplayID != 100 || got.ModelID != 10 || got.FileDataID != 5000 || got.ModelFileDataID != 5000 {
		t.Fatalf("CreatureDisplay = %#v, want display/model data", got)
	}
	if diff := diffUint32s(got.Textures, []uint32{6000, 6001}); diff != "" {
		t.Fatalf("Textures %s", diff)
	}
	if len(got.Variations) != 1 || got.Variations[0] != 307 {
		t.Fatalf("Variations = %#v, want [307]", got.Variations)
	}
}

func TestCreatureAssemblerLooksUpDisplayByModelIDFromFileDataID(t *testing.T) {
	ctx := context.Background()
	fake := newBoundedBusinessQueryService(map[string][]map[string]interface{}{
		"CreatureModelData":             {{"ID": uint32(10), "FileDataID": uint32(5000)}},
		"CreatureDisplayInfo":           {{"ID": uint32(100), "ModelID": uint32(10), "TextureVariationFileDataID": []uint32{6000}}},
		"CreatureDisplayInfoGeosetData": {{"CreatureDisplayInfoID": uint32(100), "GeosetIndex": uint32(0), "GeosetValue": uint32(4)}},
	})
	fake.requireBounded("CreatureDisplayInfo", "CreatureModelData", "CreatureDisplayInfoGeosetData")

	got, err := CreatureDisplay(ctx, fake, CreatureDisplayRequest{Context: RequestContext{Region: "US"}, FileDataID: 5000})
	if err != nil {
		t.Fatalf("CreatureDisplay error = %v", err)
	}

	fake.assertExactBoundedCalls(t, []businessQueryCall{
		{table: "CreatureModelData", idField: "FileDataID"},
		{table: "CreatureDisplayInfo", idField: "ModelID"},
		{table: "CreatureDisplayInfoGeosetData", idField: "CreatureDisplayInfoID"},
	})
	if got == nil {
		t.Fatal("CreatureDisplay = nil, want display resolved through CreatureModelData.ID")
	}
	if got.DisplayID != 100 || got.ModelID != 10 || got.FileDataID != 5000 {
		t.Fatalf("CreatureDisplay = %#v, want display 100 model 10 fileDataID 5000", got)
	}
}
