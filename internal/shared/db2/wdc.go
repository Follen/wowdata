package db2

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	wdc2Magic = 0x32434457
	wdc3Magic = 0x33434457
	wdc4Magic = 0x34434457
	wdc5Magic = 0x35434457
	cls1Magic = 0x434C5331
)

type WDCReader struct {
	FileName           string
	CopyTable          map[uint32]uint32
	Schema             []SchemaField
	IsLoaded           bool
	IDField            string
	IDFieldIndex       int
	RelationshipLookup map[uint32][]uint32

	data             []byte
	Sections         []Section
	FieldInfo        []FieldStorageInfo
	PalletData       [][]uint32
	CommonData       []map[uint32]uint32
	RecordCount      uint32
	RecordSize       uint32
	Flags            uint16
	WDCVersion       int
	MinID            uint32
	MaxID            uint32
	TotalRecordCount uint32

	rows map[uint32]map[string]interface{}
}

func NewWDCReaderFromBytes(fileName string, data []byte, schema []SchemaField) (*WDCReader, error) {
	reader := &WDCReader{
		FileName:     fileName,
		Schema:       schema,
		IDField:      "ID",
		IDFieldIndex: -1,
	}
	if err := reader.parseBinary(data); err != nil {
		return nil, err
	}
	if reader.IDFieldIndex >= 0 && reader.IDFieldIndex < len(schema) {
		reader.IDField = schema[reader.IDFieldIndex].Name
	}
	return reader, nil
}

func (r *WDCReader) Size() int { return int(r.TotalRecordCount) + len(r.CopyTable) }

func (r *WDCReader) parseBinary(data []byte) error {
	r.data = data
	r.CopyTable = make(map[uint32]uint32)
	r.RelationshipLookup = make(map[uint32][]uint32)
	pos := 0
	readU16 := func() (uint16, error) {
		if pos+2 > len(data) {
			return 0, fmt.Errorf("truncated WDC data at offset %d: need 2 bytes, have %d", pos, len(data)-pos)
		}
		v := binary.LittleEndian.Uint16(data[pos:])
		pos += 2
		return v, nil
	}
	readU32 := func() (uint32, error) {
		if pos+4 > len(data) {
			return 0, fmt.Errorf("truncated WDC data at offset %d: need 4 bytes, have %d", pos, len(data)-pos)
		}
		v := binary.LittleEndian.Uint32(data[pos:])
		pos += 4
		return v, nil
	}
	readU64 := func() (uint64, error) {
		if pos+8 > len(data) {
			return 0, fmt.Errorf("truncated WDC data at offset %d: need 8 bytes, have %d", pos, len(data)-pos)
		}
		v := binary.LittleEndian.Uint64(data[pos:])
		pos += 8
		return v, nil
	}
	skip := func(n int) error {
		if pos+n > len(data) {
			return fmt.Errorf("truncated WDC data at offset %d: need %d bytes, have %d", pos, n, len(data)-pos)
		}
		pos += n
		return nil
	}

	if len(data) < 4 {
		return fmt.Errorf("data too short for WDC magic")
	}

	magic, _ := readU32()

	switch magic {
	case wdc2Magic, cls1Magic:
		r.WDCVersion = 2
	case wdc3Magic:
		r.WDCVersion = 3
	case wdc4Magic:
		r.WDCVersion = 4
	case wdc5Magic:
		r.WDCVersion = 5
	default:
		return fmt.Errorf("unsupported DB2 type: 0x%X", magic)
	}

	if r.WDCVersion == 5 {
		if err := skip(4); err != nil {
			return err
		} // schemaVersion
		if err := skip(128); err != nil {
			return err
		} // schemaBuildString
	}

	var err error
	if r.RecordCount, err = readU32(); err != nil {
		return err
	}
	if err := skip(4); err != nil {
		return err
	} // fieldCount (unused)
	if r.RecordSize, err = readU32(); err != nil {
		return err
	}
	if err := skip(4); err != nil {
		return err
	} // stringTableSize
	if err := skip(4); err != nil {
		return err
	} // tableHash

	// layoutHash: 4 LE bytes, reverse, hex uppercase
	if pos+4 > len(data) {
		return fmt.Errorf("truncated WDC data at offset %d: need layout hash", pos)
	}
	lh := make([]byte, 4)
	copy(lh, data[pos:pos+4])
	for i, j := 0, 3; i < j; i, j = i+1, j-1 {
		lh[i], lh[j] = lh[j], lh[i]
	}
	_ = strings.ToUpper(fmt.Sprintf("%02x%02x%02x%02x", lh[0], lh[1], lh[2], lh[3]))
	pos += 4

	if r.MinID, err = readU32(); err != nil {
		return err
	}
	if r.MaxID, err = readU32(); err != nil {
		return err
	}
	if err := skip(4); err != nil {
		return err
	} // locale
	if r.Flags, err = readU16(); err != nil {
		return err
	}
	idIndex, err := readU16()
	if err != nil {
		return err
	}
	r.IDFieldIndex = int(idIndex)
	totalFieldCount, err := readU32()
	if err != nil {
		return err
	}
	if err := skip(4); err != nil {
		return err
	} // bitpackedDataOffset
	if err := skip(4); err != nil {
		return err
	} // lookupColumnCount
	fieldStorageInfoSize, err := readU32()
	if err != nil {
		return err
	}
	commonDataSize, err := readU32()
	if err != nil {
		return err
	}
	palletDataSize, err := readU32()
	if err != nil {
		return err
	}
	sectionCount, err := readU32()
	if err != nil {
		return err
	}

	// Read section headers
	sectionHeaders := make([]SectionHeader, sectionCount)
	for i := uint32(0); i < sectionCount; i++ {
		sh := SectionHeader{}
		if sh.TactKeyHash, err = readU64(); err != nil {
			return err
		}
		if sh.FileOffset, err = readU32(); err != nil {
			return err
		}
		if sh.RecordCount, err = readU32(); err != nil {
			return err
		}
		if sh.StringTableSize, err = readU32(); err != nil {
			return err
		}
		if r.WDCVersion == 2 {
			if sh.CopyTableSize, err = readU32(); err != nil {
				return err
			}
			if sh.OffsetMapOffset, err = readU32(); err != nil {
				return err
			}
			if sh.IDListSize, err = readU32(); err != nil {
				return err
			}
			if sh.RelationshipDataSize, err = readU32(); err != nil {
				return err
			}
		} else {
			if sh.OffsetRecordsEnd, err = readU32(); err != nil {
				return err
			}
			if sh.IDListSize, err = readU32(); err != nil {
				return err
			}
			if sh.RelationshipDataSize, err = readU32(); err != nil {
				return err
			}
			if sh.OffsetMapIDCount, err = readU32(); err != nil {
				return err
			}
			if sh.CopyTableCount, err = readU32(); err != nil {
				return err
			}
		}
		sectionHeaders[i] = sh
	}

	// Fields
	totalFieldCountUint := totalFieldCount // shadow
	for i := uint32(0); i < totalFieldCountUint; i++ {
		if err := skip(2 + 2); err != nil {
			return err
		} // size and position
	}

	// Field storage info
	infoCount := int(fieldStorageInfoSize / 24)
	r.FieldInfo = make([]FieldStorageInfo, infoCount)
	for i := 0; i < infoCount; i++ {
		if r.FieldInfo[i].FieldOffsetBits, err = readU16(); err != nil {
			return err
		}
		if r.FieldInfo[i].FieldSizeBits, err = readU16(); err != nil {
			return err
		}
		if r.FieldInfo[i].AdditionalDataSize, err = readU32(); err != nil {
			return err
		}
		compression, err := readU32()
		if err != nil {
			return err
		}
		r.FieldInfo[i].FieldCompression = CompressionType(compression)
		if r.FieldInfo[i].FieldCompressionPacking[0], err = readU32(); err != nil {
			return err
		}
		if r.FieldInfo[i].FieldCompressionPacking[1], err = readU32(); err != nil {
			return err
		}
		if r.FieldInfo[i].FieldCompressionPacking[2], err = readU32(); err != nil {
			return err
		}
	}

	// Pallet data
	prevPallet := pos
	r.PalletData = make([][]uint32, len(r.FieldInfo))
	for fi := 0; fi < len(r.FieldInfo); fi++ {
		fiInfo := r.FieldInfo[fi]
		if fiInfo.FieldCompression == CompBitpackedIndexed || fiInfo.FieldCompression == CompBitpackedIndexedArray {
			n := int(fiInfo.AdditionalDataSize / 4)
			r.PalletData[fi] = make([]uint32, n)
			for i := 0; i < n; i++ {
				var err error
				if r.PalletData[fi][i], err = readU32(); err != nil {
					return err
				}
			}
		}
	}
	if prevPallet+int(palletDataSize) > len(data) {
		return fmt.Errorf("truncated WDC pallet data at offset %d: size %d exceeds file size %d", prevPallet, palletDataSize, len(data))
	}
	pos = prevPallet + int(palletDataSize)

	// Common data
	prevCommon := pos
	r.CommonData = make([]map[uint32]uint32, len(r.FieldInfo))
	for fi := 0; fi < len(r.FieldInfo); fi++ {
		fiInfo := r.FieldInfo[fi]
		if fiInfo.FieldCompression == CompCommonData {
			n := int(fiInfo.AdditionalDataSize / 8)
			cm := make(map[uint32]uint32)
			for i := 0; i < n; i++ {
				key, err := readU32()
				if err != nil {
					return err
				}
				val, err := readU32()
				if err != nil {
					return err
				}
				cm[key] = val
			}
			r.CommonData[fi] = cm
		}
	}
	if prevCommon+int(commonDataSize) > len(data) {
		return fmt.Errorf("truncated WDC common data at offset %d: size %d exceeds file size %d", prevCommon, commonDataSize, len(data))
	}
	pos = prevCommon + int(commonDataSize)

	// WDC4+ extra chunk
	if r.WDCVersion > 3 {
		for i := uint32(0); i < sectionCount-1; i++ {
			if pos == len(data) && r.RecordCount == 0 {
				break
			}
			entryCountRaw, err := readU32()
			if err != nil {
				return err
			}
			if err := skip(int(entryCountRaw) * 4); err != nil {
				return err
			}
		}
	}

	// Read sections
	r.Sections = make([]Section, sectionCount)
	var previousStringTableSize uint32
	for si := uint32(0); si < sectionCount; si++ {
		sh := sectionHeaders[si]
		isNormal := r.Flags&1 == 0

		recordDataOfs := int64(pos)
		recordsOfs := sh.OffsetMapOffset
		if r.WDCVersion > 2 {
			recordsOfs = sh.OffsetRecordsEnd
		}
		recordDataSize := int64(r.RecordSize) * int64(sh.RecordCount)
		if !isNormal {
			recordDataSize = int64(recordsOfs) - int64(sh.FileOffset)
		}
		stringBlockOfs := recordDataOfs + recordDataSize

		// Offset map for WDC2 sparse
		var offsetMap map[uint32]OffsetMapEntry
		if r.WDCVersion == 2 && !isNormal {
			omPos := int(sh.OffsetMapOffset)
			omCount := r.MaxID - r.MinID + 1
			offsetMap = make(map[uint32]OffsetMapEntry, omCount)
			for i := uint32(0); i < omCount; i++ {
				if omPos < 0 || omPos+6 > len(data) {
					return fmt.Errorf("truncated WDC offset map section %d at offset %d: need 6 bytes, have %d", si, omPos, len(data)-omPos)
				}
				id := r.MinID + i
				off := binary.LittleEndian.Uint32(data[omPos:])
				omPos += 4
				sz := binary.LittleEndian.Uint16(data[omPos:])
				omPos += 2
				offsetMap[id] = OffsetMapEntry{Offset: off, Size: sz}
			}
		}

		stringTableOffset := stringBlockOfs
		stringTableOffsetBase := int64(previousStringTableSize)
		if r.WDCVersion > 2 {
			previousStringTableSize += sh.StringTableSize
		}

		// Seek to string table + string data
		sPos := stringBlockOfs + int64(sh.StringTableSize)
		readSectionU16 := func() (uint16, error) {
			if sPos < 0 || sPos+2 > int64(len(data)) {
				return 0, fmt.Errorf("truncated WDC section %d at offset %d: need 2 bytes, have %d", si, sPos, int64(len(data))-sPos)
			}
			v := binary.LittleEndian.Uint16(data[sPos:])
			sPos += 2
			return v, nil
		}
		readSectionU32 := func() (uint32, error) {
			if sPos < 0 || sPos+4 > int64(len(data)) {
				return 0, fmt.Errorf("truncated WDC section %d at offset %d: need 4 bytes, have %d", si, sPos, int64(len(data))-sPos)
			}
			v := binary.LittleEndian.Uint32(data[sPos:])
			sPos += 4
			return v, nil
		}
		skipSection := func(n int64) error {
			if n < 0 || sPos < 0 || sPos+n > int64(len(data)) {
				return fmt.Errorf("truncated WDC section %d at offset %d: need %d bytes, have %d", si, sPos, n, int64(len(data))-sPos)
			}
			sPos += n
			return nil
		}

		// ID list
		idList := make([]uint32, sh.IDListSize/4)
		for i := range idList {
			id, err := readSectionU32()
			if err != nil {
				return err
			}
			idList[i] = id
		}

		// Copy table
		copyCount := int(sh.CopyTableSize / 8)
		if r.WDCVersion > 2 {
			copyCount = int(sh.CopyTableCount)
		}
		for i := 0; i < copyCount; i++ {
			destRaw, err := readSectionU32()
			if err != nil {
				return err
			}
			srcRaw, err := readSectionU32()
			if err != nil {
				return err
			}
			destID := int32(destRaw)
			srcID := int32(srcRaw)
			if destID != srcID {
				r.CopyTable[uint32(destID)] = uint32(srcID)
			}
		}

		// WDC3+ offset map
		if r.WDCVersion > 2 {
			offsetMap = make(map[uint32]OffsetMapEntry, sh.OffsetMapIDCount)
			for i := uint32(0); i < sh.OffsetMapIDCount; i++ {
				off, err := readSectionU32()
				if err != nil {
					return err
				}
				sz, err := readSectionU16()
				if err != nil {
					return err
				}
				offsetMap[i] = OffsetMapEntry{Offset: off, Size: sz}
			}
		}

		// Relationship data
		var relationshipMap map[uint32]uint32
		if sh.RelationshipDataSize > 0 {
			relStart := sPos
			relEnd := relStart + int64(sh.RelationshipDataSize)
			if relEnd > int64(len(data)) {
				return fmt.Errorf("truncated WDC section %d relationship data at offset %d: need %d bytes, have %d", si, relStart, sh.RelationshipDataSize, int64(len(data))-relStart)
			}
			relEntryCount, err := readSectionU32()
			if err != nil {
				return err
			}
			if err := skipSection(8); err != nil {
				return err
			} // minID, maxID
			relationshipMap = make(map[uint32]uint32, relEntryCount)
			maxEntries := uint32((relEnd - sPos) / 8)
			if relEntryCount > maxEntries {
				relEntryCount = maxEntries
			}
			for i := uint32(0); i < relEntryCount; i++ {
				foreignID, err := readSectionU32()
				if err != nil {
					return err
				}
				recordIndex, err := readSectionU32()
				if err != nil {
					return err
				}
				relationshipMap[recordIndex] = foreignID
				if _, ok := r.RelationshipLookup[foreignID]; !ok {
					r.RelationshipLookup[foreignID] = nil
				}
				recordID := recordIndex
				if int(recordIndex) < len(idList) {
					recordID = idList[recordIndex]
				}
				if !containsUint32(r.RelationshipLookup[foreignID], recordID) {
					r.RelationshipLookup[foreignID] = append(r.RelationshipLookup[foreignID], recordID)
				}
			}
			sPos = relEnd
		}

		// Offset map ID list (WDC3+, duplicate)
		if r.WDCVersion > 2 {
			if err := skipSection(int64(sh.OffsetMapIDCount) * 4); err != nil {
				return err
			}
		}

		r.Sections[si] = Section{
			Header:                sh,
			IsNormal:              isNormal,
			RecordDataOfs:         recordDataOfs,
			RecordDataSize:        recordDataSize,
			StringBlockOfs:        stringBlockOfs,
			StringTableOffset:     stringTableOffset,
			StringTableOffsetBase: stringTableOffsetBase,
			IDList:                idList,
			OffsetMap:             offsetMap,
			RelationshipMap:       relationshipMap,
		}
		pos = int(sPos)
	}

	// Detect encrypted sections
	r.TotalRecordCount = 0
	for si := uint32(0); si < sectionCount; si++ {
		section := &r.Sections[si]
		sh := section.Header
		if sh.TactKeyHash != 0 {
			// Check if record data is all zeros
			isZeroed := true
			for i := int64(0); i < section.RecordDataSize; i++ {
				if data[section.RecordDataOfs+i] != 0 {
					isZeroed = false
					break
				}
			}
			if isZeroed {
				section.IsEncrypted = true
				continue
			}
		}
		r.TotalRecordCount += sh.RecordCount
	}

	r.IsLoaded = true
	return nil
}

func (r *WDCReader) GetRow(recordID uint32) map[string]interface{} {
	if !r.IsLoaded {
		return nil
	}

	// Check copy table
	if srcID, ok := r.CopyTable[recordID]; ok {
		row := r.readRecord(srcID)
		if row != nil {
			row["ID"] = recordID
			return row
		}
	}

	return r.readRecord(recordID)
}

func (r *WDCReader) GetAllRows() map[uint32]map[string]interface{} {
	if !r.IsLoaded {
		return nil
	}

	rows := make(map[uint32]map[string]interface{})

	for si := range r.Sections {
		section := &r.Sections[si]
		if section.IsEncrypted {
			continue
		}

		hasIDMap := len(section.IDList) > 0
		emptyIDMap := hasIDMap
		for _, id := range section.IDList {
			if id != 0 {
				emptyIDMap = false
				break
			}
		}

		for ri := uint32(0); ri < section.Header.RecordCount; ri++ {
			var recordID uint32
			if hasIDMap && emptyIDMap {
				recordID = ri
			} else if hasIDMap {
				recordID = section.IDList[ri]
			}

			row := r.readRecordFromSection(si, ri, recordID)
			if row != nil {
				if recordID == 0 && !hasIDMap {
					recordID = rowIDUint32(row["ID"])
					if recordID == 0 {
						recordID = uint32(len(rows))
					}
				}
				rows[recordID] = row
			}
		}
	}

	// Inflate copy table
	for destID, srcID := range r.CopyTable {
		if src, ok := rows[srcID]; ok {
			cp := make(map[string]interface{})
			for k, v := range src {
				cp[k] = v
			}
			cp["ID"] = destID
			rows[destID] = cp
		}
	}

	return rows
}

func (r *WDCReader) ForEachRow(fn func(uint32, map[string]interface{}) error) error {
	if !r.IsLoaded {
		return nil
	}
	seen := map[uint32]struct{}{}
	for si := range r.Sections {
		section := &r.Sections[si]
		if section.IsEncrypted {
			continue
		}
		hasIDMap := len(section.IDList) > 0
		emptyIDMap := hasIDMap
		for _, id := range section.IDList {
			if id != 0 {
				emptyIDMap = false
				break
			}
		}
		for ri := uint32(0); ri < section.Header.RecordCount; ri++ {
			var recordID uint32
			if hasIDMap && emptyIDMap {
				recordID = ri
			} else if hasIDMap {
				recordID = section.IDList[ri]
			}
			row := r.readRecordFromSection(si, ri, recordID)
			if row == nil {
				continue
			}
			if recordID == 0 && !hasIDMap {
				recordID = rowIDUint32(row["ID"])
				if recordID == 0 {
					recordID = uint32(len(seen))
				}
			}
			seen[recordID] = struct{}{}
			if err := fn(recordID, row); err != nil {
				return err
			}
		}
	}
	copyIDs := make([]uint32, 0, len(r.CopyTable))
	for destID := range r.CopyTable {
		copyIDs = append(copyIDs, destID)
	}
	sort.Slice(copyIDs, func(i, j int) bool { return copyIDs[i] < copyIDs[j] })
	for _, destID := range copyIDs {
		if _, ok := seen[destID]; ok {
			continue
		}
		srcID := r.CopyTable[destID]
		row := r.readRecord(srcID)
		if row == nil {
			continue
		}
		cp := make(map[string]interface{}, len(row)+1)
		for key, value := range row {
			cp[key] = value
		}
		cp["ID"] = destID
		if err := fn(destID, cp); err != nil {
			return err
		}
	}
	return nil
}

func (r *WDCReader) GetRelationshipRows(fkValue uint32) ([]map[string]interface{}, bool) {
	if !r.IsLoaded || r.RelationshipLookup == nil {
		return nil, false
	}
	recordIDs, ok := r.RelationshipLookup[fkValue]
	if !ok {
		return nil, false
	}
	if len(recordIDs) == 0 {
		return []map[string]interface{}{}, true
	}
	rows := make([]map[string]interface{}, 0, len(recordIDs))
	for _, recordID := range recordIDs {
		if row := r.readRecord(recordID); row != nil {
			rows = append(rows, row)
		}
	}
	return rows, true
}

func (r *WDCReader) readRecord(recordID uint32) map[string]interface{} {
	for si := range r.Sections {
		section := &r.Sections[si]
		if section.IsEncrypted {
			continue
		}

		hasIDMap := len(section.IDList) > 0
		emptyIDMap := hasIDMap
		for _, id := range section.IDList {
			if id != 0 {
				emptyIDMap = false
				break
			}
		}

		if hasIDMap && !emptyIDMap {
			for ri, id := range section.IDList {
				if id == recordID {
					return r.readRecordFromSection(si, uint32(ri), recordID)
				}
			}
		} else if emptyIDMap && recordID < section.Header.RecordCount {
			return r.readRecordFromSection(si, recordID, recordID)
		} else if !hasIDMap {
			// Scan for inline ID
			for ri := uint32(0); ri < section.Header.RecordCount; ri++ {
				row := r.readRecordFromSection(si, ri, 0)
				if row != nil {
					if rowIDUint32(row["ID"]) == recordID {
						return row
					}
				}
			}
		}
	}

	return nil
}

func rowIDUint32(value interface{}) uint32 {
	switch v := value.(type) {
	case uint8:
		return uint32(v)
	case uint16:
		return uint32(v)
	case uint32:
		return v
	case uint64:
		return uint32(v)
	case uint:
		return uint32(v)
	case int8:
		if v > 0 {
			return uint32(v)
		}
	case int16:
		if v > 0 {
			return uint32(v)
		}
	case int32:
		if v > 0 {
			return uint32(v)
		}
	case int64:
		if v > 0 {
			return uint32(v)
		}
	case int:
		if v > 0 {
			return uint32(v)
		}
	}
	return 0
}

func (r *WDCReader) readRecordFromSection(sectionIndex int, recordIndex, recordID uint32) map[string]interface{} {
	if sectionIndex < 0 || sectionIndex >= len(r.Sections) {
		return nil
	}
	section := &r.Sections[sectionIndex]
	if section.IsEncrypted {
		return nil
	}

	isNormal := section.IsNormal
	hasIDMap := len(section.IDList) > 0
	emptyIDMap := hasIDMap
	for _, id := range section.IDList {
		if id != 0 {
			emptyIDMap = false
			break
		}
	}

	// Compute record offset
	var recordOfs uint32
	if isNormal {
		recordOfs = recordIndex * r.RecordSize
	} else {
		if r.WDCVersion == 2 {
			if om, ok := section.OffsetMap[recordID]; ok {
				recordOfs = om.Offset
			} else {
				return nil
			}
		} else {
			if om, ok := section.OffsetMap[recordIndex]; ok {
				recordOfs = om.Offset
			} else {
				return nil
			}
		}
	}

	out := make(map[string]interface{})
	fieldInfoIndex := 0
	recordBase := section.RecordDataOfs + int64(recordOfs)
	if !isNormal {
		recordBase = int64(recordOfs)
	}
	cursor := recordBase
	read := func(n int64) ([]byte, bool) {
		if n < 0 || cursor < 0 || cursor+n > int64(len(r.data)) {
			return nil, false
		}
		if isNormal && cursor+n > section.RecordDataOfs+section.RecordDataSize {
			return nil, false
		}
		data := r.data[cursor : cursor+n]
		cursor += n
		return data, true
	}
	captureInlineRecordID := func(field SchemaField, fieldIndex int, value interface{}) {
		if !hasIDMap && (fieldIndex == r.IDFieldIndex || field.Name == r.IDField || field.Name == "ID") {
			if id := rowIDUint32(value); id != 0 {
				recordID = id
			}
		}
	}

	for _, sf := range r.Schema {
		if sf.Type == FieldRelation {
			if section.RelationshipMap != nil {
				if foreignID, ok := section.RelationshipMap[recordIndex]; ok {
					out[sf.Name] = foreignID
				} else {
					out[sf.Name] = uint32(0)
				}
			} else {
				out[sf.Name] = uint32(0)
			}
			continue
		}

		if sf.Type == FieldNonInlineID {
			if len(section.IDList) > int(recordIndex) {
				id := section.IDList[recordIndex]
				if !emptyIDMap {
					recordID = id
				}
				out[sf.Name] = id
			}
			continue
		}
		if fieldInfoIndex >= len(r.FieldInfo) {
			break
		}
		rfi := r.FieldInfo[fieldInfoIndex]
		fieldInfoIndex++

		if rfi.FieldCompression != CompNone {
			out[sf.Name] = r.readCompressedField(section, fieldInfoIndex-1, rfi, sf, recordOfs, recordID)
			captureInlineRecordID(sf, fieldInfoIndex-1, out[sf.Name])
			continue
		}

		if sf.ArrayLen > 0 && sf.Type != FieldString {
			var ok bool
			out[sf.Name], cursor, ok = r.readSequentialArray(cursor, sf, section, isNormal)
			if !ok {
				return nil
			}
			continue
		}

		switch sf.Type {
		case FieldInt8:
			data, ok := read(1)
			if !ok {
				return nil
			}
			out[sf.Name] = int8(data[0])
		case FieldUInt8:
			data, ok := read(1)
			if !ok {
				return nil
			}
			out[sf.Name] = data[0]
		case FieldInt16:
			data, ok := read(2)
			if !ok {
				return nil
			}
			out[sf.Name] = int16(binary.LittleEndian.Uint16(data))
		case FieldUInt16:
			data, ok := read(2)
			if !ok {
				return nil
			}
			out[sf.Name] = binary.LittleEndian.Uint16(data)
		case FieldInt32:
			data, ok := read(4)
			if !ok {
				return nil
			}
			out[sf.Name] = int32(binary.LittleEndian.Uint32(data))
		case FieldUInt32:
			data, ok := read(4)
			if !ok {
				return nil
			}
			out[sf.Name] = binary.LittleEndian.Uint32(data)
		case FieldInt64:
			data, ok := read(8)
			if !ok {
				return nil
			}
			out[sf.Name] = int64(binary.LittleEndian.Uint64(data))
		case FieldUInt64:
			data, ok := read(8)
			if !ok {
				return nil
			}
			out[sf.Name] = binary.LittleEndian.Uint64(data)
		case FieldFloat:
			data, ok := read(4)
			if !ok {
				return nil
			}
			out[sf.Name] = math.Float32frombits(binary.LittleEndian.Uint32(data))
		case FieldString:
			if isNormal && r.WDCVersion > 2 {
				// String table offset (WDC3+)
				count := sf.ArrayLen
				if count <= 0 {
					count = 1
				}
				values := make([]interface{}, count)
				bitSize := int(rfi.FieldSizeBits) / count
				if bitSize == 0 {
					bitSize = 32
				}
				for i := 0; i < count; i++ {
					fieldByteOffset := int64((int(rfi.FieldOffsetBits) + i*bitSize) / 8)
					data, ok := read(4)
					if !ok {
						return nil
					}
					ofs := binary.LittleEndian.Uint32(data)
					if ofs == 0 {
						values[i] = ""
					} else {
						values[i] = r.readString(section, fieldByteOffset, int64(ofs), recordOfs)
					}
				}
				if sf.ArrayLen > 0 {
					out[sf.Name] = values
				} else {
					out[sf.Name] = values[0]
				}
			} else {
				// Inline null-terminated (WDC2 or sparse)
				count := sf.ArrayLen
				if count <= 0 {
					count = 1
				}
				values := make([]interface{}, count)
				for i := 0; i < count; i++ {
					start := cursor
					end := start
					recordEnd := int64(len(r.data))
					if isNormal {
						recordEnd = section.RecordDataOfs + section.RecordDataSize
					}
					for end < recordEnd && end < int64(len(r.data)) && r.data[end] != 0 {
						end++
					}
					if start < 0 || start > end || end > int64(len(r.data)) {
						return nil
					}
					values[i] = string(r.data[start:end])
					if end < recordEnd && end < int64(len(r.data)) {
						end++
					}
					cursor = end
				}
				if sf.ArrayLen > 0 {
					out[sf.Name] = values
				} else {
					out[sf.Name] = values[0]
				}
			}
		}
		captureInlineRecordID(sf, fieldInfoIndex-1, out[sf.Name])
	}

	if section.RelationshipMap != nil {
		if foreignID, ok := section.RelationshipMap[recordIndex]; ok {
			lookup := r.RelationshipLookup[foreignID]
			if lookup != nil && !containsUint32(lookup, recordID) {
				r.RelationshipLookup[foreignID] = append(lookup, recordID)
			}
		}
	}

	return out
}

func containsUint32(values []uint32, needle uint32) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func (r *WDCReader) readArrayField(dataOfs int64, info FieldStorageInfo, field SchemaField) interface{} {
	count := field.ArrayLen
	if count <= 0 {
		return nil
	}
	out := make([]interface{}, count)
	bitSize := int(info.FieldSizeBits) / count
	if bitSize == 0 {
		bitSize = fieldTypeBitSize(field.Type)
	}
	for i := 0; i < count; i++ {
		bitOffset := int(info.FieldOffsetBits) + i*bitSize
		raw := r.readBits(dataOfs, bitOffset, bitSize)
		out[i] = reinterpretCompressedValue(raw, field.Type)
	}
	return out
}

func (r *WDCReader) readSequentialArray(cursor int64, field SchemaField, section *Section, isNormal bool) (interface{}, int64, bool) {
	out := make([]interface{}, field.ArrayLen)
	read := func(n int64) ([]byte, bool) {
		if n < 0 || cursor < 0 || cursor+n > int64(len(r.data)) {
			return nil, false
		}
		if isNormal && cursor+n > section.RecordDataOfs+section.RecordDataSize {
			return nil, false
		}
		data := r.data[cursor : cursor+n]
		cursor += n
		return data, true
	}
	for i := 0; i < field.ArrayLen; i++ {
		switch field.Type {
		case FieldInt8:
			data, ok := read(1)
			if !ok {
				return nil, cursor, false
			}
			out[i] = int8(data[0])
		case FieldUInt8:
			data, ok := read(1)
			if !ok {
				return nil, cursor, false
			}
			out[i] = data[0]
		case FieldInt16:
			data, ok := read(2)
			if !ok {
				return nil, cursor, false
			}
			out[i] = int16(binary.LittleEndian.Uint16(data))
		case FieldUInt16:
			data, ok := read(2)
			if !ok {
				return nil, cursor, false
			}
			out[i] = binary.LittleEndian.Uint16(data)
		case FieldInt32:
			data, ok := read(4)
			if !ok {
				return nil, cursor, false
			}
			out[i] = int32(binary.LittleEndian.Uint32(data))
		case FieldUInt32:
			data, ok := read(4)
			if !ok {
				return nil, cursor, false
			}
			out[i] = binary.LittleEndian.Uint32(data)
		case FieldInt64:
			data, ok := read(8)
			if !ok {
				return nil, cursor, false
			}
			out[i] = int64(binary.LittleEndian.Uint64(data))
		case FieldUInt64:
			data, ok := read(8)
			if !ok {
				return nil, cursor, false
			}
			out[i] = binary.LittleEndian.Uint64(data)
		case FieldFloat:
			data, ok := read(4)
			if !ok {
				return nil, cursor, false
			}
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data))
		}
	}
	return out, cursor, true
}

func (r *WDCReader) readBits(base int64, bitOffset, bitSize int) uint64 {
	if bitSize <= 0 {
		return 0
	}
	byteOfs := base + int64(bitOffset/8)
	shift := uint(bitOffset & 7)
	var raw uint64
	for i := 0; i < 8; i++ {
		pos := byteOfs + int64(i)
		if pos >= int64(len(r.data)) {
			break
		}
		raw |= uint64(r.data[pos]) << (8 * i)
	}
	if bitSize >= 64 {
		return raw >> shift
	}
	return (raw >> shift) & ((uint64(1) << bitSize) - 1)
}

func fieldTypeBitSize(fieldType FieldType) int {
	switch fieldType {
	case FieldInt8, FieldUInt8:
		return 8
	case FieldInt16, FieldUInt16:
		return 16
	case FieldInt32, FieldUInt32, FieldFloat:
		return 32
	case FieldInt64, FieldUInt64:
		return 64
	default:
		return 32
	}
}

func (r *WDCReader) readCompressedField(section *Section, fieldInfoIndex int, info FieldStorageInfo, field SchemaField, recordOfs, recordID uint32) interface{} {
	var value interface{}
	switch info.FieldCompression {
	case CompCommonData:
		value = uint64(info.FieldCompressionPacking[0])
		if fieldInfoIndex >= 0 && fieldInfoIndex < len(r.CommonData) && r.CommonData[fieldInfoIndex] != nil {
			if v, ok := r.CommonData[fieldInfoIndex][recordID]; ok {
				value = uint64(v)
			}
		}
	case CompBitpacked, CompBitpackedSigned, CompBitpackedIndexed, CompBitpackedIndexedArray:
		bitpacked := r.readBitpackedValue(section, info, recordOfs)
		if info.FieldCompression == CompBitpackedIndexedArray {
			count := int(info.FieldCompressionPacking[2])
			values := make([]interface{}, count)
			for i := 0; i < count; i++ {
				idx := int(bitpacked)*count + i
				var raw uint64
				if fieldInfoIndex >= 0 && fieldInfoIndex < len(r.PalletData) && idx >= 0 && idx < len(r.PalletData[fieldInfoIndex]) {
					raw = uint64(r.PalletData[fieldInfoIndex][idx])
				}
				values[i] = reinterpretCompressedValue(raw, field.Type)
			}
			return values
		}
		if info.FieldCompression == CompBitpackedIndexed {
			idx := int(bitpacked)
			if fieldInfoIndex >= 0 && fieldInfoIndex < len(r.PalletData) && idx >= 0 && idx < len(r.PalletData[fieldInfoIndex]) {
				value = uint64(r.PalletData[fieldInfoIndex][idx])
			} else {
				value = uint64(0)
			}
		} else if info.FieldCompression == CompBitpackedSigned {
			value = signExtend(bitpacked, int(info.FieldSizeBits))
		} else {
			value = bitpacked
		}
	default:
		value = uint64(0)
	}
	return reinterpretCompressedValue(value, field.Type)
}

func (r *WDCReader) readBitpackedValue(section *Section, info FieldStorageInfo, recordOfs uint32) uint64 {
	dataOfs := section.RecordDataOfs + int64(recordOfs) + int64(info.FieldOffsetBits/8)
	if !section.IsNormal {
		dataOfs = int64(recordOfs) + int64(info.FieldOffsetBits/8)
	}
	if dataOfs < 0 || dataOfs >= int64(len(r.data)) {
		return 0
	}
	var raw uint64
	for i := 0; i < 8; i++ {
		pos := dataOfs + int64(i)
		if pos >= int64(len(r.data)) {
			break
		}
		raw |= uint64(r.data[pos]) << (8 * i)
	}
	bitOffset := uint(info.FieldOffsetBits & 7)
	if info.FieldSizeBits == 0 {
		return 0
	}
	if info.FieldSizeBits >= 64 {
		return raw >> bitOffset
	}
	mask := (uint64(1) << info.FieldSizeBits) - 1
	return (raw >> bitOffset) & mask
}

func signExtend(value uint64, bits int) int64 {
	if bits <= 0 {
		return 0
	}
	if bits >= 64 {
		return int64(value)
	}
	shift := 64 - bits
	return int64(value<<shift) >> shift
}

func reinterpretCompressedValue(value interface{}, fieldType FieldType) interface{} {
	var unsigned uint64
	var signed int64
	switch v := value.(type) {
	case int64:
		signed = v
		unsigned = uint64(v)
	case uint64:
		unsigned = v
		signed = int64(v)
	case uint32:
		unsigned = uint64(v)
		signed = int64(v)
	case int:
		signed = int64(v)
		unsigned = uint64(v)
	default:
		return value
	}
	switch fieldType {
	case FieldInt8:
		return int8(signed)
	case FieldUInt8:
		return uint8(unsigned)
	case FieldInt16:
		return int16(signed)
	case FieldUInt16:
		return uint16(unsigned)
	case FieldInt32:
		return int32(signed)
	case FieldUInt32:
		return uint32(unsigned)
	case FieldInt64:
		return int64(signed)
	case FieldUInt64:
		return uint64(unsigned)
	case FieldFloat:
		return math.Float32frombits(uint32(unsigned))
	case FieldString:
		return ""
	default:
		return uint32(unsigned)
	}
}

func (r *WDCReader) readString(section *Section, fieldOfs, ofs int64, recordOfs uint32) string {
	// Compute outsideDataSize: sum of recordDataSize for all prior sections
	var outsideDataSize int64
	for i := range r.Sections {
		if &r.Sections[i] == section {
			break
		}
		outsideDataSize += r.Sections[i].RecordDataSize
	}

	absoluteRecordOfs := int64(recordOfs) - int64(r.RecordCount)*int64(r.RecordSize)
	stringTableIndex := outsideDataSize + absoluteRecordOfs + fieldOfs + ofs

	// Find which section this string table index belongs to
	for i := range r.Sections {
		sec := &r.Sections[i]
		localOfs := stringTableIndex - sec.StringTableOffsetBase
		if localOfs >= 0 && localOfs < int64(sec.Header.StringTableSize) {
			pos := sec.StringTableOffset + localOfs
			end := pos
			for end < int64(len(r.data)) && r.data[end] != 0 {
				end++
			}
			return string(r.data[pos:end])
		}
	}

	return ""
}
