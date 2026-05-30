package wowdata

type ItemSummary struct {
	ID            uint32 `json:"id"`
	Name          string `json:"name,omitempty"`
	InventoryType int    `json:"inventoryType"`
	ClassID       int    `json:"classID"`
	SubclassID    int    `json:"subclassID"`
	Quality       int    `json:"quality"`
	SlotName      string `json:"slotName,omitempty"`
}

type ItemModelResult struct {
	ItemID   uint32     `json:"itemID"`
	RaceID   int        `json:"raceID"`
	Gender   int        `json:"gender"`
	DisplayID uint32    `json:"displayID,omitempty"`
	Models   []uint32   `json:"models"`
	Textures []uint32   `json:"textures"`
	GeosetGroup []int   `json:"geosetGroup,omitempty"`
}

type ItemGeosetResult struct {
	ItemID          uint32 `json:"itemID"`
	GeosetGroup     []int  `json:"geosetGroup"`
	HelmetGeosetVis []int  `json:"helmetGeosetVis,omitempty"`
	HelmetHide      []int  `json:"helmetHide,omitempty"`
}

type ItemTextureResult struct {
	ItemID   uint32          `json:"itemID"`
	Sections []TextureSection `json:"sections"`
}

type TextureSection struct {
	Section      int    `json:"section"`
	FileDataID   uint32 `json:"fileDataID"`
	SectionName  string `json:"sectionName,omitempty"`
}

type ItemService struct {
	items        map[uint32]ItemSummary
	models       map[uint32]ItemModelResult
	displays     map[uint32]uint32 // itemID -> displayID
	modelData    map[uint32]uint32 // displayID -> modelFileDataID
	geosets      map[uint32]ItemGeosetResult
	textures     map[uint32]ItemTextureResult
}

func NewItemService() *ItemService {
	return &ItemService{
		items:     make(map[uint32]ItemSummary),
		models:    make(map[uint32]ItemModelResult),
		displays:  make(map[uint32]uint32),
		modelData: make(map[uint32]uint32),
		geosets:   make(map[uint32]ItemGeosetResult),
		textures:  make(map[uint32]ItemTextureResult),
	}
}

func (s *ItemService) AddItem(item ItemSummary) {
	if item.InventoryType > 0 {
		item.SlotName = GetSlotName(GetSlotIDForInventoryType(item.InventoryType))
	}
	s.items[item.ID] = item
}

func (s *ItemService) GetItem(id uint32) *ItemSummary {
	if item, ok := s.items[id]; ok {
		cp := item
		cp.SlotName = GetSlotName(GetSlotIDForInventoryType(cp.InventoryType))
		return &cp
	}
	return nil
}

func (s *ItemService) SetItemDisplay(itemID, displayID uint32) {
	s.displays[itemID] = displayID
}

func (s *ItemService) SetDisplayModel(displayID, modelFileDataID uint32) {
	s.modelData[displayID] = modelFileDataID
}

func (s *ItemService) GetItemModels(itemID, raceID, gender int) *ItemModelResult {
	result := &ItemModelResult{
		ItemID:   uint32(itemID),
		RaceID:   raceID,
		Gender:   gender,
	}
	if displayID, ok := s.displays[uint32(itemID)]; ok {
		result.DisplayID = displayID
		if modelFDID, ok := s.modelData[displayID]; ok {
			result.Models = append(result.Models, modelFDID)
		}
	}
	return result
}

func (s *ItemService) SetItemGeoset(itemID uint32, geo ItemGeosetResult) {
	s.geosets[itemID] = geo
}

func (s *ItemService) GetItemGeosets(itemID uint32) *ItemGeosetResult {
	if geo, ok := s.geosets[itemID]; ok {
		return &geo
	}
	return nil
}

func (s *ItemService) SetItemTextures(itemID uint32, tex ItemTextureResult) {
	s.textures[itemID] = tex
}

func (s *ItemService) GetItemTextures(itemID uint32) *ItemTextureResult {
	if tex, ok := s.textures[itemID]; ok {
		return &tex
	}
	return nil
}

func (s *ItemService) IsItemBow(itemID uint32) bool {
	item := s.GetItem(itemID)
	return item != nil && item.SubclassID == 2 && (item.InventoryType == 15 || item.InventoryType == 26)
}
