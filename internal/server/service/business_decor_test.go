package service

import (
	"context"
	"testing"
)

func TestDecorAssemblerUsesBoundedQueries(t *testing.T) {
	ctx := context.Background()
	fake := newBoundedBusinessQueryService(map[string][]map[string]interface{}{
		"HouseDecor": {{"ID": uint32(77), "Name_lang": "Banner", "ModelFileDataID": uint32(888), "ThumbnailFileDataID": uint32(999), "ItemID": uint32(123), "GameObjectID": uint32(456), "Type": int32(2), "ModelType": int32(3)}},
	})
	fake.requireBounded("HouseDecor")

	got, err := DecorItem(ctx, fake, DecorItemRequest{Context: RequestContext{Region: "US"}, ID: 77})
	if err != nil {
		t.Fatalf("DecorItem error = %v", err)
	}

	fake.assertExactBoundedCalls(t, []businessQueryCall{
		{table: "HouseDecor", idField: "ID"},
	})
	if got.ID != 77 || got.Name != "Banner" || got.ModelFileDataID != 888 || got.ThumbnailFileDataID != 999 || got.ItemID != 123 || got.GameObjectID != 456 {
		t.Fatalf("DecorItem = %#v, want decor row payload", got)
	}
}
