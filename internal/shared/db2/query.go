package db2

import (
	"fmt"
	"sort"
	"strings"
)

type QueryResult struct {
	Schema []SchemaField            `json:"schema"`
	Rows   []map[string]interface{} `json:"rows"`
}

type RowReader interface {
	GetRow(recordID uint32) map[string]interface{}
	GetAllRows() map[uint32]map[string]interface{}
}

type RelationshipRowReader interface {
	GetRelationshipRows(fkValue uint32) ([]map[string]interface{}, bool)
}

func GetSchema(schema []SchemaField) []SchemaField {
	return schema
}

func GetRowByID(reader RowReader, id uint32) map[string]interface{} {
	return reader.GetRow(id)
}

func GetRows(reader RowReader, ids []uint32, fields []string, filterFn func(map[string]interface{}) bool, limit int) []map[string]interface{} {
	var results []map[string]interface{}

	if len(ids) > 0 {
		for _, id := range ids {
			row := reader.GetRow(id)
			if row != nil {
				if filterFn == nil || filterFn(row) {
					results = append(results, projectFields(row, fields))
				}
			}
		}
		return results
	}

	allRows := reader.GetAllRows()
	rowIDs := sortedRowIDs(allRows)

	for _, id := range rowIDs {
		row := allRows[uint32(id)]
		if filterFn != nil && !filterFn(row) {
			continue
		}
		results = append(results, projectFields(row, fields))
		if limit > 0 && len(results) >= limit {
			break
		}
	}

	return results
}

func SearchRows(reader RowReader, field string, query string, caseSensitive bool, limit int) []map[string]interface{} {
	allRows := reader.GetAllRows()
	var results []map[string]interface{}

	rowIDs := sortedRowIDs(allRows)
	for _, id := range rowIDs {
		row := allRows[uint32(id)]
		val, ok := row[field]
		if !ok {
			continue
		}
		strVal := fmt.Sprint(val)
		matches := false
		if caseSensitive {
			matches = strings.Contains(strVal, query)
		} else {
			matches = strings.Contains(strings.ToLower(strVal), strings.ToLower(query))
		}
		if matches {
			results = append(results, row)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}

	return results
}

func GetForeignRows(reader RowReader, table string, fkField string, fkValue uint32) []map[string]interface{} {
	if relationReader, ok := reader.(RelationshipRowReader); ok {
		if rows, handled := relationReader.GetRelationshipRows(fkValue); handled {
			return rows
		}
	}

	allRows := reader.GetAllRows()
	var results []map[string]interface{}

	rowIDs := sortedRowIDs(allRows)
	for _, id := range rowIDs {
		row := allRows[uint32(id)]
		if val, ok := row[fkField]; ok {
			var rowVal uint32
			switch v := val.(type) {
			case uint32:
				rowVal = v
			case int32:
				rowVal = uint32(v)
			default:
				continue
			}
			if rowVal == fkValue {
				results = append(results, row)
			}
		}
	}

	return results
}

func sortedRowIDs(rows map[uint32]map[string]interface{}) []int {
	rowIDs := make([]int, 0, len(rows))
	for id := range rows {
		rowIDs = append(rowIDs, int(id))
	}
	sort.Ints(rowIDs)
	return rowIDs
}

func projectFields(row map[string]interface{}, fields []string) map[string]interface{} {
	if len(fields) == 0 {
		return row
	}
	out := make(map[string]interface{})
	for _, f := range fields {
		if v, ok := row[f]; ok {
			out[f] = v
		}
	}
	return out
}
