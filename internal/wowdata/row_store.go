package wowdata

import "fmt"

type rowStore interface {
	Rows(table string, ids []uint32, fields []string, filter string, limit int) ([]map[string]interface{}, error)
}

type relationRowStore interface {
	ForeignKey(table string, field string, value uint32, limit int) ([]map[string]interface{}, error)
}

func rowsByRelation(store rowStore, table, field string, value uint32) []map[string]interface{} {
	if related, ok := store.(relationRowStore); ok {
		rows, err := related.ForeignKey(table, field, value, 0)
		if err == nil {
			return rows
		}
	}
	rows, err := store.Rows(table, nil, nil, "", 0)
	if err != nil {
		return nil
	}
	filtered := make([]map[string]interface{}, 0)
	for _, row := range rows {
		if rowUint32(row, field) == value {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

type readyRowStore interface {
	Ready() bool
}

func isRuntimeReady(store rowStore) bool {
	if store == nil {
		return true
	}
	if ready, ok := store.(readyRowStore); ok {
		return ready.Ready()
	}
	return true
}

func isRowStoreReady(store rowStore, tables ...string) bool {
	if store == nil {
		return false
	}
	if !isRuntimeReady(store) {
		return false
	}
	for _, table := range tables {
		if _, err := store.Rows(table, nil, nil, "", 1); err != nil {
			return false
		}
	}
	return true
}

func rowString(row map[string]interface{}, field string) string {
	if v, ok := row[field]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

func rowUint32(row map[string]interface{}, field string) uint32 {
	switch v := row[field].(type) {
	case uint8:
		return uint32(v)
	case uint16:
		return uint32(v)
	case uint32:
		return v
	case uint64:
		return uint32(v)
	case uint:
		return uint32(v)
	case int:
		if v > 0 {
			return uint32(v)
		}
	case int8:
		if v > 0 {
			return uint32(v)
		}
	case int16:
		if v > 0 {
			return uint32(v)
		}
	case int32:
		if v > 0 {
			return uint32(v)
		}
	case int64:
		if v > 0 {
			return uint32(v)
		}
	case float32:
		if v > 0 {
			return uint32(v)
		}
	case float64:
		if v > 0 {
			return uint32(v)
		}
	case []uint32:
		if len(v) > 0 {
			return v[0]
		}
	case []int:
		if len(v) > 0 && v[0] > 0 {
			return uint32(v[0])
		}
	case []interface{}:
		if len(v) > 0 {
			return rowUint32(map[string]interface{}{"v": v[0]}, "v")
		}
	}
	return 0
}

func rowInt(row map[string]interface{}, field string) int {
	switch v := row[field].(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case uint:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case []int:
		if len(v) > 0 {
			return v[0]
		}
	case []int8:
		if len(v) > 0 {
			return int(v[0])
		}
	case []int16:
		if len(v) > 0 {
			return int(v[0])
		}
	case []int32:
		if len(v) > 0 {
			return int(v[0])
		}
	case []int64:
		if len(v) > 0 {
			return int(v[0])
		}
	case []interface{}:
		if len(v) > 0 {
			return rowInt(map[string]interface{}{"v": v[0]}, "v")
		}
	}
	return 0
}

func rowUint32Slice(row map[string]interface{}, field string) []uint32 {
	switch v := row[field].(type) {
	case []uint32:
		return append([]uint32(nil), v...)
	case []int:
		out := make([]uint32, 0, len(v))
		for _, n := range v {
			if n > 0 {
				out = append(out, uint32(n))
			}
		}
		return out
	case []interface{}:
		out := make([]uint32, 0, len(v))
		for _, e := range v {
			row := map[string]interface{}{"v": e}
			if n := rowUint32(row, "v"); n > 0 {
				out = append(out, n)
			}
		}
		return out
	}
	return nil
}

func rowIntSlice(row map[string]interface{}, field string) []int {
	switch v := row[field].(type) {
	case []int:
		return append([]int(nil), v...)
	case []uint32:
		out := make([]int, 0, len(v))
		for _, n := range v {
			out = append(out, int(n))
		}
		return out
	case []float32:
		out := make([]int, 0, len(v))
		for _, n := range v {
			out = append(out, int(n))
		}
		return out
	case []float64:
		out := make([]int, 0, len(v))
		for _, n := range v {
			out = append(out, int(n))
		}
		return out
	case []interface{}:
		out := make([]int, 0, len(v))
		for _, e := range v {
			row := map[string]interface{}{"v": e}
			out = append(out, rowInt(row, "v"))
		}
		return out
	}
	return nil
}
