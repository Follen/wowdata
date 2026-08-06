package db2

import (
	"context"
	"sort"

	"wowdata/internal/resource"
)

const (
	LookupEncrypted    = "encrypted"
	LookupPositional   = "positional"
	LookupSorted       = "binary-search"
	LookupDense        = "dense-index"
	LookupHash         = "hash-index"
	LookupSparseOffset = "sparse-offset"
	LookupInlineScan   = "inline-scan"
)

// WDCPhysicalProfile describes the point-lookup algorithms selected from the
// parsed file shape. It is diagnostic evidence for corpus gates; query paths do
// not consult this summary and therefore do not pay per-query classification.
type WDCPhysicalProfile struct {
	Strategies     map[string]int `json:"strategies"`
	Sections       int            `json:"sections"`
	IndexedRows    int            `json:"indexedRows"`
	DenseSlots     int            `json:"denseSlots"`
	EstimatedBytes uint64         `json:"estimatedBytes"`
	Unclassified   int            `json:"unclassified"`
}

func (r *WDCReader) PhysicalProfile() WDCPhysicalProfile {
	profile := WDCPhysicalProfile{
		Strategies: make(map[string]int),
		Sections:   len(r.Sections),
	}
	for sectionIndex := range r.Sections {
		section := &r.Sections[sectionIndex]
		strategy := ""
		switch {
		case section.IsEncrypted:
			strategy = LookupEncrypted
		case len(section.IDList) > 0 && section.IDListAllZero && r.hasNonInlineID():
			strategy = LookupPositional
		case len(section.IDList) > 0 && section.IDListAllZero:
			strategy = LookupInlineScan
		case len(section.IDList) > 0 && section.IDListSorted:
			strategy = LookupSorted
		case len(section.IDListDense) > 0:
			strategy = LookupDense
			profile.IndexedRows += len(section.IDList)
			profile.DenseSlots += len(section.IDListDense)
			profile.EstimatedBytes += uint64(len(section.IDListDense)) * 4
		case len(section.IDList) > 0:
			strategy = LookupHash
			profile.IndexedRows += len(section.IDList)
			// Go map storage is implementation-specific. Key, section and row
			// identifiers are counted as three logical uint32 values so corpus
			// reports remain comparable across architectures and Go versions.
			profile.EstimatedBytes += uint64(len(section.IDList)) * 12
		case !section.IsNormal && r.WDCVersion == 2 && len(section.OffsetMap) > 0:
			strategy = LookupSparseOffset
			profile.IndexedRows += len(section.OffsetMap)
			profile.EstimatedBytes += uint64(len(section.OffsetMap)) * 12
		case section.Header.RecordCount > 0:
			strategy = LookupInlineScan
		default:
			// Empty sections have no point-lookup work but still need a stable
			// classification so corpus accounting is exhaustive.
			strategy = LookupPositional
		}
		if strategy == "" {
			profile.Unclassified++
			continue
		}
		profile.Strategies[strategy]++
	}
	return profile
}

func (r *WDCReader) hasNonInlineID() bool {
	for _, field := range r.Schema {
		if field.Type == FieldNonInlineID && (r.IDField == "" || field.Name == r.IDField) {
			return true
		}
	}
	return false
}

type rowLocation struct {
	section int
	record  uint32
}

func (r *WDCReader) buildKnownRowLocations() {
	capacity := 0
	for sectionIndex := range r.Sections {
		section := &r.Sections[sectionIndex]
		entries := 0
		switch {
		case section.IsEncrypted:
			continue
		case len(section.IDList) > 0:
			section.IDListAllZero, section.IDListSorted = classifyIDList(section.IDList)
			if !section.IDListAllZero && !section.IDListSorted {
				section.IDListDenseMin, section.IDListDense = buildDenseIDIndex(section.IDList)
				if len(section.IDListDense) == 0 {
					entries = len(section.IDList)
				}
			}
		case !section.IsNormal && r.WDCVersion == 2:
			entries = len(section.OffsetMap)
		}
		if entries > int(^uint(0)>>1)-capacity {
			capacity = 0
			break
		}
		capacity += entries
	}
	r.rowLocations = make(map[uint32]rowLocation, capacity)
	for sectionIndex := range r.Sections {
		section := &r.Sections[sectionIndex]
		if section.IsEncrypted {
			continue
		}
		if len(section.IDList) > 0 {
			if section.IDListAllZero || section.IDListSorted || len(section.IDListDense) > 0 {
				continue
			}
			for recordIndex := uint32(0); recordIndex < section.Header.RecordCount; recordIndex++ {
				id := section.IDList[recordIndex]
				r.rowLocations[id] = rowLocation{section: sectionIndex, record: recordIndex}
			}
			continue
		}
		if !section.IsNormal && r.WDCVersion == 2 {
			for id, entry := range section.OffsetMap {
				if entry.Offset != 0 && entry.Size != 0 {
					r.rowLocations[id] = rowLocation{section: sectionIndex, record: id}
				}
			}
		}
	}
}

func buildDenseIDIndex(ids []uint32) (uint32, []uint32) {
	if len(ids) == 0 {
		return 0, nil
	}
	minID, maxID := ids[0], ids[0]
	for _, id := range ids[1:] {
		if id < minID {
			minID = id
		}
		if id > maxID {
			maxID = id
		}
	}
	span := uint64(maxID) - uint64(minID) + 1
	const maxDenseIDSlots = 16 << 20
	if span > uint64(len(ids))*4 || span > maxDenseIDSlots {
		return 0, nil
	}
	index := make([]uint32, int(span))
	for recordIndex, id := range ids {
		index[id-minID] = uint32(recordIndex) + 1
	}
	return minID, index
}

func classifyIDList(ids []uint32) (allZero, sorted bool) {
	allZero = true
	sorted = true
	for index, id := range ids {
		if id != 0 {
			allZero = false
		}
		if index > 0 && id <= ids[index-1] {
			sorted = false
		}
	}
	return allZero, sorted
}

func (r *WDCReader) GetRowProjected(recordID uint32, fields []string) map[string]interface{} {
	row := projectFields(r.GetRow(recordID), fields)
	resource.RecordWDCQuery(0, 0, len(row), 0)
	return row
}

func (r *WDCReader) GetRows(ids []uint32, fields []string) []map[string]interface{} {
	rows, _ := r.GetRowsContext(context.Background(), ids, fields)
	return rows
}

func (r *WDCReader) GetRowsContext(ctx context.Context, ids []uint32, fields []string) ([]map[string]interface{}, error) {
	if len(ids) == 0 {
		return nil, ctx.Err()
	}
	// Decode in physical ID order for locality, then restore request order and duplicates.
	unique := make([]uint32, 0, len(ids))
	seen := make(map[uint32]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	decoded := make(map[uint32]map[string]interface{}, len(unique))
	for index, id := range unique {
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if row := r.GetRowProjected(id, fields); row != nil {
			decoded[id] = row
		}
	}
	rows := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		if row := decoded[id]; row != nil {
			rows = append(rows, row)
		}
	}
	return rows, ctx.Err()
}

func (r *WDCReader) Scan(fields []string, filterFn func(map[string]interface{}) bool, limit int) []map[string]interface{} {
	rows, _ := r.ScanContext(context.Background(), fields, filterFn, limit)
	return rows
}

func (r *WDCReader) ScanContext(ctx context.Context, fields []string, filterFn func(map[string]interface{}) bool, limit int) ([]map[string]interface{}, error) {
	rows := make([]map[string]interface{}, 0)
	err := r.StreamRowsContext(ctx, fields, filterFn, limit, func(row map[string]interface{}) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

func (r *WDCReader) StreamRowsContext(ctx context.Context, fields []string, filterFn func(map[string]interface{}) bool, limit int, yield func(map[string]interface{}) error) error {
	if !r.IsLoaded {
		return ctx.Err()
	}
	emitted := 0
	for sectionIndex := range r.Sections {
		section := &r.Sections[sectionIndex]
		if section.IsEncrypted {
			continue
		}
		for recordIndex := uint32(0); recordIndex < section.Header.RecordCount; recordIndex++ {
			if recordIndex&255 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			recordID := r.recordIDAt(section, recordIndex)
			row := r.readRecordFromSection(sectionIndex, recordIndex, recordID)
			resource.RecordWDCQuery(1, 0, 0, 0)
			if row == nil {
				continue
			}
			if filterFn != nil {
				resource.RecordWDCQuery(0, 1, 0, 0)
				if !filterFn(row) {
					continue
				}
			}
			projected := projectFields(row, fields)
			resource.RecordWDCQuery(0, 0, len(projected), 0)
			if err := yield(projected); err != nil {
				return err
			}
			emitted++
			if limit > 0 && emitted >= limit {
				return nil
			}
		}
	}
	copyIDs := make([]uint32, 0, len(r.CopyTable))
	for destination := range r.CopyTable {
		copyIDs = append(copyIDs, destination)
	}
	sort.Slice(copyIDs, func(i, j int) bool { return copyIDs[i] < copyIDs[j] })
	for _, destination := range copyIDs {
		source := r.CopyTable[destination]
		if err := ctx.Err(); err != nil {
			return err
		}
		row := r.GetRow(source)
		if row == nil {
			continue
		}
		copyRow := make(map[string]interface{}, len(row))
		for key, value := range row {
			copyRow[key] = value
		}
		copyRow[r.IDField] = destination
		if filterFn != nil {
			resource.RecordWDCQuery(0, 1, 0, 0)
			if !filterFn(copyRow) {
				continue
			}
		}
		projected := projectFields(copyRow, fields)
		resource.RecordWDCQuery(0, 0, len(projected), 0)
		if err := yield(projected); err != nil {
			return err
		}
		emitted++
		if limit > 0 && emitted >= limit {
			return nil
		}
	}
	return ctx.Err()
}

func (r *WDCReader) recordIDAt(section *Section, recordIndex uint32) uint32 {
	if int(recordIndex) < len(section.IDList) && section.IDList[recordIndex] != 0 {
		return section.IDList[recordIndex]
	}
	return recordIndex
}

func (r *WDCReader) GetRelationshipRowsBatch(fkValues []uint32, fields []string) map[uint32][]map[string]interface{} {
	rows, _ := r.GetRelationshipRowsBatchContext(context.Background(), fkValues, fields)
	return rows
}

func (r *WDCReader) GetRelationshipRowsBatchContext(ctx context.Context, fkValues []uint32, fields []string) (map[uint32][]map[string]interface{}, error) {
	result := make(map[uint32][]map[string]interface{}, len(fkValues))
	for index, value := range fkValues {
		resource.RecordWDCQuery(0, 0, 0, 1)
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		ids, exists := r.RelationshipLookup[value]
		if !exists {
			continue
		}
		rows, err := r.GetRowsContext(ctx, ids, fields)
		if err != nil {
			return nil, err
		}
		result[value] = rows
	}
	return result, ctx.Err()
}

func (r *DBCReader) Size() int {
	return int(r.RecordCount)
}

func (r *DBCReader) GetRowProjected(recordID uint32, fields []string) map[string]interface{} {
	row := projectFields(r.GetRow(recordID), fields)
	resource.RecordDBCQuery(0, 0, len(row), 0)
	return row
}

func (r *DBCReader) GetRows(ids []uint32, fields []string) []map[string]interface{} {
	rows, _ := r.GetRowsContext(context.Background(), ids, fields)
	return rows
}

func (r *DBCReader) GetRowsContext(ctx context.Context, ids []uint32, fields []string) ([]map[string]interface{}, error) {
	rows := make([]map[string]interface{}, 0, len(ids))
	for index, id := range ids {
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if row := r.GetRowProjected(id, fields); row != nil {
			rows = append(rows, row)
		}
	}
	return rows, ctx.Err()
}

func (r *DBCReader) Scan(fields []string, filterFn func(map[string]interface{}) bool, limit int) []map[string]interface{} {
	rows, _ := r.ScanContext(context.Background(), fields, filterFn, limit)
	return rows
}

func (r *DBCReader) ScanContext(ctx context.Context, fields []string, filterFn func(map[string]interface{}) bool, limit int) ([]map[string]interface{}, error) {
	rows := make([]map[string]interface{}, 0)
	err := r.StreamRowsContext(ctx, fields, filterFn, limit, func(row map[string]interface{}) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

func (r *DBCReader) StreamRowsContext(ctx context.Context, fields []string, filterFn func(map[string]interface{}) bool, limit int, yield func(map[string]interface{}) error) error {
	if !r.IsLoaded {
		return ctx.Err()
	}
	emitted := 0
	for recordIndex := uint32(0); recordIndex < r.RecordCount; recordIndex++ {
		if recordIndex&255 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		offset := int64(20) + int64(recordIndex)*int64(r.RecordSize)
		row := r.readRecord(offset)
		resource.RecordDBCQuery(1, 0, 0, 0)
		if row == nil {
			continue
		}
		if filterFn != nil {
			resource.RecordDBCQuery(0, 1, 0, 0)
			if !filterFn(row) {
				continue
			}
		}
		projected := projectFields(row, fields)
		resource.RecordDBCQuery(0, 0, len(projected), 0)
		if err := yield(projected); err != nil {
			return err
		}
		emitted++
		if limit > 0 && emitted >= limit {
			return nil
		}
	}
	return ctx.Err()
}
