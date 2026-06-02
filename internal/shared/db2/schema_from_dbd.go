package db2

import (
	"fmt"

	"wowdata/internal/shared/dbd"
)

func SchemaFromDBD(entry *dbd.DBDEntry) ([]SchemaField, error) {
	if entry == nil {
		return nil, fmt.Errorf("DBD entry is nil")
	}

	schema := make([]SchemaField, 0, len(entry.Fields))
	for _, field := range entry.Fields {
		fieldType, err := fieldTypeFromDBD(field)
		if err != nil {
			return nil, err
		}
		arrayLen := 0
		if field.ArrayLen > -1 {
			arrayLen = field.ArrayLen
		}
		schema = append(schema, SchemaField{Name: field.Name, Type: fieldType, ArrayLen: arrayLen})
	}
	return schema, nil
}

func fieldTypeFromDBD(field dbd.DBDField) (FieldType, error) {
	if !field.IsInline && field.IsRelation {
		return FieldRelation, nil
	}
	if !field.IsInline && field.IsID {
		return FieldNonInlineID, nil
	}

	switch field.Type {
	case "string", "locstring":
		return FieldString, nil
	case "float":
		return FieldFloat, nil
	case "int":
		switch field.Size {
		case 8:
			if field.IsSigned {
				return FieldInt8, nil
			}
			return FieldUInt8, nil
		case 16:
			if field.IsSigned {
				return FieldInt16, nil
			}
			return FieldUInt16, nil
		case 32:
			if field.IsSigned {
				return FieldInt32, nil
			}
			return FieldUInt32, nil
		case 64:
			if field.IsSigned {
				return FieldInt64, nil
			}
			return FieldUInt64, nil
		default:
			return 0, fmt.Errorf("unsupported DBD integer size %d for field %s", field.Size, field.Name)
		}
	default:
		return 0, fmt.Errorf("unrecognized DBD type %s for field %s", field.Type, field.Name)
	}
}
