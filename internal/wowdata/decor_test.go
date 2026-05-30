package wowdata

import "testing"

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
	svc.AddItem(DecorItem{ID: 1})
	svc.AddItem(DecorItem{ID: 2})

	all := svc.ListAll()
	if len(all) != 2 {
		t.Fatalf("count = %d", len(all))
	}
}
