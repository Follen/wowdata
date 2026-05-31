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
	ItemID      uint32   `json:"itemID"`
	RaceID      int      `json:"raceID"`
	Gender      int      `json:"gender"`
	DisplayID   uint32   `json:"displayID,omitempty"`
	Models      []uint32 `json:"models"`
	Textures    []uint32 `json:"textures"`
	GeosetGroup []int    `json:"geosetGroup,omitempty"`
}

type ItemGeosetResult struct {
	ItemID          uint32 `json:"itemID"`
	GeosetGroup     []int  `json:"geosetGroup"`
	HelmetGeosetVis []int  `json:"helmetGeosetVis,omitempty"`
	HelmetHide      []int  `json:"helmetHide,omitempty"`
}

type ItemTextureResult struct {
	ItemID   uint32           `json:"itemID"`
	Sections []TextureSection `json:"sections"`
}

type TextureSection struct {
	Section     int    `json:"section"`
	FileDataID  uint32 `json:"fileDataID"`
	SectionName string `json:"sectionName,omitempty"`
}

type ItemService struct {
	items     map[uint32]ItemSummary
	models    map[uint32]ItemModelResult
	displays  map[uint32]uint32 // itemID -> displayID
	modelData map[uint32]uint32 // displayID -> modelFileDataID
	geosets   map[uint32]ItemGeosetResult
	textures  map[uint32]ItemTextureResult
	db2       rowStore
	loaded    bool
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

func NewItemServiceWithDB2(store rowStore) *ItemService {
	svc := NewItemService()
	svc.db2 = store
	return svc
}

func (s *ItemService) Ready() bool {
	return s.db2 == nil || isRowStoreReady(s.db2, "Item", "ItemSparse")
}

func (s *ItemService) RuntimeReady() bool {
	return isRuntimeReady(s.db2)
}

func (s *ItemService) AddItem(item ItemSummary) {
	if item.InventoryType > 0 {
		item.SlotName = GetSlotName(GetSlotIDForInventoryType(item.InventoryType))
	}
	s.items[item.ID] = item
}

func (s *ItemService) GetItem(id uint32) *ItemSummary {
	s.loadFromDB2()
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
	s.loadFromDB2()
	result := &ItemModelResult{
		ItemID: uint32(itemID),
		RaceID: raceID,
		Gender: gender,
	}
	if model, ok := s.models[uint32(itemID)]; ok {
		model.RaceID = raceID
		model.Gender = gender
		return &model
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
	s.loadFromDB2()
	if geo, ok := s.geosets[itemID]; ok {
		return &geo
	}
	return nil
}

func (s *ItemService) SetItemTextures(itemID uint32, tex ItemTextureResult) {
	s.textures[itemID] = tex
}

func (s *ItemService) GetItemTextures(itemID uint32) *ItemTextureResult {
	s.loadFromDB2()
	if tex, ok := s.textures[itemID]; ok {
		return &tex
	}
	return nil
}

func (s *ItemService) IsItemBow(itemID uint32) bool {
	item := s.GetItem(itemID)
	return item != nil && item.SubclassID == 2 && (item.InventoryType == 15 || item.InventoryType == 26)
}

func (s *ItemService) loadFromDB2() {
	if s.db2 == nil || s.loaded {
		return
	}
	s.loaded = true

	itemClass := map[uint32]ItemSummary{}
	for _, row := range mustRows(s.db2, "Item") {
		id := rowUint32(row, "ID")
		itemClass[id] = ItemSummary{ID: id, ClassID: rowInt(row, "ClassID"), SubclassID: rowInt(row, "SubclassID")}
	}
	for _, row := range mustRows(s.db2, "ItemSparse") {
		id := rowUint32(row, "ID")
		item := itemClass[id]
		item.ID = id
		item.Name = rowString(row, "Display_lang")
		if item.Name == "" {
			item.Name = "Unknown item #" + rowString(row, "ID")
		}
		item.InventoryType = rowInt(row, "InventoryType")
		item.Quality = rowInt(row, "OverallQualityID")
		s.AddItem(item)
	}

	itemToDisplay := s.itemDisplayMap()
	modelByResource := groupFileDataByResource(mustRows(s.db2, "ModelFileData"), "ModelResourcesID")
	textureByMaterial := groupTextureByMaterial(mustRows(s.db2, "TextureFileData"))
	componentInfo := componentModelInfo(mustRows(s.db2, "ComponentModelFileData"))
	displayRows := rowsByID(mustRows(s.db2, "ItemDisplayInfo"))

	for itemID, displayID := range itemToDisplay {
		row, ok := displayRows[displayID]
		if !ok {
			continue
		}
		models := make([]uint32, 0)
		for _, resourceID := range rowUint32Slice(row, "ModelResourcesID") {
			candidates := modelByResource[resourceID]
			if len(candidates) == 0 {
				continue
			}
			models = append(models, chooseComponentModel(candidates, raceGender{RaceID: 0, Gender: 0}, componentInfo))
		}
		textures := make([]uint32, 0)
		for _, materialID := range rowUint32Slice(row, "ModelMaterialResourcesID") {
			if fdids := textureByMaterial[materialID]; len(fdids) > 0 {
				textures = append(textures, fdids[0])
			}
		}
		if len(models) > 0 || len(textures) > 0 {
			s.models[itemID] = ItemModelResult{ItemID: itemID, DisplayID: displayID, Models: models, Textures: textures, GeosetGroup: rowIntSlice(row, "GeosetGroup")}
			s.displays[itemID] = displayID
		}
		if geosetGroup := rowIntSlice(row, "GeosetGroup"); len(geosetGroup) > 0 {
			s.geosets[itemID] = ItemGeosetResult{ItemID: itemID, GeosetGroup: geosetGroup, HelmetGeosetVis: rowIntSlice(row, "HelmetGeosetVis")}
		}
	}

	helmetHide := helmetHideByVis(mustRows(s.db2, "HelmetGeosetData"))
	for itemID, geo := range s.geosets {
		for _, visID := range geo.HelmetGeosetVis {
			if groups := helmetHide[uint32(visID)]; len(groups) > 0 {
				geo.HelmetHide = append(geo.HelmetHide, groups...)
			}
		}
		s.geosets[itemID] = geo
	}

	componentsByDisplay := map[uint32][]TextureSection{}
	for _, row := range mustRows(s.db2, "ItemDisplayInfoMaterialRes") {
		displayID := rowUint32(row, "ItemDisplayInfoID")
		materialID := rowUint32(row, "MaterialResourcesID")
		fdids := textureByMaterial[materialID]
		if len(fdids) == 0 {
			continue
		}
		componentsByDisplay[displayID] = append(componentsByDisplay[displayID], TextureSection{Section: rowInt(row, "ComponentSection"), FileDataID: fdids[0]})
	}
	for itemID, displayID := range itemToDisplay {
		if sections := componentsByDisplay[displayID]; len(sections) > 0 {
			s.textures[itemID] = ItemTextureResult{ItemID: itemID, Sections: sections}
		}
	}
}

func (s *ItemService) itemDisplayMap() map[uint32]uint32 {
	appearanceByItem := map[uint32]uint32{}
	for _, row := range mustRows(s.db2, "ItemModifiedAppearance") {
		appearanceByItem[rowUint32(row, "ItemID")] = rowUint32(row, "ItemAppearanceID")
	}
	displayByAppearance := map[uint32]uint32{}
	for _, row := range mustRows(s.db2, "ItemAppearance") {
		displayByAppearance[rowUint32(row, "ID")] = rowUint32(row, "ItemDisplayInfoID")
	}
	out := map[uint32]uint32{}
	for itemID, appearanceID := range appearanceByItem {
		if displayID := displayByAppearance[appearanceID]; displayID != 0 {
			out[itemID] = displayID
		}
	}
	return out
}
