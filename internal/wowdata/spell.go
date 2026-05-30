package wowdata

type SpellInfo struct {
	SpellID     uint32        `json:"spellID"`
	SeedCount   int           `json:"seedCount"`
	TotalCount  int           `json:"totalCount"`
	ChainDepth  int           `json:"chainDepth"`
	Triggers    []uint32      `json:"triggers"`
	DescRefs    []string      `json:"descRefs"`
	Spells      []SpellNode   `json:"spells"`
}

type SpellNode struct {
	SpellID   uint32 `json:"spellID"`
	Depth     int   `json:"depth"`
	ParentID  uint32 `json:"parentID,omitempty"`
}

type AuraResult struct {
	SpellID uint32 `json:"spellID"`
	HasAura bool   `json:"hasAura"`
	NoAura  bool   `json:"noAura"`
}

type SummonEntry struct {
	SpellID     uint32                 `json:"spellID"`
	NPCID       uint32                 `json:"npcID"`
	EffectIndex int                    `json:"effectIndex"`
	Row         map[string]interface{} `json:"row,omitempty"`
}

type SpellService struct {
	triggers map[uint32][]uint32
	effects  map[uint32][]map[string]interface{}
}

func NewSpellService() *SpellService {
	return &SpellService{
		triggers: make(map[uint32][]uint32),
		effects:  make(map[uint32][]map[string]interface{}),
	}
}

func (s *SpellService) SetTriggers(triggers map[uint32][]uint32) {
	s.triggers = triggers
}

func (s *SpellService) SetEffects(effects map[uint32][]map[string]interface{}) {
	s.effects = effects
}

func (s *SpellService) GetSpellInfo(spellID uint32, maxDepth int) *SpellInfo {
	info := &SpellInfo{
		SpellID:  spellID,
		Triggers: []uint32{},
		DescRefs: []string{},
		Spells:   []SpellNode{},
	}

	visited := make(map[uint32]bool)
	info.ChainDepth = s.traverse(spellID, 0, maxDepth, visited, &info.Spells, &info.Triggers)

	info.SeedCount = 1
	info.TotalCount = len(info.Spells) + 1
	return info
}

func (s *SpellService) traverse(spellID uint32, depth, maxDepth int, visited map[uint32]bool, nodes *[]SpellNode, triggers *[]uint32) int {
	if depth > maxDepth || visited[spellID] {
		return depth
	}
	visited[spellID] = true

	maxChain := depth
	children, ok := s.triggers[spellID]
	if !ok {
		return depth
	}

	for _, childID := range children {
		*triggers = append(*triggers, childID)
		*nodes = append(*nodes, SpellNode{
			SpellID:  childID,
			Depth:    depth + 1,
			ParentID: spellID,
		})
		childDepth := s.traverse(childID, depth+1, maxDepth, visited, nodes, triggers)
		if childDepth > maxChain {
			maxChain = childDepth
		}
	}

	return maxChain
}

func (s *SpellService) DetectAuras(spellID uint32) *AuraResult {
	effects, ok := s.effects[spellID]
	has := ok && len(effects) > 0
	return &AuraResult{
		SpellID: spellID,
		HasAura: has,
		NoAura:  !has,
	}
}

func (s *SpellService) DetectSummons(spellID, npcID uint32) []SummonEntry {
	var results []SummonEntry
	for _, efx := range s.effects[spellID] {
		effectType, _ := efx["Effect"].(uint32)
		miscValueA, _ := efx["EffectMiscValue"].(uint32) // NPC ID in summon effects
		if effectType == 28 { // Summon effect
			entry := SummonEntry{
				SpellID: spellID,
				Row:     efx,
			}
			if effectIdx, ok := efx["EffectIndex"].(uint32); ok {
				entry.EffectIndex = int(effectIdx)
			}
			if miscValueA != 0 {
				entry.NPCID = miscValueA
			}
			if npcID == 0 || entry.NPCID == npcID {
				results = append(results, entry)
			}
		}
	}
	return results
}
