package duckdb

import "fmt"

func SelectByID(table string, field string, id uint32) (string, []interface{}, error) {
	if !IsSafeIdentifier(table) {
		return "", nil, fmt.Errorf("unsafe table identifier: %q", table)
	}
	if !IsSafeIdentifier(field) {
		return "", nil, fmt.Errorf("unsafe field identifier: %q", field)
	}
	sql := fmt.Sprintf(`SELECT * FROM "%s" WHERE "%s" = ?`, table, field)
	return sql, []interface{}{id}, nil
}
