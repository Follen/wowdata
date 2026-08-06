package wowdata

import (
	"fmt"
	"regexp"
	"sort"
)

type SpellInfo struct {
	SeedCount  int                          `json:"seedCount"`
	TotalCount int                          `json:"totalCount"`
	ChainDepth int                          `json:"chainDepth"`
	Triggers   map[uint32][]uint32          `json:"triggers"`
	DescRefs   map[uint32][]uint32          `json:"descRefs"`
	Spells     map[uint32]DetailedSpellInfo `json:"spells"`
}

type SpellNode struct {
	SpellID  uint32 `json:"spellID"`
	Depth    int    `json:"depth"`
	ParentID uint32 `json:"parentID,omitempty"`
}

type DetailedSpellInfo struct {
	SpellID         uint32                   `json:"spellId"`
	Name            interface{}              `json:"name"`
	Description     interface{}              `json:"description"`
	AuraDescription interface{}              `json:"auraDescription"`
	IsSeed          bool                     `json:"isSeed"`
	TriggeredBy     []uint32                 `json:"triggeredBy"`
	Misc            interface{}              `json:"misc"`
	Effects         []map[string]interface{} `json:"effects"`
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
	triggers     map[uint32][]uint32
	effects      map[uint32][]map[string]interface{}
	effectLoaded map[uint32]bool
	db2          rowStore
}

func NewSpellService() *SpellService {
	return &SpellService{
		triggers:     make(map[uint32][]uint32),
		effects:      make(map[uint32][]map[string]interface{}),
		effectLoaded: make(map[uint32]bool),
	}
}

func NewSpellServiceWithDB2(store rowStore) *SpellService {
	svc := NewSpellService()
	svc.db2 = store
	return svc
}

func (s *SpellService) Ready() bool {
	return s.db2 == nil || isRowStoreReady(s.db2, "SpellEffect")
}

func (s *SpellService) RuntimeReady() bool {
	return isRuntimeReady(s.db2)
}

func (s *SpellService) SetTriggers(triggers map[uint32][]uint32) {
	s.triggers = triggers
}

func (s *SpellService) SetEffects(effects map[uint32][]map[string]interface{}) {
	s.effects = effects
}

func (s *SpellService) GetSpellInfo(spellID uint32, maxDepth int) *SpellInfo {
	s.loadFromDB2()
	if s.db2 != nil {
		return s.getDetailedSpellInfo([]uint32{spellID}, maxDepth)
	}
	info := &SpellInfo{
		SeedCount: 1,
		Triggers:  map[uint32][]uint32{},
		DescRefs:  map[uint32][]uint32{},
		Spells:    map[uint32]DetailedSpellInfo{},
	}

	allIDs := map[uint32]bool{spellID: true}
	traverseLegacySpellInfo(spellID, 0, maxDepth, s.triggers, allIDs, info.Triggers)
	info.ChainDepth = legacyChainDepth(spellID, 0, maxDepth, s.triggers, map[uint32]bool{})
	info.TotalCount = len(allIDs)
	for id := range allIDs {
		info.Spells[id] = DetailedSpellInfo{SpellID: id, Name: nil, Description: nil, AuraDescription: nil, IsSeed: id == spellID, TriggeredBy: []uint32{}, Misc: nil, Effects: []map[string]interface{}{}}
	}
	addTriggeredBy(info.Spells, info.Triggers)
	return info
}

func (s *SpellService) GetSpellInfoBatch(spellIDs []uint32, maxDepth int) *SpellInfo {
	s.loadFromDB2()
	if s.db2 != nil {
		return s.getDetailedSpellInfo(spellIDs, maxDepth)
	}
	merged := &SpellInfo{Triggers: map[uint32][]uint32{}, DescRefs: map[uint32][]uint32{}, Spells: map[uint32]DetailedSpellInfo{}}
	for _, spellID := range spellIDs {
		info := s.GetSpellInfo(spellID, maxDepth)
		merged.SeedCount++
		if info.ChainDepth > merged.ChainDepth {
			merged.ChainDepth = info.ChainDepth
		}
		for id, spell := range info.Spells {
			merged.Spells[id] = spell
		}
		for id, values := range info.Triggers {
			merged.Triggers[id] = values
		}
		for id, values := range info.DescRefs {
			merged.DescRefs[id] = values
		}
	}
	merged.TotalCount = len(merged.Spells)
	return merged
}

func (s *SpellService) getDetailedSpellInfo(spellIDs []uint32, maxDepth int) *SpellInfo {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	seedSet := map[uint32]bool{}
	allIDs := map[uint32]bool{}
	frontier := append([]uint32(nil), spellIDs...)
	for _, id := range spellIDs {
		seedSet[id] = true
		allIDs[id] = true
	}

	spellRows := map[uint32]map[string]interface{}{}
	spellDescCache := map[uint32]map[string]string{}
	triggers := map[uint32][]uint32{}
	descRefs := map[uint32][]uint32{}
	depth := 0
	refRe := regexp.MustCompile(`\$@spellname(\d+)|\$(\d{5,})[a-zA-Z]`)

	for len(frontier) > 0 && depth < maxDepth {
		s.loadEffects(frontier)
		for id, row := range rowsByID(mustRowsByID(s.db2, "Spell", frontier)) {
			spellRows[id] = row
		}
		next := []uint32{}
		for _, sid := range frontier {
			for _, e := range s.effects[sid] {
				for _, child := range spellEffectChildren(e) {
					if child != 0 && child != sid {
						addRef(triggers, sid, child)
						if !allIDs[child] {
							allIDs[child] = true
							next = append(next, child)
						}
					}
				}
			}
			if row := spellRows[sid]; row != nil {
				desc := rowString(row, "Description_lang")
				aura := rowString(row, "AuraDescription_lang")
				spellDescCache[sid] = map[string]string{"desc": desc, "auraDesc": aura}
				for _, match := range refRe.FindAllStringSubmatch(desc+" "+aura, -1) {
					refID := parseRegexUint32(match[1])
					if refID == 0 {
						refID = parseRegexUint32(match[2])
					}
					if refID != 0 && refID != sid {
						addRef(descRefs, sid, refID)
						if !allIDs[refID] {
							allIDs[refID] = true
							next = append(next, refID)
						}
					}
				}
			}
		}
		frontier = next
		depth++
	}

	allSpellIDs := sortedSpellIDs(allIDs)
	nameRows := rowsByID(mustRowsByID(s.db2, "SpellName", allSpellIDs))
	miscMap := map[uint32]map[string]interface{}{}
	for _, spellID := range allSpellIDs {
		for _, row := range rowsByRelation(s.db2, "SpellMisc", "SpellID", spellID) {
			miscMap[spellID] = row
			break
		}
	}
	castIDs := make([]uint32, 0, len(miscMap))
	durationIDs := make([]uint32, 0, len(miscMap))
	rangeIDs := make([]uint32, 0, len(miscMap))
	for _, row := range miscMap {
		castIDs = appendNonZeroUnique(castIDs, rowUint32(row, "CastingTimeIndex"))
		durationIDs = appendNonZeroUnique(durationIDs, rowUint32(row, "DurationIndex"))
		rangeIDs = appendNonZeroUnique(rangeIDs, rowUint32(row, "RangeIndex"))
	}
	castRows := rowsByID(mustRowsByID(s.db2, "SpellCastTimes", castIDs))
	durationRows := rowsByID(mustRowsByID(s.db2, "SpellDuration", durationIDs))
	rangeRows := rowsByID(mustRowsByID(s.db2, "SpellRange", rangeIDs))

	spells := map[uint32]DetailedSpellInfo{}
	for _, sid := range allSpellIDs {
		if _, ok := spellDescCache[sid]; !ok {
			if row := spellRows[sid]; row != nil {
				spellDescCache[sid] = map[string]string{"desc": rowString(row, "Description_lang"), "auraDesc": rowString(row, "AuraDescription_lang")}
			}
		}
		name := interface{}(nil)
		if row := nameRows[sid]; row != nil {
			if value := rowString(row, "Name_lang"); value != "" {
				name = value
			}
		}
		desc := nullableString(spellDescCache[sid]["desc"])
		auraDesc := nullableString(spellDescCache[sid]["auraDesc"])
		misc := spellMiscPayload(miscMap[sid], castRows, durationRows, rangeRows)
		spells[sid] = DetailedSpellInfo{
			SpellID:         sid,
			Name:            name,
			Description:     desc,
			AuraDescription: auraDesc,
			IsSeed:          seedSet[sid],
			TriggeredBy:     []uint32{},
			Misc:            misc,
			Effects:         spellEffectPayloads(s.effects[sid]),
		}
	}
	addTriggeredBy(spells, triggers)
	addTriggeredBy(spells, descRefs)

	return &SpellInfo{
		SeedCount:  len(spellIDs),
		TotalCount: len(allIDs),
		ChainDepth: depth,
		Triggers:   triggers,
		DescRefs:   descRefs,
		Spells:     spells,
	}
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

func traverseLegacySpellInfo(spellID uint32, depth, maxDepth int, triggers map[uint32][]uint32, allIDs map[uint32]bool, out map[uint32][]uint32) {
	if depth >= maxDepth {
		return
	}
	for _, child := range triggers[spellID] {
		addRef(out, spellID, child)
		if !allIDs[child] {
			allIDs[child] = true
			traverseLegacySpellInfo(child, depth+1, maxDepth, triggers, allIDs, out)
		}
	}
}

func legacyChainDepth(spellID uint32, depth, maxDepth int, triggers map[uint32][]uint32, visited map[uint32]bool) int {
	if depth > maxDepth || visited[spellID] {
		return depth
	}
	visited[spellID] = true
	maxChain := depth
	for _, child := range triggers[spellID] {
		childDepth := legacyChainDepth(child, depth+1, maxDepth, triggers, visited)
		if childDepth > maxChain {
			maxChain = childDepth
		}
	}
	return maxChain
}

func addRef(m map[uint32][]uint32, parent, child uint32) {
	for _, existing := range m[parent] {
		if existing == child {
			return
		}
	}
	m[parent] = append(m[parent], child)
}

func addTriggeredBy(spells map[uint32]DetailedSpellInfo, refs map[uint32][]uint32) {
	for parent, children := range refs {
		for _, child := range children {
			spell, ok := spells[child]
			if !ok {
				continue
			}
			found := false
			for _, existing := range spell.TriggeredBy {
				if existing == parent {
					found = true
					break
				}
			}
			if !found {
				spell.TriggeredBy = append(spell.TriggeredBy, parent)
				spells[child] = spell
			}
		}
	}
}

func spellEffectChildren(row map[string]interface{}) []uint32 {
	children := []uint32{}
	if child := rowUint32(row, "EffectTriggerSpell"); child != 0 {
		children = append(children, child)
	}
	if rowUint32(row, "Effect") == 64 {
		if child := rowUint32(row, "EffectMiscValue"); child != 0 {
			children = append(children, child)
		}
	}
	return children
}

func spellEffectPayloads(rows []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(rows))
	for _, e := range rows {
		out = append(out, map[string]interface{}{
			"EffectIndex":        rowInt(e, "EffectIndex"),
			"Effect":             rowInt(e, "Effect"),
			"EffectAura":         rowInt(e, "EffectAura"),
			"EffectTriggerSpell": nullableUint32(rowUint32(e, "EffectTriggerSpell")),
			"EffectAuraPeriod":   nullableUint32(rowUint32(e, "EffectAuraPeriod")),
			"EffectBasePoints":   rowInt(e, "EffectBasePointsF"),
			"EffectMechanic":     rowInt(e, "EffectMechanic"),
			"ImplicitTarget":     rowIntSlice(e, "ImplicitTarget"),
			"EffectRadiusIndex":  rowIntSlice(e, "EffectRadiusIndex"),
			"EffectMiscValue":    rowIntSlice(e, "EffectMiscValue"),
		})
	}
	return out
}

func spellMiscPayload(misc map[string]interface{}, castRows, durationRows, rangeRows map[uint32]map[string]interface{}) interface{} {
	if misc == nil {
		return nil
	}
	return map[string]interface{}{
		"Attributes":          rowIntSlice(misc, "Attributes"),
		"SchoolMask":          rowInt(misc, "SchoolMask"),
		"Speed":               rowInt(misc, "Speed"),
		"SpellIconFileDataID": rowInt(misc, "SpellIconFileDataID"),
		"castTime":            castTimePayload(castRows[rowUint32(misc, "CastingTimeIndex")]),
		"duration":            durationPayload(durationRows[rowUint32(misc, "DurationIndex")]),
		"range":               rangePayload(rangeRows[rowUint32(misc, "RangeIndex")]),
	}
}

func castTimePayload(row map[string]interface{}) interface{} {
	if row == nil {
		return nil
	}
	return map[string]interface{}{"Base": rowInt(row, "Base"), "Minimum": rowInt(row, "Minimum")}
}

func durationPayload(row map[string]interface{}) interface{} {
	if row == nil {
		return nil
	}
	return map[string]interface{}{"Duration": rowInt(row, "Duration"), "MaxDuration": rowInt(row, "MaxDuration")}
}

func rangePayload(row map[string]interface{}) interface{} {
	if row == nil {
		return nil
	}
	return map[string]interface{}{"DisplayName": rowString(row, "DisplayName_lang"), "RangeMin": rowIntSlice(row, "RangeMin"), "RangeMax": rowIntSlice(row, "RangeMax")}
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func nullableUint32(value uint32) interface{} {
	if value == 0 {
		return nil
	}
	return value
}

func parseRegexUint32(value string) uint32 {
	if value == "" {
		return 0
	}
	var out uint32
	_, _ = fmt.Sscanf(value, "%d", &out)
	return out
}

func sortedSpellIDs(ids map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *SpellService) DetectAuras(spellID uint32) *AuraResult {
	s.loadFromDB2()
	s.loadEffects([]uint32{spellID})
	effects, ok := s.effects[spellID]
	has := false
	if ok {
		for _, efx := range effects {
			if rowUint32(efx, "Effect") == 6 {
				has = true
				break
			}
		}
	}
	return &AuraResult{
		SpellID: spellID,
		HasAura: has,
		NoAura:  !has,
	}
}

func (s *SpellService) DetectSummons(spellID, npcID uint32) []SummonEntry {
	s.loadFromDB2()
	s.loadEffects([]uint32{spellID})
	var results []SummonEntry
	for _, efx := range s.effects[spellID] {
		effectType := rowUint32(efx, "Effect")
		miscValueA := rowUint32(efx, "EffectMiscValue") // NPC ID in summon effects
		if effectType == 28 {                           // Summon effect
			entry := SummonEntry{
				SpellID: spellID,
				Row:     efx,
			}
			entry.EffectIndex = int(rowUint32(efx, "EffectIndex"))
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

func (s *SpellService) loadFromDB2() {
	if s.db2 == nil {
		return
	}
	if s.triggers == nil {
		s.triggers = make(map[uint32][]uint32)
	}
	if s.effects == nil {
		s.effects = make(map[uint32][]map[string]interface{})
	}
	if s.effectLoaded == nil {
		s.effectLoaded = make(map[uint32]bool)
	}
}

func (s *SpellService) loadEffects(spellIDs []uint32) {
	if s.db2 == nil {
		return
	}
	s.loadFromDB2()
	for _, spellID := range spellIDs {
		if s.effectLoaded[spellID] {
			continue
		}
		rows := rowsByRelation(s.db2, "SpellEffect", "SpellID", spellID)
		s.effects[spellID] = rows
		for _, row := range rows {
			childID := rowUint32(row, "EffectTriggerSpell")
			if childID != 0 {
				addRef(s.triggers, spellID, childID)
			}
			if rowUint32(row, "Effect") == 64 {
				if childID := rowUint32(row, "EffectMiscValue"); childID != 0 {
					addRef(s.triggers, spellID, childID)
				}
			}
		}
		s.effectLoaded[spellID] = true
	}
}

func appendNonZeroUnique(values []uint32, value uint32) []uint32 {
	if value == 0 {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
