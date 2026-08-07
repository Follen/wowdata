package db2

import (
	"context"
	"fmt"
	"strings"
)

func getRowsContext(ctx context.Context, reader RowReader, ids []uint32, fields []string, filterFn func(map[string]interface{}) bool, limit int) ([]map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		if batch, ok := reader.(ContextBatchRowReader); ok {
			rows, err := batch.GetRowsContext(ctx, ids, fields)
			if err != nil {
				return nil, err
			}
			return filterRowsContext(ctx, rows, filterFn, limit)
		}
		rows := make([]map[string]interface{}, 0, len(ids))
		for index, id := range ids {
			if index&255 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			row := reader.GetRow(id)
			if projected, ok := reader.(ProjectedRowReader); ok {
				row = projected.GetRowProjected(id, fields)
			}
			if row != nil && (filterFn == nil || filterFn(row)) {
				rows = append(rows, projectFields(row, fields))
				if limit > 0 && len(rows) >= limit {
					break
				}
			}
		}
		return rows, ctx.Err()
	}
	if scanner, ok := reader.(ContextScanRowReader); ok {
		return scanner.ScanContext(ctx, fields, filterFn, limit)
	}
	rows := GetRows(reader, ids, fields, filterFn, limit)
	return rows, ctx.Err()
}

func searchRowsContext(ctx context.Context, reader RowReader, field, query string, caseSensitive bool, fields []string, limit int) ([]map[string]interface{}, error) {
	searchQuery := query
	if !caseSensitive {
		searchQuery = strings.ToLower(query)
	}
	match := func(row map[string]interface{}) bool {
		value, exists := row[field]
		if !exists {
			return false
		}
		return containsSearchValue(value, searchQuery, caseSensitive)
	}
	if scanner, ok := reader.(ContextScanRowReader); ok {
		return scanner.ScanContext(ctx, fields, match, limit)
	}
	rows := SearchRows(reader, field, query, caseSensitive, limit)
	if len(fields) > 0 {
		for i, row := range rows {
			rows[i] = projectFields(row, fields)
		}
	}
	return rows, ctx.Err()
}

func getForeignRowsContext(ctx context.Context, reader RowReader, table, field string, value uint32) ([]map[string]interface{}, bool, error) {
	if batch, ok := reader.(ContextBatchRelationshipRowReader); ok {
		byValue, err := batch.GetRelationshipRowsBatchContext(ctx, []uint32{value}, nil)
		if err != nil {
			return nil, false, err
		}
		rows, handled := byValue[value]
		if handled && (len(rows) == 0 || relationshipRowsMatchField(rows, field, value)) {
			return rows, true, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if _, native := reader.(ContextBatchRelationshipRowReader); !native {
		rows, relationship := GetForeignRows(reader, table, field, value)
		return rows, relationship, ctx.Err()
	}
	match := func(row map[string]interface{}) bool { return rowForeignValue(row[field]) == value }
	if scanner, ok := reader.(ContextScanRowReader); ok {
		rows, err := scanner.ScanContext(ctx, nil, match, 0)
		return rows, false, err
	}
	return nil, false, fmt.Errorf("context relationship reader for %s has no scan fallback", table)
}

func getForeignRowsBatchContext(ctx context.Context, reader RowReader, table, field string, values []uint32, fields []string) ([]map[string]interface{}, bool, error) {
	if len(values) == 0 {
		return nil, true, ctx.Err()
	}
	if batch, ok := reader.(ContextBatchRelationshipRowReader); ok {
		byValue, err := batch.GetRelationshipRowsBatchContext(ctx, values, fields)
		if err != nil {
			return nil, false, err
		}
		rows := make([]map[string]interface{}, 0)
		allHandled := true
		for _, value := range values {
			matched, handled := byValue[value]
			if !handled || (len(matched) > 0 && !relationshipRowsMatchField(matched, field, value)) {
				allHandled = false
				break
			}
			rows = append(rows, matched...)
		}
		if allHandled {
			return rows, true, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if _, native := reader.(ContextBatchRelationshipRowReader); !native {
		var rows []map[string]interface{}
		allRelationship := true
		for _, value := range values {
			matched, relationship := GetForeignRows(reader, table, field, value)
			allRelationship = allRelationship && relationship
			for _, row := range matched {
				rows = append(rows, projectFields(row, fields))
			}
		}
		return rows, allRelationship, ctx.Err()
	}
	wanted := make(map[uint32]struct{}, len(values))
	for _, value := range values {
		wanted[value] = struct{}{}
	}
	match := func(row map[string]interface{}) bool { _, ok := wanted[rowForeignValue(row[field])]; return ok }
	if scanner, ok := reader.(ContextScanRowReader); ok {
		rows, err := scanner.ScanContext(ctx, fields, match, 0)
		return rows, false, err
	}
	return nil, false, fmt.Errorf("context relationship reader for %s has no scan fallback", table)
}

func filterRowsContext(ctx context.Context, rows []map[string]interface{}, filterFn func(map[string]interface{}) bool, limit int) ([]map[string]interface{}, error) {
	if filterFn == nil && (limit <= 0 || len(rows) <= limit) {
		return rows, ctx.Err()
	}
	filtered := rows[:0]
	for index, row := range rows {
		if index&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if filterFn == nil || filterFn(row) {
			filtered = append(filtered, row)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
	}
	return filtered, ctx.Err()
}

func streamRowsContext(ctx context.Context, reader RowReader, fields []string, filterFn func(map[string]interface{}) bool, limit int, yield func(map[string]interface{}) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if streamer, ok := reader.(ContextStreamRowReader); ok {
		return streamer.StreamRowsContext(ctx, fields, filterFn, limit, yield)
	}
	return fmt.Errorf("reader does not support bounded streaming")
}
