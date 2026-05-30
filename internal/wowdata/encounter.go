package wowdata

import "sort"

type EncounterSection struct {
	ID       uint32             `json:"id"`
	ParentID uint32             `json:"parentID,omitempty"`
	Title    string             `json:"title,omitempty"`
	OrderIndex uint32           `json:"orderIndex"`
	Children []*EncounterSection `json:"children,omitempty"`
	SpellIDs []uint32           `json:"spellIDs,omitempty"`
}

type EncounterResult struct {
	JournalEncounterID uint32              `json:"journalEncounterID"`
	SectionCount       int                 `json:"sectionCount"`
	SpellCount         int                 `json:"spellCount"`
	SpellIDs           []uint32            `json:"spellIDs"`
	Sections           []*EncounterSection `json:"sections"`
}

type EncounterService struct {
	sections map[uint32]*EncounterSection
	rootIDs  []uint32
}

func NewEncounterService() *EncounterService {
	return &EncounterService{
		sections: make(map[uint32]*EncounterSection),
	}
}

func (es *EncounterService) AddSection(section *EncounterSection) {
	es.sections[section.ID] = section
}

func (es *EncounterService) GetEncounter(encounterID uint32) *EncounterResult {
	// Collect sections matching this encounter
	var roots []*EncounterSection
	var allSpellIDs []uint32

	for _, sec := range es.sections {
		if sec.ParentID == 0 && sec.ID >= encounterID { // approximate match
			es.buildTree(sec)
			roots = append(roots, sec)
		}
		allSpellIDs = append(allSpellIDs, sec.SpellIDs...)
	}

	sort.Slice(roots, func(i, j int) bool {
		return roots[i].OrderIndex < roots[j].OrderIndex
	})

	return &EncounterResult{
		JournalEncounterID: encounterID,
		SectionCount:       len(es.sections),
		SpellCount:         len(allSpellIDs),
		SpellIDs:           allSpellIDs,
		Sections:           roots,
	}
}

func (es *EncounterService) buildTree(root *EncounterSection) {
	for _, sec := range es.sections {
		if sec.ParentID == root.ID {
			root.Children = append(root.Children, sec)
			es.buildTree(sec)
		}
	}
	sort.Slice(root.Children, func(i, j int) bool {
		return root.Children[i].OrderIndex < root.Children[j].OrderIndex
	})
}
