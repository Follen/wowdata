package wowdata

type DecorItem struct {
	ID                  uint32   `json:"id"`
	Name                string   `json:"name,omitempty"`
	ModelFileDataID     uint32   `json:"modelFileDataID,omitempty"`
	ThumbnailFileDataID uint32   `json:"thumbnailFileDataID,omitempty"`
	ItemID              uint32   `json:"itemID,omitempty"`
	GameObjectID        uint32   `json:"gameObjectID,omitempty"`
	Type                int      `json:"type"`
	ModelType           int      `json:"modelType"`
}

type DecorService struct {
	items  map[uint32]DecorItem
	byModel map[uint32]DecorItem
}

func NewDecorService() *DecorService {
	return &DecorService{
		items:   make(map[uint32]DecorItem),
		byModel: make(map[uint32]DecorItem),
	}
}

func (s *DecorService) AddItem(item DecorItem) {
	s.items[item.ID] = item
	if item.ModelFileDataID != 0 {
		s.byModel[item.ModelFileDataID] = item
	}
}

func (s *DecorService) GetByID(id uint32) *DecorItem {
	if item, ok := s.items[id]; ok {
		return &item
	}
	return nil
}

func (s *DecorService) GetByModelFileDataID(fdid uint32) *DecorItem {
	if item, ok := s.byModel[fdid]; ok {
		return &item
	}
	return nil
}

func (s *DecorService) ListAll() []DecorItem {
	result := make([]DecorItem, 0, len(s.items))
	for _, item := range s.items {
		result = append(result, item)
	}
	return result
}
