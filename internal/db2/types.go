package db2

type CompressionType uint32

const (
	CompNone                CompressionType = 0
	CompBitpacked           CompressionType = 1
	CompCommonData          CompressionType = 2
	CompBitpackedIndexed    CompressionType = 3
	CompBitpackedIndexedArray CompressionType = 4
	CompBitpackedSigned     CompressionType = 5
)

type FieldType int

const (
	FieldString   FieldType = iota
	FieldInt8
	FieldUInt8
	FieldInt16
	FieldUInt16
	FieldInt32
	FieldUInt32
	FieldInt64
	FieldUInt64
	FieldFloat
	FieldRelation
	FieldNonInlineID
)

type SchemaField struct {
	Name     string
	Type     FieldType
	ArrayLen int // 0 = scalar
}

type FieldStorageInfo struct {
	FieldOffsetBits        uint16
	FieldSizeBits          uint16
	AdditionalDataSize     uint32
	FieldCompression       CompressionType
	FieldCompressionPacking [3]uint32
}

type SectionHeader struct {
	TactKeyHash         uint64
	FileOffset          uint32
	RecordCount         uint32
	StringTableSize     uint32
	CopyTableSize       uint32  // WDC2: bytes
	OffsetMapOffset     uint32  // WDC2
	OffsetRecordsEnd    uint32  // WDC3+
	IDListSize          uint32
	RelationshipDataSize uint32
	OffsetMapIDCount    uint32  // WDC3+
	CopyTableCount      uint32  // WDC3+: count
}

type OffsetMapEntry struct {
	Offset uint32
	Size   uint16
}

type Section struct {
	Header               SectionHeader
	IsNormal             bool
	RecordDataOfs        int64
	RecordDataSize       int64
	StringBlockOfs       int64
	StringTableOffset    int64
	StringTableOffsetBase int64
	IDList               []uint32
	OffsetMap            map[uint32]OffsetMapEntry
	RelationshipMap      map[uint32]uint32
	IsEncrypted          bool
}

type CopyTableEntry struct {
	DestID int32
	SrcID  int32
}
