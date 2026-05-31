package main

import (
	"fmt"
	"os"
	"strings"

	"wowdata/internal/db2"
	"wowdata/internal/dbd"
)

func main() {
	data, _ := os.ReadFile("fixtures/golden/inputs/ItemSparse-classic-era.db2")
	raw, _ := os.ReadFile("cache/dbd/ItemSparse.dbd")
	parser, err := dbd.Parse(strings.NewReader(string(raw)))
	if err != nil {
		panic(err)
	}
	entry := parser.GetStructure("1.15.8.67156", "")
	if entry == nil {
		panic("no entry")
	}
	schema, _ := db2.SchemaFromDBD(entry)
	for i := 0; i < len(schema); i++ {
		fmt.Println("schema", i, schema[i].Name, schema[i].Type, schema[i].ArrayLen)
	}
	reader, err := db2.NewWDCReaderFromBytes("ItemSparse", data, schema)
	if err != nil {
		panic(err)
	}
	fmt.Println("sections", len(reader.Sections), "recordCount", reader.RecordCount, "recordSize", reader.RecordSize, "flags", reader.Flags)
	for i := 0; i < 8 && i < len(reader.FieldInfo); i++ {
		fi := reader.FieldInfo[i]
		fmt.Println("fieldInfo", i, "ofsBits", fi.FieldOffsetBits, "sizeBits", fi.FieldSizeBits, "compression", fi.FieldCompression, "packing", fi.FieldCompressionPacking)
	}
	for i, section := range reader.Sections {
		if i > 5 {
			break
		}
		ids := section.IDList
		if len(ids) > 5 {
			ids = ids[:5]
		}
		fmt.Println("section", i, "isNormal", section.IsNormal, "ofs", section.RecordDataOfs, "size", section.RecordDataSize, "offsetEnd", section.Header.OffsetRecordsEnd, "fileOffset", section.Header.FileOffset, "strings", section.Header.StringTableSize, "ids", len(section.IDList), "first", ids)
	}
	row := reader.GetRow(25)
	for _, key := range []string{"ID", "AllowableRace", "Display_lang", "DmgVariance", "InventoryType", "OverallQualityID", "SheatheType"} {
		fmt.Printf("%s=%#v\n", key, row[key])
	}
}
