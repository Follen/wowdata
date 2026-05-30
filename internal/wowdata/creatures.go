package wowdata

type CreatureDisplayInfo struct {
	DisplayID     uint32   `json:"displayID"`
	ModelFileDataID uint32 `json:"modelFileDataID,omitempty"`
	Textures      []uint32 `json:"textures,omitempty"`
	Scale         float32  `json:"scale,omitempty"`
	Variations    []int    `json:"variations,omitempty"`
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
}

type CreatureLegacyInfo struct {
	ID       uint32   `json:"id"`
	Textures []string `json:"textures"`
	ModelPath string  `json:"modelPath,omitempty"`
}

func NewCreatureService() *CreatureService {
	return &CreatureService{
		displays:      make(map[uint32][]CreatureDisplayInfo),
		fileDataIDMap: make(map[uint32]uint32),
		models:        make(map[uint32]CreatureModelInfo),
		legacyData:    make(map[string][]CreatureLegacyInfo),
	}
}

func (s *CreatureService) AddDisplay(displayID uint32, info CreatureDisplayInfo) {
	s.displays[displayID] = append(s.displays[displayID], info)
	if info.ModelFileDataID != 0 {
		s.fileDataIDMap[displayID] = info.ModelFileDataID
	}
}

func (s *CreatureService) AddModel(fdid uint32, info CreatureModelInfo) {
	s.models[fdid] = info
}

func (s *CreatureService) GetCreatureDisplaysByFileDataID(fdid uint32) []CreatureDisplayInfo {
	return s.displays[fdid]
}

func (s *CreatureService) GetFileDataIDByDisplayID(displayID uint32) uint32 {
	return s.fileDataIDMap[displayID]
}

func (s *CreatureService) GetDisplayByID(displayID uint32) *CreatureDisplayInfo {
	infos, ok := s.displays[displayID]
	if !ok || len(infos) == 0 {
		return nil
	}
	return &infos[0]
}

func (s *CreatureService) AddLegacyEntry(modelPath string, info CreatureLegacyInfo) {
	s.legacyData[modelPath] = append(s.legacyData[modelPath], info)
}

func (s *CreatureService) GetLegacyByPath(modelPath string) []CreatureLegacyInfo {
	return s.legacyData[modelPath]
}
