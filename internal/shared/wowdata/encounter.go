package wowdata

import "sort"

type EncounterSection struct {
	ID                        uint32              `json:"id"`
	ParentID                  uint32              `json:"-"`
	Title                     string              `json:"title,omitempty"`
	BodyText                  string              `json:"bodyText"`
	SpellID                   uint32              `json:"spellID"`
	IconFlags                 int32               `json:"iconFlags"`
	Type                      uint32              `json:"type"`
	DifficultyMask            int32               `json:"difficultyMask"`
	IconCreatureDisplayInfoID uint32              `json:"iconCreatureDisplayInfoID"`
	OrderIndex                uint32              `json:"orderIndex"`
	ParentSectionID           uint32              `json:"parentSectionID"`
	FirstChildSectionID       uint32              `json:"firstChildSectionID"`
	NextSiblingSectionID      uint32              `json:"nextSiblingSectionID"`
	Children                  []*EncounterSection `json:"children,omitempty"`
	SpellIDs                  []uint32            `json:"spellIDs,omitempty"`
}

type EncounterResult struct {
	JournalEncounterID uint32              `json:"journalEncounterID"`
	SectionCount       int                 `json:"sectionCount"`
	SpellCount         int                 `json:"spellCount"`
	SpellIDs           []uint32            `json:"spellIds"`
	Sections           []*EncounterSection `json:"sections"`
}

type EncounterService struct {
	sections map[uint32]*EncounterSection
	rootIDs  []uint32
	db2      rowStore
}

func NewEncounterService() *EncounterService {
	return &EncounterService{
		sections: make(map[uint32]*EncounterSection),
	}
}

func NewEncounterServiceWithDB2(store rowStore) *EncounterService {
	svc := NewEncounterService()
	svc.db2 = store
	return svc
}

func (es *EncounterService) Ready() bool {
	return es.db2 == nil || isRowStoreReady(es.db2, "JournalEncounterSection")
}

func (es *EncounterService) RuntimeReady() bool {
	return isRuntimeReady(es.db2)
}

func (es *EncounterService) AddSection(section *EncounterSection) {
	es.sections[section.ID] = section
}

func (es *EncounterService) GetEncounter(encounterID uint32) *EncounterResult {
	es.loadFromDB2(encounterID)
	// Collect sections matching this encounter
	roots := make([]*EncounterSection, 0)
	for _, sec := range es.sections {
		if sec.ParentID == 0 {
			es.buildTree(sec)
			roots = append(roots, sec)
		}
	}

	sort.Slice(roots, func(i, j int) bool {
		if roots[i].OrderIndex == roots[j].OrderIndex {
			return roots[i].ID < roots[j].ID
		}
		return roots[i].OrderIndex < roots[j].OrderIndex
	})
	allSpellIDs := collectEncounterSpellIDs(roots)

	return &EncounterResult{
		JournalEncounterID: encounterID,
		SectionCount:       len(es.sections),
		SpellCount:         len(allSpellIDs),
		SpellIDs:           allSpellIDs,
		Sections:           roots,
	}
}

func (es *EncounterService) loadFromDB2(encounterID uint32) {
	if es.db2 == nil {
		return
	}
	rows, err := es.db2.Rows("JournalEncounterSection", nil, nil, "", 0)
	if err != nil {
		return
	}
	sections := make(map[uint32]*EncounterSection)
	for _, row := range rows {
		if rowUint32(row, "JournalEncounterID") != encounterID {
			continue
		}
		id := rowUint32(row, "ID")
		if id == 0 {
			continue
		}
		sec := &EncounterSection{
			ID:                        id,
			ParentID:                  rowUint32(row, "ParentSectionID"),
			Title:                     rowString(row, "Title_lang"),
			BodyText:                  rowString(row, "BodyText_lang"),
			SpellID:                   rowUint32(row, "SpellID"),
			IconFlags:                 int32(rowInt(row, "IconFlags")),
			Type:                      rowUint32(row, "Type"),
			DifficultyMask:            int32(rowInt(row, "DifficultyMask")),
			IconCreatureDisplayInfoID: rowUint32(row, "IconCreatureDisplayInfoID"),
			OrderIndex:                rowUint32(row, "OrderIndex"),
			ParentSectionID:           rowUint32(row, "ParentSectionID"),
			FirstChildSectionID:       rowUint32(row, "FirstChildSectionID"),
			NextSiblingSectionID:      rowUint32(row, "NextSiblingSectionID"),
		}
		if sec.SpellID != 0 {
			sec.SpellIDs = []uint32{sec.SpellID}
		}
		sections[id] = sec
	}
	es.sections = sections
}

func (es *EncounterService) buildTree(root *EncounterSection) {
	root.Children = nil
	for _, sec := range es.sections {
		if sec.ParentID == root.ID {
			root.Children = append(root.Children, sec)
			es.buildTree(sec)
		}
	}
	sort.Slice(root.Children, func(i, j int) bool {
		if root.Children[i].OrderIndex == root.Children[j].OrderIndex {
			return root.Children[i].ID < root.Children[j].ID
		}
		return root.Children[i].OrderIndex < root.Children[j].OrderIndex
	})
}

func collectEncounterSpellIDs(sections []*EncounterSection) []uint32 {
	out := make([]uint32, 0)
	for _, sec := range sections {
		out = append(out, sec.SpellIDs...)
		out = append(out, collectEncounterSpellIDs(sec.Children)...)
	}
	return out
}
