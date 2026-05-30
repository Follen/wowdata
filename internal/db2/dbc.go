package db2

import (
	"encoding/binary"
	"fmt"
)

const dbcMagic = 0x43424457 // WDBC

type DBCReader struct {
	FileName         string
	Schema           []SchemaField
	BuildID          string
	IsLoaded         bool
	Data             []byte
	RecordCount      uint32
	FieldCount       uint32
	RecordSize       uint32
	StringBlockSize  uint32
	StringBlockOffset int64
}

func NewDBCReader(fileName, buildID string) *DBCReader {
	return &DBCReader{
		FileName: fileName,
		BuildID:  buildID,
	}
}

func (r *DBCReader) Parse() error {
	data := r.Data
	if len(data) < 20 {
		return fmt.Errorf("DBC data too short")
	}

	magic := binary.LittleEndian.Uint32(data)
	if magic != dbcMagic {
		return fmt.Errorf("invalid DBC magic: 0x%X", magic)
	}

	r.RecordCount = binary.LittleEndian.Uint32(data[4:])
	r.FieldCount = binary.LittleEndian.Uint32(data[8:])
	r.RecordSize = binary.LittleEndian.Uint32(data[12:])
	r.StringBlockSize = binary.LittleEndian.Uint32(data[16:])

	r.StringBlockOffset = 20 + int64(r.RecordCount)*int64(r.RecordSize)
	r.IsLoaded = true
	return nil
}

func (r *DBCReader) GetRow(recordID uint32) map[string]interface{} {
	if !r.IsLoaded {
		return nil
	}

	ofs := int64(20 + recordID*r.RecordSize)
	if ofs+int64(r.RecordSize) > int64(len(r.Data)) {
		return nil
	}
	if ofs >= r.StringBlockOffset {
		return nil
	}

	return r.readRecord(ofs)
}

func (r *DBCReader) GetAllRows() map[uint32]map[string]interface{} {
	if !r.IsLoaded {
		return nil
	}

	rows := make(map[uint32]map[string]interface{})
	for i := uint32(0); i < r.RecordCount; i++ {
		ofs := int64(20 + i*r.RecordSize)
		row := r.readRecord(ofs)
		if row != nil {
			if id, ok := row["ID"].(uint32); ok {
				rows[id] = row
			} else if id, ok := row["id"].(uint32); ok {
				rows[id] = row
			} else {
				rows[i] = row
			}
		}
	}
	return rows
}

func (r *DBCReader) readRecord(ofs int64) map[string]interface{} {
	out := make(map[string]interface{})

	byteOfs := int64(0)
	for _, sf := range r.Schema {
		switch sf.Type {
		case FieldString:
			if byteOfs+4 > int64(r.RecordSize) {
				out[sf.Name] = ""
				byteOfs += 4
				continue
			}
			offset := binary.LittleEndian.Uint32(r.Data[ofs+byteOfs:])
			byteOfs += 4
			if offset == 0 {
				out[sf.Name] = ""
			} else {
				out[sf.Name] = r.readString(int64(offset))
			}

		case FieldInt32:
			out[sf.Name] = int32(binary.LittleEndian.Uint32(r.Data[ofs+byteOfs:]))
			byteOfs += 4
		case FieldUInt32:
			out[sf.Name] = binary.LittleEndian.Uint32(r.Data[ofs+byteOfs:])
			byteOfs += 4
		case FieldInt8:
			out[sf.Name] = int8(r.Data[ofs+byteOfs])
			byteOfs += 1
		case FieldUInt8:
			out[sf.Name] = r.Data[ofs+byteOfs]
			byteOfs += 1
		case FieldInt16:
			out[sf.Name] = int16(binary.LittleEndian.Uint16(r.Data[ofs+byteOfs:]))
			byteOfs += 2
		case FieldUInt16:
			out[sf.Name] = binary.LittleEndian.Uint16(r.Data[ofs+byteOfs:])
			byteOfs += 2
		case FieldInt64:
			out[sf.Name] = int64(binary.LittleEndian.Uint64(r.Data[ofs+byteOfs:]))
			byteOfs += 8
		case FieldUInt64:
			out[sf.Name] = binary.LittleEndian.Uint64(r.Data[ofs+byteOfs:])
			byteOfs += 8
		case FieldFloat:
			out[sf.Name] = float32(binary.LittleEndian.Uint32(r.Data[ofs+byteOfs:]))
			byteOfs += 4
		default:
			out[sf.Name] = nil
			byteOfs += 4
		}
	}

	return out
}

func (r *DBCReader) readString(offset int64) string {
	pos := r.StringBlockOffset + offset
	if pos < 0 || pos >= int64(len(r.Data)) {
		return ""
	}
	end := pos
	for end < int64(len(r.Data)) && r.Data[end] != 0 {
		end++
	}
	return string(r.Data[pos:end])
}
