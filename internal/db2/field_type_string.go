package db2

func (t FieldType) String() string {
	switch t {
	case FieldString:
		return "string"
	case FieldInt8:
		return "int8"
	case FieldUInt8:
		return "uint8"
	case FieldInt16:
		return "int16"
	case FieldUInt16:
		return "uint16"
	case FieldInt32:
		return "int32"
	case FieldUInt32:
		return "uint32"
	case FieldInt64:
		return "int64"
	case FieldUInt64:
		return "uint64"
	case FieldFloat:
		return "float"
	case FieldRelation:
		return "relation"
	case FieldNonInlineID:
		return "noninline-id"
	default:
		return "unknown"
	}
}

func (t FieldType) SchemaDescription() string {
	switch t {
	case FieldString:
		return "dbFieldString"
	case FieldInt8:
		return "dbFieldI8"
	case FieldUInt8:
		return "dbFieldU8"
	case FieldInt16:
		return "dbFieldI16"
	case FieldUInt16:
		return "dbFieldU16"
	case FieldInt32:
		return "dbFieldI32"
	case FieldUInt32:
		return "dbFieldU32"
	case FieldInt64:
		return "dbFieldI64"
	case FieldUInt64:
		return "dbFieldU64"
	case FieldFloat:
		return "dbFieldF32"
	case FieldRelation:
		return "dbFieldRelation"
	case FieldNonInlineID:
		return "dbFieldNonInlineID"
	default:
		return "dbFieldUnknown"
	}
}
