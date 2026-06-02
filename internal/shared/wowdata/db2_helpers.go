package wowdata

type raceGender struct {
	RaceID int
	Gender int
}

type componentInfoEntry struct {
	RaceID        int
	Gender        int
	PositionIndex int
}

func mustRows(store rowStore, table string) []map[string]interface{} {
	rows, err := store.Rows(table, nil, nil, "", 0)
	if err != nil {
		return nil
	}
	return rows
}

func rowsByID(rows []map[string]interface{}) map[uint32]map[string]interface{} {
	out := map[uint32]map[string]interface{}{}
	for _, row := range rows {
		if id := rowUint32(row, "ID"); id != 0 {
			out[id] = row
		}
	}
	return out
}

func groupFileDataByResource(rows []map[string]interface{}, resourceField string) map[uint32][]uint32 {
	out := map[uint32][]uint32{}
	for _, row := range rows {
		id := rowUint32(row, "FileDataID")
		if id == 0 {
			id = rowUint32(row, "ID")
		}
		resourceID := rowUint32(row, resourceField)
		if id != 0 && resourceID != 0 {
			out[resourceID] = append(out[resourceID], id)
		}
	}
	return out
}

func groupTextureByMaterial(rows []map[string]interface{}) map[uint32][]uint32 {
	out := map[uint32][]uint32{}
	for _, row := range rows {
		if rowUint32(row, "UsageType") != 0 {
			continue
		}
		id := rowUint32(row, "FileDataID")
		if id == 0 {
			id = rowUint32(row, "ID")
		}
		materialID := rowUint32(row, "MaterialResourcesID")
		if id != 0 && materialID != 0 {
			out[materialID] = append(out[materialID], id)
		}
	}
	return out
}

func componentModelInfo(rows []map[string]interface{}) map[uint32]componentInfoEntry {
	out := map[uint32]componentInfoEntry{}
	for _, row := range rows {
		if id := rowUint32(row, "ID"); id != 0 {
			out[id] = componentInfoEntry{RaceID: rowInt(row, "RaceID"), Gender: rowInt(row, "GenderIndex"), PositionIndex: rowInt(row, "PositionIndex")}
		}
	}
	return out
}

func chooseComponentModel(candidates []uint32, want raceGender, infos map[uint32]componentInfoEntry) uint32 {
	if len(candidates) == 0 {
		return 0
	}
	if want.RaceID == 0 && want.Gender == 0 {
		return candidates[0]
	}
	for _, fdid := range candidates {
		info, ok := infos[fdid]
		if ok && info.RaceID == want.RaceID && info.Gender == want.Gender {
			return fdid
		}
	}
	for _, fdid := range candidates {
		info, ok := infos[fdid]
		if ok && info.RaceID == want.RaceID && info.Gender == 2 {
			return fdid
		}
	}
	for _, fdid := range candidates {
		info, ok := infos[fdid]
		if ok && info.RaceID == 0 {
			return fdid
		}
	}
	return candidates[0]
}

func chooseShoulderComponentModels(candidates []uint32, want raceGender, infos map[uint32]componentInfoEntry) []uint32 {
	out := make([]uint32, 0, 2)
	for _, position := range []int{0, 1} {
		if fdid := chooseComponentModelForPosition(candidates, want, infos, position); fdid != 0 {
			out = append(out, fdid)
		}
	}
	return out
}

func chooseComponentModelForPosition(candidates []uint32, want raceGender, infos map[uint32]componentInfoEntry, position int) uint32 {
	positionCandidates := make([]uint32, 0)
	for _, fdid := range candidates {
		info, ok := infos[fdid]
		if ok && info.PositionIndex == position {
			positionCandidates = append(positionCandidates, fdid)
		}
	}
	if len(positionCandidates) == 0 {
		return 0
	}
	return chooseComponentModel(positionCandidates, want, infos)
}

func helmetHideByVis(rows []map[string]interface{}) map[uint32][]int {
	out := map[uint32][]int{}
	for _, row := range rows {
		visID := rowUint32(row, "HelmetGeosetVisDataID")
		hide := rowInt(row, "HideGeosetGroup")
		if visID != 0 {
			out[visID] = append(out[visID], hide)
		}
	}
	return out
}
