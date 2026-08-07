package wowdata

import "testing"

type decorDB2TestStore struct {
	rows map[string][]map[string]interface{}
}

func (s decorDB2TestStore) Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error) {
	return s.rows[table], nil
}

func TestDecorGetByID(t *testing.T) {
	svc := NewDecorService()
	svc.AddItem(DecorItem{ID: 1, Name: "Chair", ModelFileDataID: 100, Type: 1})

	item := svc.GetByID(1)
	if item == nil {
		t.Fatal("decor not found")
	}
	if item.Name != "Chair" {
		t.Fatalf("name = %s", item.Name)
	}
}

func TestDecorGetByModel(t *testing.T) {
	svc := NewDecorService()
	svc.AddItem(DecorItem{ID: 2, ModelFileDataID: 200})

	item := svc.GetByModelFileDataID(200)
	if item == nil {
		t.Fatal("decor not found by model")
	}
	if item.ID != 2 {
		t.Fatalf("id = %d", item.ID)
	}
}

func TestDecorListAll(t *testing.T) {
	svc := NewDecorService()
	svc.AddItem(DecorItem{ID: 20, ItemID: 3, ModelFileDataID: 300})
	svc.AddItem(DecorItem{ID: 1, ItemID: 2, ModelFileDataID: 200})
	svc.AddItem(DecorItem{ID: 10, ItemID: 1, ModelFileDataID: 100})

	all := svc.ListAll()
	if len(all) != 3 {
		t.Fatalf("count = %d", len(all))
	}
	want := []uint32{1, 10, 20}
	for i := range want {
		if all[i].ID != want[i] {
			t.Fatalf("ids = [%d %d %d], want %v", all[0].ID, all[1].ID, all[2].ID, want)
		}
	}
}

func TestDecorServiceUsesDB2Rows(t *testing.T) {
	svc := NewDecorServiceWithDB2(decorDB2TestStore{rows: map[string][]map[string]interface{}{
		"HouseDecor": {
			{"ID": uint32(1), "Name_lang": "Chair", "ModelFileDataID": uint32(100), "ThumbnailFileDataID": uint32(101), "ItemID": uint32(200), "GameObjectID": uint32(300), "Type": uint32(4), "ModelType": uint32(5)},
			{"ID": uint32(2), "Name_lang": "No Model", "ModelFileDataID": uint32(0)},
		},
	}})

	all := svc.ListAll()
	if len(all) != 1 || all[0].Name != "Chair" || all[0].ThumbnailFileDataID != 101 || all[0].Type != 4 {
		t.Fatalf("all = %#v", all)
	}
	if item := svc.GetByModelFileDataID(100); item == nil || item.ID != 1 {
		t.Fatalf("item by model = %#v", item)
	}
}
