package wowdata

type CreatureDisplayInfo struct {
	DisplayID       uint32   `json:"displayID"`
	ModelID         uint32   `json:"modelID,omitempty"`
	FileDataID      uint32   `json:"fileDataID,omitempty"`
	ModelFileDataID uint32   `json:"modelFileDataID,omitempty"`
	Textures        []uint32 `json:"textures,omitempty"`
	Scale           float32  `json:"scale,omitempty"`
	Variations      []int    `json:"variations,omitempty"`
}

type CreatureModelInfo struct {
	FileDataID uint32 `json:"fileDataID"`
	RaceID     int    `json:"raceID,omitempty"`
	Gender     int    `json:"gender,omitempty"`
}

type CreatureService struct {
	displays      map[uint32][]CreatureDisplayInfo
	fileDataIDMap map[uint32]uint32 // displayID -> modelFileDataID
	models        map[uint32]CreatureModelInfo
	legacyData    map[string][]CreatureLegacyInfo
	db2           rowStore
	loaded        bool
}

type CreatureLegacyInfo struct {
	ID        uint32   `json:"id"`
	Textures  []string `json:"textures"`
	ModelPath string   `json:"modelPath,omitempty"`
}

func NewCreatureService() *CreatureService {
	return &CreatureService{
		displays:      make(map[uint32][]CreatureDisplayInfo),
		fileDataIDMap: make(map[uint32]uint32),
		models:        make(map[uint32]CreatureModelInfo),
		legacyData:    make(map[string][]CreatureLegacyInfo),
	}
}

func NewCreatureServiceWithDB2(store rowStore) *CreatureService {
	svc := NewCreatureService()
	svc.db2 = store
	return svc
}

func (s *CreatureService) Ready() bool {
	return s.db2 == nil || isRowStoreReady(s.db2, "CreatureDisplayInfo", "CreatureModelData")
}

func (s *CreatureService) RuntimeReady() bool {
	return isRuntimeReady(s.db2)
}

func (s *CreatureService) AddDisplay(displayID uint32, info CreatureDisplayInfo) {
	if info.ModelFileDataID == 0 && info.FileDataID != 0 {
		info.ModelFileDataID = info.FileDataID
	}
	if info.FileDataID == 0 && info.ModelFileDataID != 0 {
		info.FileDataID = info.ModelFileDataID
	}
	s.displays[displayID] = append(s.displays[displayID], info)
	if info.ModelFileDataID != 0 {
		s.fileDataIDMap[displayID] = info.ModelFileDataID
	}
}

func (s *CreatureService) AddModel(fdid uint32, info CreatureModelInfo) {
	s.models[fdid] = info
}

func (s *CreatureService) GetCreatureDisplaysByFileDataID(fdid uint32) []CreatureDisplayInfo {
	s.loadFromDB2()
	return s.displays[fdid]
}

func (s *CreatureService) GetFileDataIDByDisplayID(displayID uint32) uint32 {
	s.loadFromDB2()
	return s.fileDataIDMap[displayID]
}

func (s *CreatureService) GetDisplayByID(displayID uint32) *CreatureDisplayInfo {
	s.loadFromDB2()
	if fdid := s.fileDataIDMap[displayID]; fdid != 0 {
		for _, info := range s.displays[fdid] {
			if info.DisplayID == displayID {
				cp := info
				return &cp
			}
		}
	}
	infos, ok := s.displays[displayID]
	if !ok || len(infos) == 0 {
		return nil
	}
	cp := infos[0]
	return &cp
}

func (s *CreatureService) GetDisplayByFileDataID(fdid uint32) *CreatureDisplayInfo {
	s.loadFromDB2()
	infos := s.displays[fdid]
	if len(infos) == 0 {
		return nil
	}
	cp := infos[0]
	return &cp
}

func (s *CreatureService) AddLegacyEntry(modelPath string, info CreatureLegacyInfo) {
	s.legacyData[modelPath] = append(s.legacyData[modelPath], info)
}

func (s *CreatureService) GetLegacyByPath(modelPath string) []CreatureLegacyInfo {
	return s.legacyData[modelPath]
}

func (s *CreatureService) loadFromDB2() {
	if s.db2 == nil || s.loaded {
		return
	}
	s.loaded = true

	geosets := map[uint32][]int{}
	for _, row := range mustRows(s.db2, "CreatureDisplayInfoGeosetData") {
		displayID := rowUint32(row, "CreatureDisplayInfoID")
		geosets[displayID] = append(geosets[displayID], int((rowUint32(row, "GeosetIndex")+1)*100+rowUint32(row, "GeosetValue")))
	}

	displaysByModel := map[uint32][]CreatureDisplayInfo{}
	for _, row := range mustRows(s.db2, "CreatureDisplayInfo") {
		displayID := rowUint32(row, "ID")
		modelID := rowUint32(row, "ModelID")
		if displayID == 0 || modelID == 0 {
			continue
		}
		displaysByModel[modelID] = append(displaysByModel[modelID], CreatureDisplayInfo{
			DisplayID:  displayID,
			ModelID:    modelID,
			Textures:   rowUint32Slice(row, "TextureVariationFileDataID"),
			Variations: geosets[displayID],
		})
	}

	for _, row := range mustRows(s.db2, "CreatureModelData") {
		modelID := rowUint32(row, "ID")
		fdid := rowUint32(row, "FileDataID")
		if modelID == 0 || fdid == 0 {
			continue
		}
		for _, display := range displaysByModel[modelID] {
			display.ModelFileDataID = fdid
			display.FileDataID = fdid
			s.AddDisplay(fdid, display)
			s.fileDataIDMap[display.DisplayID] = fdid
		}
	}
}
