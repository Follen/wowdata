package db2

import (
	"encoding/binary"
	"fmt"
)

const dbcMagic = 0x43424457 // WDBC

type DBCReader struct {
	FileName          string
	Schema            []SchemaField
	BuildID           string
	IsLoaded          bool
	Data              []byte
	RecordCount       uint32
	FieldCount        uint32
	RecordSize        uint32
	StringBlockSize   uint32
	StringBlockOffset int64
}

func NewDBCReader(fileName, buildID string) *DBCReader {
	return &DBCReader{
		FileName: fileName,
		BuildID:  buildID,
	}
}

func NewDBCReaderFromBytes(fileName, buildID string, data []byte, schema []SchemaField) (*DBCReader, error) {
	reader := NewDBCReader(fileName, buildID)
	reader.Data = data
	reader.Schema = schema
	if err := reader.Parse(); err != nil {
		return nil, err
	}
	return reader, nil
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
	read := func(n int64) ([]byte, bool) {
		if n < 0 || byteOfs+n > int64(r.RecordSize) || ofs+byteOfs+n > int64(len(r.Data)) {
			return nil, false
		}
		data := r.Data[ofs+byteOfs : ofs+byteOfs+n]
		byteOfs += n
		return data, true
	}
	for _, sf := range r.Schema {
		switch sf.Type {
		case FieldString:
			data, ok := read(4)
			if !ok {
				return nil
			}
			offset := binary.LittleEndian.Uint32(data)
			if offset == 0 {
				out[sf.Name] = ""
			} else {
				out[sf.Name] = r.readString(int64(offset))
			}

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
			out[sf.Name] = float32(binary.LittleEndian.Uint32(data))
		default:
			if _, ok := read(4); !ok {
				return nil
			}
			out[sf.Name] = nil
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
