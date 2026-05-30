package db2

import (
	"encoding/binary"
	"fmt"
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

func (r *WDCReader) Size() int { return int(r.TotalRecordCount) + len(r.CopyTable) }

func (r *WDCReader) parseBinary(data []byte) error {
	r.data = data
	r.CopyTable = make(map[uint32]uint32)
	r.RelationshipLookup = make(map[uint32][]uint32)
	pos := 0

	if len(data) < 4 {
		return fmt.Errorf("data too short for WDC magic")
	}

	magic := binary.LittleEndian.Uint32(data[pos:])
	pos += 4

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
		pos += 4            // schemaVersion
		pos += 128          // schemaBuildString
	}

	r.RecordCount = binary.LittleEndian.Uint32(data[pos:]); pos += 4
	pos += 4 // fieldCount (unused)
	r.RecordSize = binary.LittleEndian.Uint32(data[pos:]); pos += 4
	pos += 4 // stringTableSize
	pos += 4 // tableHash

	// layoutHash: 4 LE bytes, reverse, hex uppercase
	lh := make([]byte, 4)
	copy(lh, data[pos:pos+4])
	for i, j := 0, 3; i < j; i, j = i+1, j-1 {
		lh[i], lh[j] = lh[j], lh[i]
	}
	_ = strings.ToUpper(fmt.Sprintf("%02x%02x%02x%02x", lh[0], lh[1], lh[2], lh[3]))
	pos += 4

	r.MinID = binary.LittleEndian.Uint32(data[pos:]); pos += 4
	r.MaxID = binary.LittleEndian.Uint32(data[pos:]); pos += 4
	pos += 4 // locale
	r.Flags = binary.LittleEndian.Uint16(data[pos:]); pos += 2
	idIndex := binary.LittleEndian.Uint16(data[pos:]); pos += 2
	r.IDFieldIndex = int(idIndex)
	totalFieldCount := binary.LittleEndian.Uint32(data[pos:]); pos += 4
	pos += 4                          // bitpackedDataOffset
	pos += 4                          // lookupColumnCount
	fieldStorageInfoSize := binary.LittleEndian.Uint32(data[pos:]); pos += 4
	commonDataSize := binary.LittleEndian.Uint32(data[pos:]); pos += 4
	palletDataSize := binary.LittleEndian.Uint32(data[pos:]); pos += 4
	sectionCount := binary.LittleEndian.Uint32(data[pos:]); pos += 4

	// Read section headers
	sectionHeaders := make([]SectionHeader, sectionCount)
	for i := uint32(0); i < sectionCount; i++ {
		sh := SectionHeader{}
		sh.TactKeyHash = binary.LittleEndian.Uint64(data[pos:]); pos += 8
		sh.FileOffset = binary.LittleEndian.Uint32(data[pos:]); pos += 4
		sh.RecordCount = binary.LittleEndian.Uint32(data[pos:]); pos += 4
		sh.StringTableSize = binary.LittleEndian.Uint32(data[pos:]); pos += 4
		if r.WDCVersion == 2 {
			sh.CopyTableSize = binary.LittleEndian.Uint32(data[pos:]); pos += 4
			sh.OffsetMapOffset = binary.LittleEndian.Uint32(data[pos:]); pos += 4
			sh.IDListSize = binary.LittleEndian.Uint32(data[pos:]); pos += 4
			sh.RelationshipDataSize = binary.LittleEndian.Uint32(data[pos:]); pos += 4
		} else {
			sh.OffsetRecordsEnd = binary.LittleEndian.Uint32(data[pos:]); pos += 4
			sh.IDListSize = binary.LittleEndian.Uint32(data[pos:]); pos += 4
			sh.RelationshipDataSize = binary.LittleEndian.Uint32(data[pos:]); pos += 4
			sh.OffsetMapIDCount = binary.LittleEndian.Uint32(data[pos:]); pos += 4
			sh.CopyTableCount = binary.LittleEndian.Uint32(data[pos:]); pos += 4
		}
		sectionHeaders[i] = sh
	}

	// Fields
	totalFieldCountUint := totalFieldCount // shadow
	for i := uint32(0); i < totalFieldCountUint; i++ {
		pos += 2 + 2 // size and position
	}

	// Field storage info
	infoCount := int(fieldStorageInfoSize / 24)
	r.FieldInfo = make([]FieldStorageInfo, infoCount)
	for i := 0; i < infoCount; i++ {
		r.FieldInfo[i].FieldOffsetBits = binary.LittleEndian.Uint16(data[pos:]); pos += 2
		r.FieldInfo[i].FieldSizeBits = binary.LittleEndian.Uint16(data[pos:]); pos += 2
		r.FieldInfo[i].AdditionalDataSize = binary.LittleEndian.Uint32(data[pos:]); pos += 4
		r.FieldInfo[i].FieldCompression = CompressionType(binary.LittleEndian.Uint32(data[pos:])); pos += 4
		r.FieldInfo[i].FieldCompressionPacking[0] = binary.LittleEndian.Uint32(data[pos:]); pos += 4
		r.FieldInfo[i].FieldCompressionPacking[1] = binary.LittleEndian.Uint32(data[pos:]); pos += 4
		r.FieldInfo[i].FieldCompressionPacking[2] = binary.LittleEndian.Uint32(data[pos:]); pos += 4
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
				r.PalletData[fi][i] = binary.LittleEndian.Uint32(data[pos:]); pos += 4
			}
		}
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
				key := binary.LittleEndian.Uint32(data[pos:]); pos += 4
				val := binary.LittleEndian.Uint32(data[pos:]); pos += 4
				cm[key] = val
			}
			r.CommonData[fi] = cm
		}
	}
	pos = prevCommon + int(commonDataSize)

	// WDC4+ extra chunk
	if r.WDCVersion > 3 {
		for i := uint32(0); i < sectionCount-1; i++ {
			entryCount := int(binary.LittleEndian.Uint32(data[pos:])); pos += 4
			pos += entryCount * 4
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
				id := r.MinID + i
				off := binary.LittleEndian.Uint32(data[omPos:]); omPos += 4
				sz := binary.LittleEndian.Uint16(data[omPos:]); omPos += 2
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

		// ID list
		idList := make([]uint32, sh.IDListSize/4)
		for i := range idList {
			idList[i] = binary.LittleEndian.Uint32(data[sPos:])
			sPos += 4
		}

		// Copy table
		copyCount := int(sh.CopyTableSize / 8)
		if r.WDCVersion > 2 {
			copyCount = int(sh.CopyTableCount)
		}
		for i := 0; i < copyCount; i++ {
			destID := int32(binary.LittleEndian.Uint32(data[sPos:])); sPos += 4
			srcID := int32(binary.LittleEndian.Uint32(data[sPos:])); sPos += 4
			if destID != srcID {
				r.CopyTable[uint32(destID)] = uint32(srcID)
			}
		}

		// WDC3+ offset map
		if r.WDCVersion > 2 {
			offsetMap = make(map[uint32]OffsetMapEntry, sh.OffsetMapIDCount)
			for i := uint32(0); i < sh.OffsetMapIDCount; i++ {
				off := binary.LittleEndian.Uint32(data[sPos:]); sPos += 4
				sz := binary.LittleEndian.Uint16(data[sPos:]); sPos += 2
				offsetMap[i] = OffsetMapEntry{Offset: off, Size: sz}
			}
		}

		// Relationship data
		var relationshipMap map[uint32]uint32
		if sh.RelationshipDataSize > 0 {
			relEntryCount := binary.LittleEndian.Uint32(data[sPos:]); sPos += 4
			sPos += 8 // minID, maxID
			relationshipMap = make(map[uint32]uint32, relEntryCount)
			for i := uint32(0); i < relEntryCount; i++ {
				foreignID := binary.LittleEndian.Uint32(data[sPos:]); sPos += 4
				recordIndex := binary.LittleEndian.Uint32(data[sPos:]); sPos += 4
				relationshipMap[recordIndex] = foreignID
				if _, ok := r.RelationshipLookup[foreignID]; !ok {
					r.RelationshipLookup[foreignID] = nil
				}
			}
		}

		// Offset map ID list (WDC3+, duplicate)
		if r.WDCVersion > 2 {
			sPos += int64(sh.OffsetMapIDCount) * 4
		}

		r.Sections[si] = Section{
			Header:               sh,
			IsNormal:             isNormal,
			RecordDataOfs:        recordDataOfs,
			RecordDataSize:       recordDataSize,
			StringBlockOfs:       stringBlockOfs,
			StringTableOffset:    stringTableOffset,
			StringTableOffsetBase: stringTableOffsetBase,
			IDList:               idList,
			OffsetMap:            offsetMap,
			RelationshipMap:      relationshipMap,
		}
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
					recordID = row["ID"].(uint32)
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
					id, _ := row["ID"].(uint32)
					if id == recordID {
						return row
					}
				}
			}
		}
	}

	return nil
}

func (r *WDCReader) readRecordFromSection(sectionIndex int, recordIndex, recordID uint32) map[string]interface{} {
	section := &r.Sections[sectionIndex]
	if section.IsEncrypted {
		return nil
	}

	isNormal := section.IsNormal

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

	for fi, sf := range r.Schema {
		if fi >= len(r.FieldInfo) {
			break
		}
		rfi := r.FieldInfo[fi]

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
				out[sf.Name] = section.IDList[recordIndex]
			}
			continue
		}

		if rfi.FieldCompression != CompNone {
			// For compressed fields, return zero/default
			switch sf.Type {
			case FieldInt8, FieldUInt8, FieldInt16, FieldUInt16, FieldInt32, FieldUInt32:
				out[sf.Name] = uint32(0)
			case FieldInt64, FieldUInt64:
				out[sf.Name] = uint64(0)
			case FieldFloat:
				out[sf.Name] = float32(0)
			case FieldString:
				out[sf.Name] = ""
			}
			continue
		}

		dataOfs := section.RecordDataOfs + int64(recordOfs)
		fieldByteOffset := int64(rfi.FieldOffsetBits / 8)

		switch sf.Type {
		case FieldInt8:
			out[sf.Name] = int8(r.data[dataOfs+fieldByteOffset])
		case FieldUInt8:
			out[sf.Name] = r.data[dataOfs+fieldByteOffset]
		case FieldInt16:
			out[sf.Name] = int16(binary.LittleEndian.Uint16(r.data[dataOfs+fieldByteOffset:]))
		case FieldUInt16:
			out[sf.Name] = binary.LittleEndian.Uint16(r.data[dataOfs+fieldByteOffset:])
		case FieldInt32:
			out[sf.Name] = int32(binary.LittleEndian.Uint32(r.data[dataOfs+fieldByteOffset:]))
		case FieldUInt32:
			out[sf.Name] = binary.LittleEndian.Uint32(r.data[dataOfs+fieldByteOffset:])
		case FieldInt64:
			out[sf.Name] = int64(binary.LittleEndian.Uint64(r.data[dataOfs+fieldByteOffset:]))
		case FieldUInt64:
			out[sf.Name] = binary.LittleEndian.Uint64(r.data[dataOfs+fieldByteOffset:])
		case FieldFloat:
			out[sf.Name] = float32(binary.LittleEndian.Uint32(r.data[dataOfs+fieldByteOffset:]))
		case FieldString:
			if isNormal && r.WDCVersion > 2 {
				// String table offset (WDC3+)
				ofs := binary.LittleEndian.Uint32(r.data[dataOfs+fieldByteOffset:])
				if ofs == 0 {
					out[sf.Name] = ""
				} else {
					out[sf.Name] = r.readString(section, dataOfs, fieldByteOffset, int64(ofs), recordOfs)
				}
			} else {
				// Inline null-terminated (WDC2 or sparse)
				start := dataOfs + fieldByteOffset
				end := start
				for end < int64(len(r.data)) && r.data[end] != 0 {
					end++
				}
				out[sf.Name] = string(r.data[start:end])
			}
		}
	}

	return out
}

func (r *WDCReader) readString(section *Section, dataOfs, fieldOfs, ofs int64, recordOfs uint32) string {
	// Compute outsideDataSize: sum of recordDataSize for all prior sections
	var outsideDataSize int64
	for i := range r.Sections {
		if r.Sections[i].StringTableOffset == section.StringTableOffset {
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
