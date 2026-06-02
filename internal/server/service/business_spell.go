package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
)

type SpellInfoRequest struct {
	Context  RequestContext
	SpellIDs []uint32
	MaxDepth int
}

type SpellInfoResult struct {
	SeedCount  int                                `json:"seedCount"`
	TotalCount int                                `json:"totalCount"`
	ChainDepth int                                `json:"chainDepth"`
	Triggers   map[uint32][]uint32                `json:"triggers"`
	DescRefs   map[uint32][]uint32                `json:"descRefs"`
	Spells     map[uint32]DetailedSpellInfoResult `json:"spells"`
}

type DetailedSpellInfoResult struct {
	SpellID         uint32                   `json:"spellId"`
	Name            interface{}              `json:"name"`
	Description     interface{}              `json:"description"`
	AuraDescription interface{}              `json:"auraDescription"`
	IsSeed          bool                     `json:"isSeed"`
	TriggeredBy     []uint32                 `json:"triggeredBy"`
	Misc            interface{}              `json:"misc"`
	Effects         []map[string]interface{} `json:"effects"`
}

func SpellInfo(ctx context.Context, query QueryService, req SpellInfoRequest) (*SpellInfoResult, error) {
	if req.MaxDepth <= 0 {
		req.MaxDepth = 5
	}
	seedSet := map[uint32]bool{}
	allIDs := map[uint32]bool{}
	frontier := uniqueUint32s(req.SpellIDs)
	for _, id := range frontier {
		if id == 0 {
			continue
		}
		seedSet[id] = true
		allIDs[id] = true
	}

	triggers := map[uint32][]uint32{}
	descRefs := map[uint32][]uint32{}
	effectsBySpell := map[uint32][]map[string]interface{}{}
	spellRows := map[uint32]map[string]interface{}{}
	refRe := regexp.MustCompile(`\$@spellname(\d+)|\$(\d{5,})[a-zA-Z]`)

	depth := 0
	for len(frontier) > 0 && depth < req.MaxDepth {
		effectRows, err := rowsByUint32ForeignKey(ctx, query, req.Context, "SpellEffect", "SpellID", frontier)
		if err != nil {
			return nil, err
		}
		currentSpellRows, err := rowsByUint32IDs(ctx, query, req.Context, "Spell", "ID", frontier)
		if err != nil {
			return nil, err
		}
		for id, row := range currentSpellRows {
			spellRows[id] = row
		}

		nextSet := map[uint32]bool{}
		for _, row := range effectRows {
			parent := rowUint32(row, "SpellID")
			if parent == 0 {
				continue
			}
			effectsBySpell[parent] = append(effectsBySpell[parent], row)
			for _, child := range spellEffectChildren(row) {
				if child == 0 || child == parent {
					continue
				}
				addRef(triggers, parent, child)
				if !allIDs[child] {
					allIDs[child] = true
					nextSet[child] = true
				}
			}
		}
		for _, parent := range frontier {
			row := spellRows[parent]
			if row == nil {
				continue
			}
			text := rowString(row, "Description_lang") + " " + rowString(row, "AuraDescription_lang")
			for _, match := range refRe.FindAllStringSubmatch(text, -1) {
				child := parseRegexUint32(match[1])
				if child == 0 {
					child = parseRegexUint32(match[2])
				}
				if child == 0 || child == parent {
					continue
				}
				addRef(descRefs, parent, child)
				if !allIDs[child] {
					allIDs[child] = true
					nextSet[child] = true
				}
			}
		}
		frontier = mapKeysSorted(nextSet)
		depth++
	}

	allSpellIDs := mapKeysSorted(allIDs)
	if len(allSpellIDs) > 0 {
		rows, err := rowsByUint32IDs(ctx, query, req.Context, "Spell", "ID", allSpellIDs)
		if err != nil {
			return nil, err
		}
		for id, row := range rows {
			spellRows[id] = row
		}
	}
	nameRows, err := rowsByUint32IDs(ctx, query, req.Context, "SpellName", "ID", allSpellIDs)
	if err != nil {
		return nil, err
	}
	miscRows, err := rowsByUint32IDs(ctx, query, req.Context, "SpellMisc", "SpellID", allSpellIDs)
	if err != nil {
		return nil, err
	}
	castRows, durationRows, rangeRows, err := spellLookupRows(ctx, query, req.Context, miscRows)
	if err != nil {
		return nil, err
	}

	spells := map[uint32]DetailedSpellInfoResult{}
	for _, spellID := range allSpellIDs {
		name := interface{}(nil)
		if value := rowString(nameRows[spellID], "Name_lang"); value != "" {
			name = value
		}
		row := spellRows[spellID]
		spells[spellID] = DetailedSpellInfoResult{
			SpellID:         spellID,
			Name:            name,
			Description:     nullableString(rowString(row, "Description_lang")),
			AuraDescription: nullableString(rowString(row, "AuraDescription_lang")),
			IsSeed:          seedSet[spellID],
			TriggeredBy:     []uint32{},
			Misc:            spellMiscPayload(miscRows[spellID], castRows, durationRows, rangeRows),
			Effects:         spellEffectPayloads(effectsBySpell[spellID]),
		}
	}
	addTriggeredBy(spells, triggers)
	addTriggeredBy(spells, descRefs)

	return &SpellInfoResult{
		SeedCount:  len(seedSet),
		TotalCount: len(allIDs),
		ChainDepth: depth,
		Triggers:   triggers,
		DescRefs:   descRefs,
		Spells:     spells,
	}, nil
}

func spellLookupRows(ctx context.Context, query QueryService, rc RequestContext, miscRows map[uint32]map[string]interface{}) (map[uint32]map[string]interface{}, map[uint32]map[string]interface{}, map[uint32]map[string]interface{}, error) {
	castIDs := map[uint32]bool{}
	durationIDs := map[uint32]bool{}
	rangeIDs := map[uint32]bool{}
	for _, row := range miscRows {
		if id := rowUint32(row, "CastingTimeIndex"); id != 0 {
			castIDs[id] = true
		}
		if id := rowUint32(row, "DurationIndex"); id != 0 {
			durationIDs[id] = true
		}
		if id := rowUint32(row, "RangeIndex"); id != 0 {
			rangeIDs[id] = true
		}
	}
	castRows, err := rowsByUint32IDs(ctx, query, rc, "SpellCastTimes", "ID", mapKeysSorted(castIDs))
	if err != nil {
		return nil, nil, nil, err
	}
	durationRows, err := rowsByUint32IDs(ctx, query, rc, "SpellDuration", "ID", mapKeysSorted(durationIDs))
	if err != nil {
		return nil, nil, nil, err
	}
	rangeRows, err := rowsByUint32IDs(ctx, query, rc, "SpellRange", "ID", mapKeysSorted(rangeIDs))
	if err != nil {
		return nil, nil, nil, err
	}
	return castRows, durationRows, rangeRows, nil
}

func rowsByUint32IDs(ctx context.Context, query QueryService, rc RequestContext, table, idField string, ids []uint32) (map[uint32]map[string]interface{}, error) {
	boundedIDs := uint32sToUint64s(uniqueUint32s(ids))
	if len(boundedIDs) == 0 {
		return map[uint32]map[string]interface{}{}, nil
	}
	rows, err := query.Rows(ctx, QueryRowsRequest{
		Context: rc,
		Table:   table,
		IDs:     boundedIDs,
		IDField: idField,
	})
	if err != nil {
		return nil, err
	}
	out := map[uint32]map[string]interface{}{}
	for _, row := range rows {
		if id := rowUint32(row, idField); id != 0 {
			out[id] = row
		}
	}
	return out, nil
}

func rowsByUint32ForeignKey(ctx context.Context, query QueryService, rc RequestContext, table, idField string, ids []uint32) ([]map[string]interface{}, error) {
	boundedIDs := uint32sToUint64s(uniqueUint32s(ids))
	if len(boundedIDs) == 0 {
		return []map[string]interface{}{}, nil
	}
	return query.Rows(ctx, QueryRowsRequest{
		Context: rc,
		Table:   table,
		IDs:     boundedIDs,
		IDField: idField,
	})
}

func uint32sToUint64s(ids []uint32) []uint64 {
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id != 0 {
			out = append(out, uint64(id))
		}
	}
	return out
}

func uniqueUint32s(ids []uint32) []uint32 {
	seen := map[uint32]bool{}
	out := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func mapKeysSorted(m map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(m))
	for id := range m {
		if id != 0 {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func addRef(m map[uint32][]uint32, parent, child uint32) {
	for _, existing := range m[parent] {
		if existing == child {
			return
		}
	}
	m[parent] = append(m[parent], child)
}

func addTriggeredBy(spells map[uint32]DetailedSpellInfoResult, refs map[uint32][]uint32) {
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
		for _, child := range rowUint32Slice(row, "EffectMiscValue") {
			if child != 0 {
				children = append(children, child)
			}
		}
		if child := rowUint32(row, "EffectMiscValue"); child != 0 {
			children = append(children, child)
		}
	}
	return uniqueUint32s(children)
}

func spellEffectPayloads(rows []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]interface{}{
			"EffectIndex":        rowInt(row, "EffectIndex"),
			"Effect":             rowInt(row, "Effect"),
			"EffectAura":         rowInt(row, "EffectAura"),
			"EffectTriggerSpell": nullableUint32(rowUint32(row, "EffectTriggerSpell")),
			"EffectAuraPeriod":   nullableUint32(rowUint32(row, "EffectAuraPeriod")),
			"EffectBasePoints":   rowInt(row, "EffectBasePointsF"),
			"EffectMechanic":     rowInt(row, "EffectMechanic"),
			"ImplicitTarget":     rowIntSlice(row, "ImplicitTarget"),
			"EffectRadiusIndex":  rowIntSlice(row, "EffectRadiusIndex"),
			"EffectMiscValue":    rowIntSlice(row, "EffectMiscValue"),
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

func rowString(row map[string]interface{}, key string) string {
	if row == nil {
		return ""
	}
	switch value := row[key].(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func rowUint32(row map[string]interface{}, key string) uint32 {
	if row == nil {
		return 0
	}
	switch value := row[key].(type) {
	case uint32:
		return value
	case uint64:
		return uint32(value)
	case uint:
		return uint32(value)
	case int:
		if value >= 0 {
			return uint32(value)
		}
	case int32:
		if value >= 0 {
			return uint32(value)
		}
	case int64:
		if value >= 0 {
			return uint32(value)
		}
	case float64:
		if value >= 0 {
			return uint32(value)
		}
	case float32:
		if value >= 0 {
			return uint32(value)
		}
	}
	return 0
}

func rowUint32Slice(row map[string]interface{}, key string) []uint32 {
	if row == nil {
		return nil
	}
	switch value := row[key].(type) {
	case []uint32:
		return append([]uint32(nil), value...)
	case []uint64:
		out := make([]uint32, 0, len(value))
		for _, v := range value {
			out = append(out, uint32(v))
		}
		return out
	case []int:
		out := make([]uint32, 0, len(value))
		for _, v := range value {
			if v >= 0 {
				out = append(out, uint32(v))
			}
		}
		return out
	case []int32:
		out := make([]uint32, 0, len(value))
		for _, v := range value {
			if v >= 0 {
				out = append(out, uint32(v))
			}
		}
		return out
	case []interface{}:
		out := make([]uint32, 0, len(value))
		for _, v := range value {
			if id := rowScalarUint32(v); id != 0 {
				out = append(out, id)
			}
		}
		return out
	default:
		if id := rowScalarUint32(value); id != 0 {
			return []uint32{id}
		}
	}
	return nil
}

func rowInt(row map[string]interface{}, key string) int {
	if row == nil {
		return 0
	}
	switch value := row[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case uint32:
		return int(value)
	case uint64:
		return int(value)
	case uint:
		return int(value)
	case float32:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func rowIntSlice(row map[string]interface{}, key string) []int {
	if row == nil {
		return nil
	}
	switch value := row[key].(type) {
	case []int:
		return append([]int(nil), value...)
	case []int32:
		out := make([]int, 0, len(value))
		for _, v := range value {
			out = append(out, int(v))
		}
		return out
	case []uint32:
		out := make([]int, 0, len(value))
		for _, v := range value {
			out = append(out, int(v))
		}
		return out
	case []float32:
		out := make([]int, 0, len(value))
		for _, v := range value {
			out = append(out, int(v))
		}
		return out
	case []float64:
		out := make([]int, 0, len(value))
		for _, v := range value {
			out = append(out, int(v))
		}
		return out
	case []interface{}:
		out := make([]int, 0, len(value))
		for _, v := range value {
			out = append(out, int(rowScalarUint32(v)))
		}
		return out
	default:
		return []int{rowInt(row, key)}
	}
}

func rowScalarUint32(value interface{}) uint32 {
	switch v := value.(type) {
	case uint32:
		return v
	case uint64:
		return uint32(v)
	case uint:
		return uint32(v)
	case int:
		if v >= 0 {
			return uint32(v)
		}
	case int32:
		if v >= 0 {
			return uint32(v)
		}
	case int64:
		if v >= 0 {
			return uint32(v)
		}
	case float32:
		if v >= 0 {
			return uint32(v)
		}
	case float64:
		if v >= 0 {
			return uint32(v)
		}
	}
	return 0
}
