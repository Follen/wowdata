package wowdata

import "sort"

type DecorItem struct {
	ID                  uint32 `json:"id"`
	Name                string `json:"name,omitempty"`
	ModelFileDataID     uint32 `json:"modelFileDataID,omitempty"`
	ThumbnailFileDataID uint32 `json:"thumbnailFileDataID,omitempty"`
	ItemID              uint32 `json:"itemID,omitempty"`
	GameObjectID        uint32 `json:"gameObjectID,omitempty"`
	Type                int    `json:"type"`
	ModelType           int    `json:"modelType"`
}

type DecorService struct {
	items   map[uint32]DecorItem
	byModel map[uint32]DecorItem
	db2     rowStore
	loaded  bool
}

func NewDecorService() *DecorService {
	return &DecorService{
		items:   make(map[uint32]DecorItem),
		byModel: make(map[uint32]DecorItem),
	}
}

func NewDecorServiceWithDB2(store rowStore) *DecorService {
	svc := NewDecorService()
	svc.db2 = store
	return svc
}

func (s *DecorService) Ready() bool {
	return s.db2 == nil || isRowStoreReady(s.db2, "HouseDecor")
}

func (s *DecorService) RuntimeReady() bool {
	return isRuntimeReady(s.db2)
}

func (s *DecorService) AddItem(item DecorItem) {
	s.items[item.ID] = item
	if item.ModelFileDataID != 0 {
		s.byModel[item.ModelFileDataID] = item
	}
}

func (s *DecorService) GetByID(id uint32) *DecorItem {
	s.loadFromDB2()
	if item, ok := s.items[id]; ok {
		return &item
	}
	return nil
}

func (s *DecorService) GetByModelFileDataID(fdid uint32) *DecorItem {
	s.loadFromDB2()
	if item, ok := s.byModel[fdid]; ok {
		return &item
	}
	return nil
}

func (s *DecorService) ListAll() []DecorItem {
	s.loadFromDB2()
	result := make([]DecorItem, 0, len(s.items))
	for _, item := range s.items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID != result[j].ID {
			return result[i].ID < result[j].ID
		}
		if result[i].ItemID != result[j].ItemID {
			return result[i].ItemID < result[j].ItemID
		}
		return result[i].ModelFileDataID < result[j].ModelFileDataID
	})
	return result
}

func (s *DecorService) loadFromDB2() {
	if s.db2 == nil || s.loaded {
		return
	}
	s.loaded = true
	for _, row := range mustRows(s.db2, "HouseDecor") {
		modelFDID := rowUint32(row, "ModelFileDataID")
		if modelFDID == 0 {
			continue
		}
		item := DecorItem{
			ID:                  rowUint32(row, "ID"),
			Name:                rowString(row, "Name_lang"),
			ModelFileDataID:     modelFDID,
			ThumbnailFileDataID: rowUint32(row, "ThumbnailFileDataID"),
			ItemID:              rowUint32(row, "ItemID"),
			GameObjectID:        rowUint32(row, "GameObjectID"),
			Type:                rowInt(row, "Type"),
			ModelType:           rowInt(row, "ModelType"),
		}
		if item.Name == "" {
			item.Name = "Decor " + rowString(row, "ID")
		}
		s.AddItem(item)
	}
}
