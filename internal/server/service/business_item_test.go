package service

import (
	"context"
	"testing"
)

func TestItemAssemblerUsesBoundedQueries(t *testing.T) {
	ctx := context.Background()
	fake := newBoundedBusinessQueryService(map[string][]map[string]interface{}{
		"Item":       {{"ID": uint32(19019), "ClassID": int32(2), "SubclassID": int32(7)}},
		"ItemSparse": {{"ID": uint32(19019), "Display_lang": "Thunderfury", "InventoryType": int32(13), "OverallQualityID": int32(5)}},
	})
	fake.requireBounded("Item", "ItemSparse")

	got, err := ItemInfo(ctx, fake, ItemInfoRequest{Context: RequestContext{Region: "US"}, ItemID: 19019})
	if err != nil {
		t.Fatalf("ItemInfo error = %v", err)
	}

	for _, want := range []businessQueryCall{
		{table: "Item", idField: "ID"},
		{table: "ItemSparse", idField: "ID"},
	} {
		if !fake.sawBoundedCall(want.table, want.idField) {
			t.Fatalf("missing bounded %s query by %s; calls: %#v", want.table, want.idField, fake.calls)
		}
	}
	if got.ID != 19019 || got.Name != "Thunderfury" || got.ClassID != 2 || got.SubclassID != 7 || got.Quality != 5 {
		t.Fatalf("ItemInfo = %#v, want merged item summary", got)
	}
}
