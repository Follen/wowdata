package db2

import (
	"context"
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

// ReaderCapabilities are optional fast paths implemented by native readers.
// The legacy RowReader contract remains the compatibility fallback.
type SizedReader interface {
	Size() int
}

type BatchRowReader interface {
	GetRows(ids []uint32, fields []string) []map[string]interface{}
}

type ProjectedRowReader interface {
	GetRowProjected(recordID uint32, fields []string) map[string]interface{}
}

type ScanRowReader interface {
	Scan(fields []string, filterFn func(map[string]interface{}) bool, limit int) []map[string]interface{}
}

type BatchRelationshipRowReader interface {
	GetRelationshipRowsBatch(fkValues []uint32, fields []string) map[uint32][]map[string]interface{}
}

type ContextBatchRowReader interface {
	GetRowsContext(ctx context.Context, ids []uint32, fields []string) ([]map[string]interface{}, error)
}

type ContextScanRowReader interface {
	ScanContext(ctx context.Context, fields []string, filterFn func(map[string]interface{}) bool, limit int) ([]map[string]interface{}, error)
}

type ContextStreamRowReader interface {
	StreamRowsContext(ctx context.Context, fields []string, filterFn func(map[string]interface{}) bool, limit int, yield func(map[string]interface{}) error) error
}

type ContextBatchRelationshipRowReader interface {
	GetRelationshipRowsBatchContext(ctx context.Context, fkValues []uint32, fields []string) (map[uint32][]map[string]interface{}, error)
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
		if batch, ok := reader.(BatchRowReader); ok {
			rows := batch.GetRows(ids, fields)
			if filterFn == nil {
				return rows
			}
			results := rows[:0]
			for _, row := range rows {
				if filterFn(row) {
					results = append(results, row)
				}
			}
			return results
		}
		for _, id := range ids {
			row := reader.GetRow(id)
			if projected, ok := reader.(ProjectedRowReader); ok {
				row = projected.GetRowProjected(id, fields)
			}
			if row != nil {
				if filterFn == nil || filterFn(row) {
					results = append(results, projectFields(row, fields))
				}
			}
		}
		return results
	}

	if scanner, ok := reader.(ScanRowReader); ok {
		return scanner.Scan(fields, filterFn, limit)
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
	searchQuery := query
	if !caseSensitive {
		searchQuery = strings.ToLower(searchQuery)
	}
	if scanner, ok := reader.(ScanRowReader); ok {
		return scanner.Scan(nil, func(row map[string]interface{}) bool {
			val, exists := row[field]
			if !exists {
				return false
			}
			return containsSearchValue(val, searchQuery, caseSensitive)
		}, limit)
	}
	allRows := reader.GetAllRows()
	var results []map[string]interface{}

	rowIDs := sortedRowIDs(allRows)
	for _, id := range rowIDs {
		row := allRows[uint32(id)]
		val, ok := row[field]
		if !ok {
			continue
		}
		matches := containsSearchValue(val, searchQuery, caseSensitive)
		if matches {
			results = append(results, row)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}

	return results
}

func containsSearchValue(value interface{}, query string, caseSensitive bool) bool {
	text, ok := value.(string)
	if !ok {
		text = fmt.Sprint(value)
	}
	if caseSensitive {
		return strings.Contains(text, query)
	}
	if isASCII(text) && isASCII(query) {
		return containsFoldASCII(text, query)
	}
	return strings.Contains(strings.ToLower(text), query)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func containsFoldASCII(text, query string) bool {
	if query == "" {
		return true
	}
	if len(query) > len(text) {
		return false
	}
	for start := 0; start+len(query) <= len(text); start++ {
		matched := true
		for i := 0; i < len(query); i++ {
			a, b := text[start+i], query[i]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func GetForeignRows(reader RowReader, table string, fkField string, fkValue uint32) ([]map[string]interface{}, bool) {
	if batch, ok := reader.(BatchRelationshipRowReader); ok {
		rows, handled := batch.GetRelationshipRowsBatch([]uint32{fkValue}, nil)[fkValue]
		if handled && (len(rows) == 0 || relationshipRowsMatchField(rows, fkField, fkValue)) {
			return rows, true
		}
	}
	if relationReader, ok := reader.(RelationshipRowReader); ok {
		if rows, handled := relationReader.GetRelationshipRows(fkValue); handled {
			if relationshipRowsMatchField(rows, fkField, fkValue) {
				return rows, true
			}
		}
	}
	match := func(row map[string]interface{}) bool { return rowForeignValue(row[fkField]) == fkValue }
	if scanner, ok := reader.(ScanRowReader); ok {
		return scanner.Scan(nil, match, 0), false
	}

	allRows := reader.GetAllRows()
	var results []map[string]interface{}

	rowIDs := sortedRowIDs(allRows)
	for _, id := range rowIDs {
		row := allRows[uint32(id)]
		if rowForeignValue(row[fkField]) == fkValue {
			results = append(results, row)
		}
	}

	return results, false
}

func relationshipRowsMatchField(rows []map[string]interface{}, field string, value uint32) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if rowForeignValue(row[field]) != value {
			return false
		}
	}
	return true
}

func rowForeignValue(value interface{}) uint32 {
	switch typed := value.(type) {
	case uint8:
		return uint32(typed)
	case uint16:
		return uint32(typed)
	case uint32:
		return typed
	case uint64:
		return uint32(typed)
	case int8:
		return uint32(typed)
	case int16:
		return uint32(typed)
	case int32:
		return uint32(typed)
	case int64:
		return uint32(typed)
	case int:
		return uint32(typed)
	default:
		return 0
	}
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
