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
		return "dbFieldInt8"
	case FieldUInt8:
		return "dbFieldUInt8"
	case FieldInt16:
		return "dbFieldInt16"
	case FieldUInt16:
		return "dbFieldUInt16"
	case FieldInt32:
		return "dbFieldInt32"
	case FieldUInt32:
		return "dbFieldUInt32"
	case FieldInt64:
		return "dbFieldInt64"
	case FieldUInt64:
		return "dbFieldUInt64"
	case FieldFloat:
		return "dbFieldFloat"
	case FieldRelation:
		return "dbFieldRelation"
	case FieldNonInlineID:
		return "dbFieldNonInlineID"
	default:
		return "dbFieldUnknown"
	}
}
