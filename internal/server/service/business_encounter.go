package service

import (
	"context"
	"sort"
)

type EncounterInfoRequest struct {
	Context            RequestContext
	JournalEncounterID uint32
}

type EncounterSectionResult struct {
	ID                        uint32                    `json:"id"`
	ParentID                  uint32                    `json:"-"`
	Title                     string                    `json:"title,omitempty"`
	BodyText                  string                    `json:"bodyText"`
	SpellID                   uint32                    `json:"spellID"`
	IconFlags                 int32                     `json:"iconFlags"`
	Type                      uint32                    `json:"type"`
	DifficultyMask            int32                     `json:"difficultyMask"`
	IconCreatureDisplayInfoID uint32                    `json:"iconCreatureDisplayInfoID"`
	OrderIndex                uint32                    `json:"orderIndex"`
	ParentSectionID           uint32                    `json:"parentSectionID"`
	FirstChildSectionID       uint32                    `json:"firstChildSectionID"`
	NextSiblingSectionID      uint32                    `json:"nextSiblingSectionID"`
	Children                  []*EncounterSectionResult `json:"children,omitempty"`
	SpellIDs                  []uint32                  `json:"spellIDs,omitempty"`
}

type EncounterInfoResult struct {
	JournalEncounterID uint32                    `json:"journalEncounterID"`
	SectionCount       int                       `json:"sectionCount"`
	SpellCount         int                       `json:"spellCount"`
	SpellIDs           []uint32                  `json:"spellIds"`
	Sections           []*EncounterSectionResult `json:"sections"`
}

func EncounterInfo(ctx context.Context, query QueryService, req EncounterInfoRequest) (*EncounterInfoResult, error) {
	rows, err := rowsByUint32ForeignKey(ctx, query, req.Context, "JournalEncounterSection", "JournalEncounterID", []uint32{req.JournalEncounterID})
	if err != nil {
		return nil, err
	}
	sectionsByID := map[uint32]*EncounterSectionResult{}
	for _, row := range rows {
		id := rowUint32(row, "ID")
		if id == 0 {
			continue
		}
		section := &EncounterSectionResult{
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
		if section.SpellID != 0 {
			section.SpellIDs = []uint32{section.SpellID}
		}
		sectionsByID[id] = section
	}

	roots := make([]*EncounterSectionResult, 0)
	for _, section := range sectionsByID {
		if section.ParentID != 0 {
			if parent := sectionsByID[section.ParentID]; parent != nil {
				parent.Children = append(parent.Children, section)
				continue
			}
		}
		roots = append(roots, section)
	}
	sortEncounterSections(roots)
	spellIDs := collectEncounterSpellIDs(roots)
	return &EncounterInfoResult{
		JournalEncounterID: req.JournalEncounterID,
		SectionCount:       len(sectionsByID),
		SpellCount:         len(spellIDs),
		SpellIDs:           spellIDs,
		Sections:           roots,
	}, nil
}

func sortEncounterSections(sections []*EncounterSectionResult) {
	sort.Slice(sections, func(i, j int) bool {
		if sections[i].OrderIndex == sections[j].OrderIndex {
			return sections[i].ID < sections[j].ID
		}
		return sections[i].OrderIndex < sections[j].OrderIndex
	})
	for _, section := range sections {
		sortEncounterSections(section.Children)
	}
}

func collectEncounterSpellIDs(sections []*EncounterSectionResult) []uint32 {
	out := make([]uint32, 0)
	for _, section := range sections {
		out = append(out, section.SpellIDs...)
		out = append(out, collectEncounterSpellIDs(section.Children)...)
	}
	return out
}
